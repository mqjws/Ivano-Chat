package guest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	appguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/guest"
	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/config"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
)

// Service 是游客 HTTP 层需要的应用能力。
type Service interface {
	EnsureSession(ctx context.Context, credential string, meta domainguest.SessionMeta) (*appguest.SessionResult, error)
	ResolveSession(ctx context.Context, credential string) (*domainguest.Session, error)
	PublicConfig(ctx context.Context) (*appguest.ConfigView, error)
	Usage(ctx context.Context, session *domainguest.Session) (*appguest.UsageSnapshot, error)
}

// Handler 提供游客公开接口与凭证保护接口。
type Handler struct {
	service Service
	cfg     *config.Runtime
}

// NewHandler 创建游客 HTTP 处理器。
func NewHandler(service Service, cfg *config.Runtime) *Handler {
	return &Handler{service: service, cfg: cfg}
}

// GetConfig godoc
// @Summary 查询公开游客配置
// @Description 返回游客开关、白名单模型、限额与预算重置时间；无需登录。
// @Tags guest
// @Produce json
// @Success 200 {object} ConfigResponse
// @Failure 500 {object} response.Envelope
// @Router /guest/config [get]
func (h *Handler) GetConfig(c *gin.Context) {
	view, err := h.service.PublicConfig(c.Request.Context())
	if err != nil {
		writeGuestError(c, err)
		return
	}
	response.Success(c, toConfigResponse(view))
}

// CreateSession godoc
// @Summary 创建或恢复游客会话
// @Description 使用现有 Cookie 恢复游客会话；没有有效 Cookie 时创建匿名会话并写入 HttpOnly Cookie。
// @Tags guest
// @Produce json
// @Success 200 {object} SessionResponse
// @Failure 403 {object} response.Envelope
// @Failure 500 {object} response.Envelope
// @Router /guest/session [post]
func (h *Handler) CreateSession(c *gin.Context) {
	if !h.originAllowed(c) {
		response.ErrorFrom(c, http.StatusForbidden, errOriginForbidden)
		return
	}
	existing, _ := c.Cookie(SessionCookieName)
	result, err := h.service.EnsureSession(c.Request.Context(), existing, domainguest.SessionMeta{
		IPHash:        h.clientIPDigest(c),
		UserAgentHash: h.userAgentDigest(c),
	})
	if err != nil {
		writeGuestError(c, err)
		return
	}
	if result.Created && result.Credential != "" && result.Session != nil {
		h.writeSessionCookie(c, result.Credential, result.Session.ExpiresAt)
	}
	response.Success(c, toSessionResponse(result))
}

// GetMe godoc
// @Summary 查询当前游客会话与额度
// @Description 返回游客会话信息、当天已结算与预留额度、剩余额度和预算重置时间。
// @Tags guest
// @Produce json
// @Success 200 {object} MeResponse
// @Failure 401 {object} response.Envelope
// @Failure 500 {object} response.Envelope
// @Router /guest/me [get]
func (h *Handler) GetMe(c *gin.Context) {
	session := MustSession(c)
	if session == nil {
		response.ErrorFrom(c, http.StatusUnauthorized, guestSessionRequiredError)
		return
	}
	snapshot, err := h.service.Usage(c.Request.Context(), session)
	if err != nil {
		writeGuestError(c, err)
		return
	}
	response.Success(c, toMeResponse(session, snapshot))
}

func writeGuestError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, appguest.ErrDisabled):
		response.ErrorFrom(c, http.StatusForbidden, appguest.ErrDisabled)
	case errors.Is(err, appguest.ErrSessionRequired):
		response.ErrorFrom(c, http.StatusUnauthorized, guestSessionRequiredError)
	case errors.Is(err, appguest.ErrSessionInvalid):
		response.ErrorFrom(c, http.StatusUnauthorized, guestSessionInvalidError)
	case errors.Is(err, errOriginForbidden):
		response.ErrorFrom(c, http.StatusForbidden, errOriginForbidden)
	default:
		response.InternalError(c)
	}
}

func (h *Handler) writeSessionCookie(c *gin.Context, credential string, expiresAt time.Time) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     SessionCookieName,
		Value:    credential,
		Path:     "/api/v1/guest",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   isHTTPSRequest(c),
		SameSite: http.SameSiteLaxMode,
	})
}

// originAllowed 校验修改类请求的来源。没有 Origin（同源导航、命令行）时放行，
// 配置了允许来源时要求精确匹配，未配置允许来源时交给 CORS 中间件处理。
func (h *Handler) originAllowed(c *gin.Context) bool {
	origin := strings.TrimRight(strings.TrimSpace(c.GetHeader("Origin")), "/")
	if origin == "" || h.cfg == nil {
		return true
	}
	snapshot := h.cfg.Snapshot()
	if web := strings.TrimRight(strings.TrimSpace(snapshot.PublicWebBaseURL), "/"); web != "" && strings.EqualFold(web, origin) {
		return true
	}
	allowed := false
	for _, item := range strings.Split(snapshot.CORSAllowOrigin, ",") {
		trimmed := strings.TrimRight(strings.TrimSpace(item), "/")
		if trimmed == "" {
			continue
		}
		allowed = true
		if trimmed == "*" || strings.EqualFold(trimmed, origin) {
			return true
		}
	}
	return !allowed
}

// clientIPDigest 使用数据加密密钥对来源 IP 做 HMAC，避免明文存储地址。
func (h *Handler) clientIPDigest(c *gin.Context) string {
	return h.digest(c.ClientIP())
}

func (h *Handler) userAgentDigest(c *gin.Context) string {
	return h.digest(c.Request.UserAgent())
}

func (h *Handler) digest(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	key := ""
	if h.cfg != nil {
		key = h.cfg.Snapshot().DataEncryptionKey
	}
	if key == "" {
		sum := sha256.Sum256([]byte(trimmed))
		return hex.EncodeToString(sum[:])
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(trimmed))
	return hex.EncodeToString(mac.Sum(nil))
}

func isHTTPSRequest(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return c.GetHeader("X-Forwarded-Proto") == "https"
}
