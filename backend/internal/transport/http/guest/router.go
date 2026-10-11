package guest

import "github.com/gin-gonic/gin"

// RegisterPublicRoutes 注册游客公开路由：查询公开配置与创建/恢复游客会话。
func (m *Module) RegisterPublicRoutes(api *gin.RouterGroup) {
	api.GET("/guest/config", m.Handler.GetConfig)
	api.POST("/guest/session", m.Handler.CreateSession)
}

// RegisterAdminRoutes 注册管理端游客面板路由（由管理员中间件保护）。
func (m *Module) RegisterAdminRoutes(adminGroup *gin.RouterGroup) {
	adminGroup.GET("/guest/overview", m.Handler.AdminOverview)
	adminGroup.GET("/guest/requests", m.Handler.AdminRequests)
}

// SessionMiddleware 返回游客会话中间件，供服务端组装共享的游客凭证路由组。
func (m *Module) SessionMiddleware() gin.HandlerFunc {
	return SessionMiddleware(m.Handler.service)
}

// RegisterSessionRoutes 注册需要游客会话凭证的路由；调用方需先挂载 SessionMiddleware。
func (m *Module) RegisterSessionRoutes(api *gin.RouterGroup) {
	api.GET("/guest/me", m.Handler.GetMe)
}
