// Package guest 实现游客身份、公开配置与额度查询。
//
// 本包只负责身份与配置；实际模型调用、预算预留与结算在后续阶段接入。
package guest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	// 精简镜像可能缺少系统时区库，内嵌 tzdata 保证预算时区可用。
	_ "time/tzdata"

	appbilling "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/billing"
	appchannel "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
	domainuser "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/user"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"github.com/google/uuid"
)

const (
	// SettingsNamespace 是游客动态配置所在的命名空间。
	SettingsNamespace = "guest"
	// SessionTTL 是游客会话有效期，与浏览器端 24 小时正文缓存相互独立。
	SessionTTL = 7 * 24 * time.Hour
	// NanousdPerUSD 是站内账本的金额单位换算比例。
	NanousdPerUSD int64 = 1_000_000_000
	// DefaultBudgetTimezone 是实施文档约定的默认预算时区。
	DefaultBudgetTimezone = "Europe/Berlin"
	// BillingSubjectUsername 是专用游客计费主体的用户名；该账号不可登录。
	BillingSubjectUsername = "guest-billing-subject"
	// estimatedCostSafetyPercent 是预留安全系数：按估算费用的 125% 预留，
	// 覆盖推理 token、缓存写入等估算未覆盖的用量，结算时释放未使用部分。
	estimatedCostSafetyPercent = 125

	credentialBytes  = 32
	budgetDateLayout = "2006-01-02"
)

// 游客动态配置键名，与 settings 注册表的 guest 命名空间一一对应。
const (
	SettingEnabled               = "enabled"
	SettingModelWhitelist        = "model_whitelist"
	SettingPerVisitorDailyBudget = "per_visitor_daily_budget_usd"
	SettingGlobalDailyBudget     = "global_daily_budget_usd"
	SettingDailyGenerationLimit  = "daily_generation_limit"
	SettingRateLimitPerMinute    = "rate_limit_per_minute"
	SettingPerVisitorConcurrency = "per_visitor_concurrency"
	SettingGlobalConcurrency     = "global_concurrency"
	SettingInputTokenLimit       = "input_token_limit"
	SettingOutputTokenLimit      = "output_token_limit"
	SettingBudgetTimezone        = "budget_timezone"
	SettingBillingRetentionDays  = "billing_retention_days"
)

// Settings 是游客功能的运行时配置快照。
type Settings struct {
	Enabled                      bool
	ModelWhitelist               []string
	PerVisitorDailyBudgetNanousd int64
	GlobalDailyBudgetNanousd     int64
	DailyGenerationLimit         int
	RateLimitPerMinute           int
	PerVisitorConcurrency        int
	GlobalConcurrency            int
	InputTokenLimit              int
	OutputTokenLimit             int
	BudgetTimezone               string
	BillingRetentionDays         int
}

// DefaultSettings 返回实施文档约定的游客默认值；settings 注册表的种子与之保持一致。
func DefaultSettings() Settings {
	return Settings{
		Enabled:                      false,
		ModelWhitelist:               nil,
		PerVisitorDailyBudgetNanousd: 100_000_000,
		GlobalDailyBudgetNanousd:     1_000_000_000,
		DailyGenerationLimit:         20,
		RateLimitPerMinute:           2,
		PerVisitorConcurrency:        1,
		GlobalConcurrency:            2,
		InputTokenLimit:              8000,
		OutputTokenLimit:             2048,
		BudgetTimezone:               DefaultBudgetTimezone,
		BillingRetentionDays:         30,
	}
}

// settingsReader 读取动态配置命名空间。
type settingsReader interface {
	RuntimeValuesByNamespace(ctx context.Context, namespace string) (map[string]string, error)
}

// billingSubjectStore 负责游客计费主体的账号读写。
type billingSubjectStore interface {
	GetByUsername(ctx context.Context, username string) (*domainuser.User, error)
	CreateWithCredential(ctx context.Context, input repository.CreateWithCredentialInput) error
}

// usageEstimator 估算一次请求的站内费用上限。
type usageEstimator interface {
	EstimateUsageNanousd(ctx context.Context, userID uint, input appbilling.UsageEstimateInput) (int64, error)
}

// modelCatalog 提供白名单内的可用模型。
type modelCatalog interface {
	ListGuestModels(ctx context.Context, allowedRefs []string) ([]appchannel.ModelView, error)
}

// Service 实现游客会话、公开配置与额度查询。
type Service struct {
	repo      repository.GuestRepository
	settings  settingsReader
	models    modelCatalog
	users     billingSubjectStore
	estimator usageEstimator
	now       func() time.Time
	entropy   io.Reader

	subjectMu sync.Mutex
	subjectID uint
}

// NewService 创建游客应用服务。
func NewService(repo repository.GuestRepository, settings settingsReader, models modelCatalog) *Service {
	return &Service{
		repo:     repo,
		settings: settings,
		models:   models,
		now:      time.Now,
		entropy:  rand.Reader,
	}
}

// SetBillingDependencies 注入游客计费主体与费用估算依赖。
func (s *Service) SetBillingDependencies(users billingSubjectStore, estimator usageEstimator) {
	s.users = users
	s.estimator = estimator
}

// BillingSubjectID 返回游客专用计费主体的用户 ID，必要时创建该不可登录账号。
func (s *Service) BillingSubjectID(ctx context.Context) (uint, error) {
	s.subjectMu.Lock()
	defer s.subjectMu.Unlock()
	if s.subjectID != 0 {
		return s.subjectID, nil
	}
	if s.users == nil {
		return 0, ErrBillingSubjectUnavailable
	}
	if existing, err := s.users.GetByUsername(ctx, BillingSubjectUsername); err == nil && existing != nil {
		s.subjectID = existing.ID
		return existing.ID, nil
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return 0, err
	}
	created := &domainuser.User{
		Username:    BillingSubjectUsername,
		DisplayName: "Guest Billing Subject",
		Role:        domainuser.RoleUser,
		Status:      domainuser.StatusActive,
	}
	err := s.users.CreateWithCredential(ctx, repository.CreateWithCredentialInput{
		User: created,
		Credential: domainuser.Credential{
			PasswordEnabled:   false,
			PasswordHash:      "",
			PasswordOrigin:    domainuser.PasswordOriginSSOPlaceholder,
			MustResetPassword: true,
		},
	})
	if err != nil {
		// 并发启动时另一个实例可能已经创建；重新读取一次。
		if again, readErr := s.users.GetByUsername(ctx, BillingSubjectUsername); readErr == nil && again != nil {
			s.subjectID = again.ID
			return again.ID, nil
		}
		return 0, err
	}
	if created.ID == 0 {
		return 0, ErrBillingSubjectUnavailable
	}
	s.subjectID = created.ID
	return created.ID, nil
}

// GenerateLimits 是生成链路需要收敛的参数上限。
type GenerateLimits struct {
	InputTokens  int
	OutputTokens int
}

// GenerateLimitsFor 返回当前游客配置下的输入 / 输出 Token 上限。
func (s *Service) GenerateLimitsFor(ctx context.Context) (GenerateLimits, error) {
	settings, err := s.settingsSnapshot(ctx)
	if err != nil {
		return GenerateLimits{}, err
	}
	return GenerateLimits{InputTokens: settings.InputTokenLimit, OutputTokens: settings.OutputTokenLimit}, nil
}

// EstimateInput 是费用估算输入。
type EstimateInput struct {
	BillingSubjectID  uint
	PlatformModelName string
	InputTokens       int64
	OutputTokens      int64
}

// EstimateRequestNanousd 估算一次游客请求的预留金额，含安全系数。
func (s *Service) EstimateRequestNanousd(ctx context.Context, input EstimateInput) (int64, error) {
	if s.estimator == nil {
		return 0, nil
	}
	if input.InputTokens <= 0 && input.OutputTokens <= 0 {
		return 0, nil
	}
	subjectID := input.BillingSubjectID
	if subjectID == 0 {
		var err error
		subjectID, err = s.BillingSubjectID(ctx)
		if err != nil {
			return 0, err
		}
	}
	estimate, err := s.estimator.EstimateUsageNanousd(ctx, subjectID, appbilling.UsageEstimateInput{
		PlatformModelName: strings.TrimSpace(input.PlatformModelName),
		InputTokens:       input.InputTokens,
		OutputTokens:      input.OutputTokens,
	})
	if err != nil {
		return 0, err
	}
	if estimate <= 0 {
		return 0, nil
	}
	return estimate * estimatedCostSafetyPercent / 100, nil
}

// SessionResult 描述一次游客会话创建或恢复的结果。
type SessionResult struct {
	Session *domainguest.Session
	// Credential 只在新建会话时返回原文；恢复时为空，浏览器继续使用已有 Cookie。
	Credential string
	Created    bool
}

// ConfigView 是无需会话即可读取的公开游客配置。
type ConfigView struct {
	Enabled        bool
	SessionTTL     time.Duration
	BudgetTimezone string
	BudgetDate     string
	ResetAt        time.Time
	Settings       Settings
	Models         []appchannel.ModelView
}

// UsageView 是单个游客或全站在当前预算日的额度与用量。
type UsageView struct {
	BudgetNanousd    int64
	SettledNanousd   int64
	ReservedNanousd  int64
	ConsumedNanousd  int64
	AvailableNanousd int64
	RequestCount     int
}

// UsageSnapshot 汇总单个游客与全站当天的额度与用量。
type UsageSnapshot struct {
	BudgetTimezone string
	BudgetDate     string
	ResetAt        time.Time
	Settings       Settings
	PerVisitor     UsageView
	Global         UsageView
}

// EnsureSession 创建或恢复游客会话。credential 为空时新建，否则按凭证恢复。
func (s *Service) EnsureSession(ctx context.Context, credential string, meta domainguest.SessionMeta) (*SessionResult, error) {
	settings, err := s.settingsSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, ErrDisabled
	}
	now := s.now().UTC()
	if trimmed := strings.TrimSpace(credential); trimmed != "" {
		session, err := s.resolve(ctx, trimmed, now)
		switch {
		case err == nil:
			return &SessionResult{Session: session, Created: false}, nil
		case errors.Is(err, ErrSessionInvalid), errors.Is(err, ErrSessionRequired):
			// 凭证已过期、被撤销或根本不存在：按“无凭证”处理并签发新的匿名会话。
			// 游客在服务端没有正文数据，只有按 guest_id 统计的额度；换新身份不会泄露任何内容，
			// 但会重新获得个人额度，因此全站预算与频率 / 并发限制仍然按 IP 摘要生效。
		default:
			return nil, err
		}
	}

	plain, hash, err := newCredential(s.entropy)
	if err != nil {
		return nil, err
	}
	session := &domainguest.Session{
		PublicID:       uuid.NewString(),
		CredentialHash: hash,
		Status:         domainguest.SessionStatusActive,
		LastSeenAt:     now,
		ExpiresAt:      now.Add(SessionTTL),
		IPHash:         strings.TrimSpace(meta.IPHash),
		UserAgentHash:  strings.TrimSpace(meta.UserAgentHash),
	}
	if err := s.repo.CreateGuestSession(ctx, session); err != nil {
		return nil, err
	}
	return &SessionResult{Session: session, Credential: plain, Created: true}, nil
}

// ResolveSession 校验凭证并返回可用会话；同时刷新最近使用时间。
func (s *Service) ResolveSession(ctx context.Context, credential string) (*domainguest.Session, error) {
	return s.resolve(ctx, credential, s.now().UTC())
}

func (s *Service) resolve(ctx context.Context, credential string, now time.Time) (*domainguest.Session, error) {
	trimmed := strings.TrimSpace(credential)
	if trimmed == "" {
		return nil, ErrSessionRequired
	}
	session, err := s.repo.GetGuestSessionByCredentialHash(ctx, HashCredential(trimmed))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrSessionInvalid
		}
		return nil, err
	}
	if !session.Usable(now) {
		return nil, ErrSessionInvalid
	}
	if err := s.repo.TouchGuestSession(ctx, session.ID, now); err != nil {
		return nil, err
	}
	session.LastSeenAt = now
	return session, nil
}

// PublicConfig 返回公开游客配置与白名单模型。
func (s *Service) PublicConfig(ctx context.Context) (*ConfigView, error) {
	settings, err := s.settingsSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	budgetDate, resetAt, err := BudgetWindow(s.now().UTC(), settings.BudgetTimezone)
	if err != nil {
		return nil, err
	}
	view := &ConfigView{
		Enabled:        settings.Enabled,
		SessionTTL:     SessionTTL,
		BudgetTimezone: settings.BudgetTimezone,
		BudgetDate:     budgetDate,
		ResetAt:        resetAt,
		Settings:       settings,
		Models:         []appchannel.ModelView{},
	}
	if !settings.Enabled || len(settings.ModelWhitelist) == 0 || s.models == nil {
		return view, nil
	}
	models, err := s.models.ListGuestModels(ctx, settings.ModelWhitelist)
	if err != nil {
		return nil, err
	}
	view.Models = models
	return view, nil
}

// Usage 返回当前游客与全站当天的额度与用量。
func (s *Service) Usage(ctx context.Context, session *domainguest.Session) (*UsageSnapshot, error) {
	if session == nil || session.ID == 0 {
		return nil, ErrSessionRequired
	}
	settings, err := s.settingsSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	budgetDate, resetAt, err := BudgetWindow(s.now().UTC(), settings.BudgetTimezone)
	if err != nil {
		return nil, err
	}
	perVisitor, err := s.repo.GetGuestDailyUsage(ctx, session.ID, budgetDate)
	if err != nil {
		return nil, err
	}
	global, err := s.repo.GetGuestGlobalDailyUsage(ctx, budgetDate)
	if err != nil {
		return nil, err
	}
	return &UsageSnapshot{
		BudgetTimezone: settings.BudgetTimezone,
		BudgetDate:     budgetDate,
		ResetAt:        resetAt,
		Settings:       settings,
		PerVisitor:     buildUsageView(settings.PerVisitorDailyBudgetNanousd, perVisitor),
		Global:         buildUsageView(settings.GlobalDailyBudgetNanousd, global),
	}, nil
}

type consumedUsage interface {
	ConsumedNanousd() int64
}

// RequestInput 是一次游客生成请求在预留阶段需要的上下文。
type RequestInput struct {
	Session     *domainguest.Session
	RequestID   string
	ModelRef    string
	InputDigest string
	// InputTokens / OutputTokens 用于自动估算预留金额；
	// EstimateNanousd 非零时优先使用调用方给定的金额。
	InputTokens     int64
	OutputTokens    int64
	EstimateNanousd int64
}

// Reservation 表示一次已经占用的游客预算与次数。
type Reservation struct {
	SessionID       uint
	RequestID       string
	BudgetDate      string
	BillingRefNo    string
	EstimateNanousd int64
	// Existing 为真表示命中幂等：本次没有新增预留，Record 是已存在的请求记录。
	Existing bool
	Record   *domainguest.RequestRecord
}

// Reserve 执行游客生成前的策略校验并原子占用次数与预算。
//
// 校验顺序：开关 → 模型白名单 → 频率 → 并发 → 幂等 → 预算与次数（在数据库事务内）。
// 命中幂等时不新增预留：调用方需要根据 Record.Status 决定返回既有结果还是拒绝重复执行。
func (s *Service) Reserve(ctx context.Context, input RequestInput) (*Reservation, error) {
	if input.Session == nil || input.Session.ID == 0 {
		return nil, ErrSessionRequired
	}
	requestID := strings.TrimSpace(input.RequestID)
	if requestID == "" {
		return nil, ErrRequestConflict
	}
	settings, err := s.settingsSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, ErrDisabled
	}
	if !modelAllowed(settings.ModelWhitelist, input.ModelRef) {
		return nil, ErrModelNotAllowed
	}
	if settings.InputTokenLimit > 0 && input.InputTokens > int64(settings.InputTokenLimit) {
		return nil, ErrInputTooLarge
	}
	now := s.now().UTC()
	budgetDate, _, err := BudgetWindow(now, settings.BudgetTimezone)
	if err != nil {
		return nil, err
	}
	estimateNanousd := input.EstimateNanousd
	if estimateNanousd == 0 && (input.InputTokens > 0 || input.OutputTokens > 0) {
		outputTokens := input.OutputTokens
		if outputTokens <= 0 {
			outputTokens = int64(settings.OutputTokenLimit)
		}
		estimateNanousd, err = s.EstimateRequestNanousd(ctx, EstimateInput{
			PlatformModelName: input.ModelRef,
			InputTokens:       input.InputTokens,
			OutputTokens:      outputTokens,
		})
		if err != nil {
			return nil, err
		}
	}

	if existing, err := s.repo.GetGuestRequest(ctx, input.Session.ID, requestID); err == nil && existing != nil {
		if strings.TrimSpace(existing.InputDigest) != strings.TrimSpace(input.InputDigest) {
			return nil, ErrRequestConflict
		}
		return &Reservation{
			SessionID:       input.Session.ID,
			RequestID:       requestID,
			BudgetDate:      existing.BudgetDate,
			BillingRefNo:    existing.BillingRefNo,
			EstimateNanousd: existing.ReservedNanousd,
			Existing:        true,
			Record:          existing,
		}, nil
	} else if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	if settings.RateLimitPerMinute > 0 {
		recent, err := s.repo.CountGuestRequestsSince(ctx, input.Session.ID, now.Add(-time.Minute))
		if err != nil {
			return nil, err
		}
		if recent >= settings.RateLimitPerMinute {
			return nil, ErrRateLimited
		}
	}
	active, err := s.repo.CountActiveGuestRequests(ctx, input.Session.ID)
	if err != nil {
		return nil, err
	}
	if settings.PerVisitorConcurrency > 0 && active >= settings.PerVisitorConcurrency {
		return nil, ErrConcurrencyLimited
	}
	activeGlobal, err := s.repo.CountActiveGuestRequestsGlobal(ctx)
	if err != nil {
		return nil, err
	}
	if settings.GlobalConcurrency > 0 && activeGlobal >= settings.GlobalConcurrency {
		return nil, ErrConcurrencyLimited
	}

	record, err := s.repo.ReserveGuestRequest(ctx, domainguest.ReservationRequest{
		SessionID:       input.Session.ID,
		RequestID:       requestID,
		BudgetDate:      budgetDate,
		ModelRef:        strings.TrimSpace(input.ModelRef),
		InputDigest:     strings.TrimSpace(input.InputDigest),
		BillingRefNo:    BillingRefNo(input.Session.PublicID, requestID),
		EstimateNanousd: estimateNanousd,
		Now:             now,
	}, domainguest.ReservationLimits{
		PerVisitorDailyNanousd: settings.PerVisitorDailyBudgetNanousd,
		GlobalDailyNanousd:     settings.GlobalDailyBudgetNanousd,
		DailyGenerationLimit:   settings.DailyGenerationLimit,
	})
	if err != nil {
		return nil, translateReservationError(err)
	}
	return &Reservation{
		SessionID:       record.SessionID,
		RequestID:       record.RequestID,
		BudgetDate:      record.BudgetDate,
		BillingRefNo:    record.BillingRefNo,
		EstimateNanousd: record.ReservedNanousd,
		Record:          record,
	}, nil
}

// Settle 用实际用量结算预留；needsReview 为真时保留待核对状态。
func (s *Service) Settle(ctx context.Context, reservation *Reservation, billedNanousd int64, needsReview bool) error {
	if reservation == nil || reservation.Existing {
		return nil
	}
	status := domainguest.RequestStatusSettled
	if needsReview {
		status = domainguest.RequestStatusNeedsReview
	}
	return s.repo.SettleGuestRequest(ctx, reservation.SessionID, reservation.RequestID, billedNanousd, status, s.now().UTC())
}

// Release 释放预留；releaseCount 为真表示本次没有发生上游调用，连次数一起归还。
func (s *Service) Release(ctx context.Context, reservation *Reservation, releaseCount bool, rejected bool) error {
	if reservation == nil || reservation.Existing {
		return nil
	}
	status := domainguest.RequestStatusFailed
	if rejected {
		status = domainguest.RequestStatusRejected
	}
	return s.repo.ReleaseGuestRequest(ctx, reservation.SessionID, reservation.RequestID, releaseCount, status, s.now().UTC())
}

// BillingRefNo 生成计费主体账本的幂等编号，用于把账单追溯到具体游客请求。
// 只包含摘要，不包含正文；同一游客请求始终得到同一编号。
func BillingRefNo(guestPublicID string, requestID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(guestPublicID) + ":" + strings.TrimSpace(requestID)))
	return "guest-" + hex.EncodeToString(sum[:16])
}

func modelAllowed(whitelist []string, modelRef string) bool {
	candidate := strings.ToLower(strings.TrimSpace(modelRef))
	if candidate == "" {
		return false
	}
	for _, item := range whitelist {
		if strings.ToLower(strings.TrimSpace(item)) == candidate {
			return true
		}
	}
	return false
}

func translateReservationError(err error) error {
	switch {
	case errors.Is(err, domainguest.ErrDailyCountExceeded):
		return ErrDailyCountExceeded
	case errors.Is(err, domainguest.ErrDailyBudgetExceeded):
		return ErrDailyBudgetExceeded
	case errors.Is(err, domainguest.ErrGlobalBudgetExceeded):
		return ErrGlobalBudgetExceeded
	case errors.Is(err, repository.ErrDuplicate):
		return ErrRequestConflict
	default:
		return err
	}
}

func buildUsageView(budgetNanousd int64, usage consumedUsage) UsageView {
	consumed := usage.ConsumedNanousd()
	if consumed < 0 {
		consumed = 0
	}
	available := budgetNanousd - consumed
	if available < 0 {
		available = 0
	}
	view := UsageView{
		BudgetNanousd:    budgetNanousd,
		ConsumedNanousd:  consumed,
		AvailableNanousd: available,
	}
	switch item := usage.(type) {
	case *domainguest.DailyUsage:
		if item != nil {
			view.SettledNanousd = item.SettledNanousd
			view.ReservedNanousd = item.ReservedNanousd
			view.RequestCount = item.RequestCount
		}
	case *domainguest.GlobalDailyUsage:
		if item != nil {
			view.SettledNanousd = item.SettledNanousd
			view.ReservedNanousd = item.ReservedNanousd
			view.RequestCount = item.RequestCount
		}
	}
	return view
}

// settingsSnapshot 读取并解析游客动态配置；缺失或非法的配置项回退到默认值。
func (s *Service) settingsSnapshot(ctx context.Context) (Settings, error) {
	result := DefaultSettings()
	if s.settings == nil {
		return result, nil
	}
	values, err := s.settings.RuntimeValuesByNamespace(ctx, SettingsNamespace)
	if err != nil {
		return result, err
	}
	applySettings(&result, values)
	return result, nil
}

func applySettings(target *Settings, values map[string]string) {
	if target == nil || len(values) == 0 {
		return
	}
	if value, ok := parseBoolValue(values[SettingEnabled]); ok {
		target.Enabled = value
	}
	if value, ok := values[SettingModelWhitelist]; ok {
		target.ModelWhitelist = splitList(value)
	}
	if value, ok := parseNanousd(values[SettingPerVisitorDailyBudget]); ok {
		target.PerVisitorDailyBudgetNanousd = value
	}
	if value, ok := parseNanousd(values[SettingGlobalDailyBudget]); ok {
		target.GlobalDailyBudgetNanousd = value
	}
	if value, ok := parseNonNegativeInt(values[SettingDailyGenerationLimit]); ok {
		target.DailyGenerationLimit = value
	}
	if value, ok := parseNonNegativeInt(values[SettingRateLimitPerMinute]); ok && value > 0 {
		target.RateLimitPerMinute = value
	}
	if value, ok := parseNonNegativeInt(values[SettingPerVisitorConcurrency]); ok && value > 0 {
		target.PerVisitorConcurrency = value
	}
	if value, ok := parseNonNegativeInt(values[SettingGlobalConcurrency]); ok && value > 0 {
		target.GlobalConcurrency = value
	}
	if value, ok := parseNonNegativeInt(values[SettingInputTokenLimit]); ok && value > 0 {
		target.InputTokenLimit = value
	}
	if value, ok := parseNonNegativeInt(values[SettingOutputTokenLimit]); ok && value > 0 {
		target.OutputTokenLimit = value
	}
	if value, ok := parseNonNegativeInt(values[SettingBillingRetentionDays]); ok {
		target.BillingRetentionDays = value
	}
	if value := strings.TrimSpace(values[SettingBudgetTimezone]); value != "" {
		if _, err := time.LoadLocation(value); err == nil {
			target.BudgetTimezone = value
		}
	}
}

// BudgetWindow 返回指定时刻在预算时区下的预算日与下一次重置时间。
// 重置时间按目标时区的日历日计算，因此夏令时切换当天不是固定 24 小时。
func BudgetWindow(now time.Time, timezone string) (string, time.Time, error) {
	name := strings.TrimSpace(timezone)
	if name == "" {
		name = DefaultBudgetTimezone
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return "", time.Time{}, ErrInvalidTimezone
	}
	local := now.In(location)
	startOfDay := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return local.Format(budgetDateLayout), startOfDay.AddDate(0, 0, 1), nil
}

// HashCredential 计算会话凭证摘要；服务端只保存该摘要。
func HashCredential(credential string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(credential)))
	return hex.EncodeToString(sum[:])
}

// FormatUSD 把 nanousd 金额格式化为最多 9 位小数、去掉末尾 0 的美元字符串。
func FormatUSD(nanousd int64) string {
	if nanousd <= 0 {
		return "0"
	}
	whole := nanousd / NanousdPerUSD
	fraction := nanousd % NanousdPerUSD
	if fraction == 0 {
		return strconv.FormatInt(whole, 10)
	}
	digits := strings.TrimRight(fmt.Sprintf("%09d", fraction), "0")
	return strconv.FormatInt(whole, 10) + "." + digits
}

func newCredential(entropy io.Reader) (string, string, error) {
	if entropy == nil {
		entropy = rand.Reader
	}
	buffer := make([]byte, credentialBytes)
	if _, err := io.ReadFull(entropy, buffer); err != nil {
		return "", "", err
	}
	plain := base64.RawURLEncoding.EncodeToString(buffer)
	return plain, HashCredential(plain), nil
}

func splitList(raw string) []string {
	parts := strings.Split(raw, ",")
	results := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			results = append(results, trimmed)
		}
	}
	return results
}

func parseBoolValue(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func parseNonNegativeInt(raw string) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}

// parseNanousd 解析十进制美元字符串，按位换算为 nanousd，避免浮点误差。
func parseNanousd(raw string) (int64, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	parts := strings.SplitN(trimmed, ".", 2)
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole < 0 {
		return 0, false
	}
	if whole > (math.MaxInt64-NanousdPerUSD)/NanousdPerUSD {
		return 0, false
	}
	fraction := int64(0)
	if len(parts) == 2 {
		digits := parts[1]
		if digits == "" || strings.Trim(digits, "0123456789") != "" {
			return 0, false
		}
		for _, symbol := range digits {
			if symbol < '0' || symbol > '9' {
				return 0, false
			}
		}
		if len(digits) > 9 {
			digits = digits[:9]
		}
		for len(digits) < 9 {
			digits += "0"
		}
		parsed, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			return 0, false
		}
		fraction = parsed
	}
	return whole*NanousdPerUSD + fraction, true
}
