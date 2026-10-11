package conversation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	appconversation "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/conversation"
	appguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/guest"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/apperr"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/tokenestimate"
	guesthttp "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/guest"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

// guestChatMaxRequestBytes 限制游客 JSON 请求体大小（阶段 3 不接受附件）。
const guestChatMaxRequestBytes = 2 << 20

var (
	errGuestCapabilityNotAllowed = apperr.New("guest.capability_not_allowed", "guest request capability not allowed")
	errGuestRequestIDRequired    = apperr.New("guest.request_id_required", "guest request id required")
)

// GuestService 是游客聊天接口需要的应用能力。
type GuestService interface {
	Reserve(ctx context.Context, input appguest.RequestInput) (*appguest.Reservation, error)
	Settle(ctx context.Context, reservation *appguest.Reservation, billedNanousd int64, needsReview bool) error
	Release(ctx context.Context, reservation *appguest.Reservation, releaseCount bool, rejected bool) error
	BillingSubjectID(ctx context.Context) (uint, error)
	GenerateLimitsFor(ctx context.Context) (appguest.GenerateLimits, error)
}

// SetGuestService 注入游客应用服务；未注入时游客流式接口返回 503。
func (h *Handler) SetGuestService(service GuestService) {
	h.guestService = service
}

// StreamGuestChatMessage godoc
// @Summary 游客流式聊天
// @Description 复用临时聊天链路：不创建会话 / 消息 / 运行记录，只按游客预算与次数限制计费。
// @Tags guest
// @Accept json
// @Produce application/x-ndjson
// @Param body body TemporaryChatMessageRequest true "游客临时对话参数"
// @Success 200 {string} string "NDJSON stream"
// @Failure 401 {object} ErrorDoc
// @Failure 403 {object} ErrorDoc
// @Failure 429 {object} ErrorDoc
// @Router /guest/messages/stream [post]
func (h *Handler) StreamGuestChatMessage(c *gin.Context) {
	if h.guestService == nil {
		response.ErrorWithCode(c, http.StatusServiceUnavailable, response.CodeServiceUnavailable)
		return
	}
	guestSession := guesthttp.MustSession(c)
	if guestSession == nil {
		response.ErrorFrom(c, http.StatusUnauthorized, guesthttp.ErrSessionRequired)
		return
	}
	req, ok := h.bindGuestChatRequest(c)
	if !ok {
		return
	}
	if !guestRequestAllowsOnlyText(req) {
		response.ErrorFrom(c, http.StatusForbidden, errGuestCapabilityNotAllowed)
		return
	}
	requestID := strings.TrimSpace(req.ClientRunID)
	if requestID == "" {
		response.ErrorFrom(c, http.StatusBadRequest, errGuestRequestIDRequired)
		return
	}
	limits, err := h.guestService.GenerateLimitsFor(c.Request.Context())
	if err != nil {
		writeGuestChatError(c, err)
		return
	}
	inputTokens := estimateGuestInputTokens(req.Messages)
	if limits.InputTokens > 0 && inputTokens > int64(limits.InputTokens) {
		writeGuestChatError(c, appguest.ErrInputTooLarge)
		return
	}

	req.Options = sanitizeMessageOptions(req.Options)
	clampGuestMaxOutputTokens(req.Options, limits.OutputTokens)
	input := appconversation.TemporaryChatInput{
		RequestID:   middleware.MustRequestID(c),
		SessionID:   strings.TrimSpace(req.SessionID),
		ClientRunID: requestID,
		Model:       strings.TrimSpace(req.Model),
		Options:     req.Options,
		Messages:    make([]appconversation.TemporaryChatMessage, 0, len(req.Messages)),
	}
	for _, item := range req.Messages {
		input.Messages = append(input.Messages, appconversation.TemporaryChatMessage{
			Role:    strings.TrimSpace(item.Role),
			Content: item.Content,
		})
	}
	// 临时聊天输入校验要求 UserID 非零，因此先解析游客计费主体（懒创建且进程内缓存）。
	subjectID, err := h.guestService.BillingSubjectID(c.Request.Context())
	if err != nil {
		writeGuestChatError(c, err)
		return
	}
	input.UserID = subjectID
	if err := appconversation.ValidateTemporaryChatInput(input); err != nil {
		response.ErrorFrom(c, http.StatusBadRequest, errInvalidTemporaryChatMessages)
		return
	}

	reservation, err := h.guestService.Reserve(c.Request.Context(), appguest.RequestInput{
		Session:     guestSession,
		RequestID:   requestID,
		ModelRef:    input.Model,
		InputDigest: guestInputDigest(req),
		InputTokens: inputTokens,
	})
	if err != nil {
		writeGuestChatError(c, err)
		return
	}
	if reservation.Existing {
		// 不保存游客正文，无法回放已完成的回复；同一请求 ID 一律拒绝重复执行。
		response.ErrorFrom(c, http.StatusConflict, appguest.ErrRequestConflict)
		return
	}
	settleCtx := context.WithoutCancel(c.Request.Context())
	releaseReservation := func(releaseCount bool, rejected bool) {
		_ = h.guestService.Release(settleCtx, reservation, releaseCount, rejected)
	}

	usageSession, ok := h.beginUsageSession(c, appconversation.SendMessageBillingInput{
		UserID:            subjectID,
		PlatformModelName: input.Model,
		ClientRunID:       reservation.BillingRefNo,
	})
	if !ok {
		releaseReservation(true, true)
		return
	}
	defer usageSession.Close()
	input.UsageAuthorization = usageSession.Authorization()

	generationCtx, releaseLifecycle, ok := h.service.AcquireMessageGenerationLifecycle(c.Request.Context())
	if !ok {
		_ = usageSession.Finish(c.Request.Context(), nil)
		releaseReservation(true, true)
		response.ErrorWithCode(c, http.StatusServiceUnavailable, response.CodeServiceUnavailable)
		return
	}
	defer releaseLifecycle()

	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Cache-Control", "no-store, no-cache, no-transform")
	c.Header("Pragma", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	writeEvent := func(payload map[string]any) error {
		encoded, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			return marshalErr
		}
		if _, writeErr := c.Writer.Write(append(encoded, '\n')); writeErr != nil {
			return writeErr
		}
		c.Writer.Flush()
		return nil
	}
	input.OnEvent = func(eventType string, payload map[string]any) error {
		return writeEvent(normalizeStreamEventPayload(eventType, payload))
	}

	result, streamErr := h.service.StreamTemporaryChat(generationCtx, input, func(delta string) error {
		return writeEvent(map[string]any{"type": "delta", "delta": delta})
	})
	clientConnected := func() bool { return c.Request.Context().Err() == nil }

	billingErr := usageSession.Finish(c.Request.Context(), result)
	settledNanousd := int64(0)
	if ledger := usageSession.SettledLedger(); ledger != nil {
		settledNanousd = ledger.BilledNanousd
	}
	switch {
	case billingErr != nil:
		// 已发生上游用量但结算失败：标记待核对并保留预留，避免把未确认费用当成可用预算。
		if err := h.guestService.Settle(settleCtx, reservation, settledNanousd, true); err != nil && clientConnected() {
			_ = writeEvent(streamErrorPayload(err))
		}
	case streamErr != nil:
		if result != nil {
			_ = h.guestService.Settle(settleCtx, reservation, settledNanousd, false)
		} else {
			// 无法确认上游是否已被调用：保留当日次数，只释放费用预留。
			_ = h.guestService.Release(settleCtx, reservation, false, false)
		}
	default:
		_ = h.guestService.Settle(settleCtx, reservation, settledNanousd, false)
	}

	if billingErr != nil && clientConnected() {
		_ = writeEvent(streamErrorPayload(billingErr))
		return
	}
	if result != nil && result.IsModerationBlocked() {
		if !result.ModerationTerminalEmitted() && clientConnected() {
			_ = writeEvent(moderationBlockedStreamPayload(result, usageSession.Authorization()))
		}
		return
	}
	if streamErr != nil {
		if clientConnected() {
			_ = writeEvent(streamErrorPayload(streamErr))
		}
		return
	}
	_ = writeEvent(map[string]any{
		"type": "completed",
		"data": toSendMessageResponse(result),
	})
}

func (h *Handler) bindGuestChatRequest(c *gin.Context) (TemporaryChatMessageRequest, bool) {
	if contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type"))); !strings.HasPrefix(contentType, "application/json") && contentType != "" {
		response.ErrorFrom(c, http.StatusBadRequest, errGuestCapabilityNotAllowed)
		return TemporaryChatMessageRequest{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, guestChatMaxRequestBytes)
	var req TemporaryChatMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeTemporaryChatBindError(c, err)
		return TemporaryChatMessageRequest{}, false
	}
	return req, true
}

func guestRequestAllowsOnlyText(req TemporaryChatMessageRequest) bool {
	return len(req.SelectedToolIDs) == 0 &&
		len(req.SkillIDs) == 0 &&
		len(req.KnowledgeBaseIDs) == 0 &&
		len(req.UIComponentIDs) == 0 &&
		!req.HTMLVisualPrompt
}

func estimateGuestInputTokens(messages []TemporaryChatHistoryMessage) int64 {
	var total int64
	for _, item := range messages {
		total += int64(4) + tokenestimate.Estimate(item.Role) + tokenestimate.Estimate(item.Content)
	}
	return total
}

func guestInputDigest(req TemporaryChatMessageRequest) string {
	digest := sha256.New()
	_, _ = digest.Write([]byte(strings.TrimSpace(req.Model)))
	for _, item := range req.Messages {
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(strings.TrimSpace(item.Role)))
		_, _ = digest.Write([]byte{0})
		_, _ = digest.Write([]byte(item.Content))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// clampGuestMaxOutputTokens 把客户端请求的输出上限收敛到游客配置上限。
func clampGuestMaxOutputTokens(options map[string]any, limit int) {
	if options == nil || limit <= 0 {
		return
	}
	found := false
	for _, path := range [][]string{
		{"max_output_tokens"},
		{"max_completion_tokens"},
		{"max_tokens"},
		{"maxOutputTokens"},
		{"generationConfig", "maxOutputTokens"},
		{"generation_config", "max_output_tokens"},
	} {
		if clampOptionPath(options, path, limit) {
			found = true
		}
	}
	if !found {
		options["max_tokens"] = limit
	}
}

func clampOptionPath(options map[string]any, path []string, limit int) bool {
	current := options
	for index, key := range path {
		if index == len(path)-1 {
			value, ok := current[key]
			if !ok {
				return false
			}
			if tokens := int64FromOption(value); tokens <= 0 || tokens > int64(limit) {
				current[key] = limit
			}
			return true
		}
		next, ok := current[key]
		if !ok {
			return false
		}
		child, ok := next.(map[string]any)
		if !ok {
			return false
		}
		current = child
	}
	return false
}

func int64FromOption(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

func writeGuestChatError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, appguest.ErrDisabled):
		response.ErrorFrom(c, http.StatusForbidden, appguest.ErrDisabled)
	case errors.Is(err, appguest.ErrModelNotAllowed):
		response.ErrorFrom(c, http.StatusForbidden, appguest.ErrModelNotAllowed)
	case errors.Is(err, appguest.ErrSessionRequired), errors.Is(err, appguest.ErrSessionInvalid):
		response.ErrorFrom(c, http.StatusUnauthorized, appguest.ErrSessionInvalid)
	case errors.Is(err, appguest.ErrRateLimited):
		response.ErrorFrom(c, http.StatusTooManyRequests, appguest.ErrRateLimited)
	case errors.Is(err, appguest.ErrConcurrencyLimited):
		response.ErrorFrom(c, http.StatusTooManyRequests, appguest.ErrConcurrencyLimited)
	case errors.Is(err, appguest.ErrDailyCountExceeded):
		response.ErrorFrom(c, http.StatusTooManyRequests, appguest.ErrDailyCountExceeded)
	case errors.Is(err, appguest.ErrDailyBudgetExceeded), errors.Is(err, appguest.ErrGlobalBudgetExceeded):
		response.ErrorFrom(c, http.StatusPaymentRequired, err)
	case errors.Is(err, appguest.ErrInputTooLarge):
		response.ErrorFrom(c, http.StatusRequestEntityTooLarge, appguest.ErrInputTooLarge)
	case errors.Is(err, appguest.ErrRequestConflict):
		response.ErrorFrom(c, http.StatusConflict, appguest.ErrRequestConflict)
	case errors.Is(err, appguest.ErrBillingSubjectUnavailable):
		response.ErrorWithCode(c, http.StatusServiceUnavailable, response.CodeServiceUnavailable)
	default:
		response.InternalError(c)
	}
}
