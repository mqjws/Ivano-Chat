package guest

import (
	"context"
	"net/http"
	"strings"

	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
)

// SessionCookieName 是存放游客会话凭证的 HttpOnly Cookie 名称。
const SessionCookieName = "ivano_guest_session"

// ContextKeyGuestSession 是请求上下文中的游客会话键。
const ContextKeyGuestSession = "guest.session"

// SessionResolver 解析游客会话凭证。
type SessionResolver interface {
	ResolveSession(ctx context.Context, credential string) (*domainguest.Session, error)
}

// SessionMiddleware 校验游客会话 Cookie 并把会话写入请求上下文。
// 校验失败一律返回 401 + guest.session_invalid，前端据此重新创建匿名会话。
func SessionMiddleware(resolver SessionResolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		credential, err := c.Cookie(SessionCookieName)
		if err != nil || strings.TrimSpace(credential) == "" {
			response.ErrorFrom(c, http.StatusUnauthorized, guestSessionRequiredError)
			c.Abort()
			return
		}
		session, err := resolver.ResolveSession(c.Request.Context(), credential)
		if err != nil {
			response.ErrorFrom(c, http.StatusUnauthorized, guestSessionInvalidError)
			c.Abort()
			return
		}
		c.Set(ContextKeyGuestSession, session)
		c.Next()
	}
}

// MustSession 返回中间件写入的游客会话。
func MustSession(c *gin.Context) *domainguest.Session {
	value, ok := c.Get(ContextKeyGuestSession)
	if !ok {
		return nil
	}
	session, ok := value.(*domainguest.Session)
	if !ok {
		return nil
	}
	return session
}
