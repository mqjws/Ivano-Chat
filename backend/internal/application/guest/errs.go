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
)
