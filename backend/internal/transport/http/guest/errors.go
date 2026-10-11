package guest

import "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/apperr"

// ErrSessionRequired 是缺少游客会话凭证时的对外错误契约，供其他传输模块复用。
var ErrSessionRequired = guestSessionRequiredError

var (
	// guestSessionRequiredError 表示请求缺少游客会话凭证。
	guestSessionRequiredError = apperr.New("guest.session_required", "guest session required")
	// guestSessionInvalidError 表示游客会话凭证无效、过期或已撤销。
	guestSessionInvalidError = apperr.New("guest.session_invalid", "guest session invalid")
	// errOriginForbidden 表示游客修改类请求的来源不被信任。
	errOriginForbidden = apperr.New("guest.origin_forbidden", "guest request origin forbidden")
)
