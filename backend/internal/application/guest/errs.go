package guest

import "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/apperr"

var (
	// ErrDisabled 表示管理员关闭了游客功能，拒绝新会话与新生成。
	ErrDisabled = apperr.New("guest.disabled", "guest mode is disabled")
	// ErrSessionRequired 表示请求缺少游客会话凭证。
	ErrSessionRequired = apperr.New("guest.session_required", "guest session required")
	// ErrSessionInvalid 表示游客会话凭证无效、已过期或已撤销。
	ErrSessionInvalid = apperr.New("guest.session_invalid", "guest session invalid")
	// ErrInvalidTimezone 表示预算时区配置不可用。
	ErrInvalidTimezone = apperr.New("guest.invalid_timezone", "guest budget timezone invalid")
	// ErrModelNotAllowed 表示请求的模型不在游客白名单内。
	ErrModelNotAllowed = apperr.New("guest.model_not_allowed", "guest model not allowed")
	// ErrRateLimited 表示超过单游客每分钟频率限制。
	ErrRateLimited = apperr.New("guest.rate_limited", "guest rate limit exceeded")
	// ErrConcurrencyLimited 表示超过单游客或全站并发生成限制。
	ErrConcurrencyLimited = apperr.New("guest.concurrency_limited", "guest concurrency limit exceeded")
	// ErrDailyCountExceeded 表示单游客当日生成次数已用尽。
	ErrDailyCountExceeded = apperr.New("guest.quota_exceeded", "guest daily generation quota exceeded")
	// ErrDailyBudgetExceeded 表示单游客当日预算不足。
	ErrDailyBudgetExceeded = apperr.New("guest.daily_budget_exceeded", "guest daily budget exceeded")
	// ErrGlobalBudgetExceeded 表示全站游客当日预算不足。
	ErrGlobalBudgetExceeded = apperr.New("guest.global_budget_exceeded", "guest global daily budget exceeded")
	// ErrInputTooLarge 表示输入上下文超过游客 Token 上限。
	ErrInputTooLarge = apperr.New("guest.input_too_large", "guest input context too large")
	// ErrBillingSubjectUnavailable 表示游客计费主体不可用（未注入或创建失败）。
	ErrBillingSubjectUnavailable = apperr.New("guest.billing_subject_unavailable", "guest billing subject unavailable")
	// ErrRequestConflict 表示相同请求 ID 的内容不一致，或该请求仍在进行中。
	ErrRequestConflict = apperr.New("guest.request_conflict", "guest request conflict")
)
