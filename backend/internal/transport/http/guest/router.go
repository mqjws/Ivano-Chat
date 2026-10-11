package guest

import "github.com/gin-gonic/gin"

// RegisterPublicRoutes 注册游客公开路由：查询公开配置与创建/恢复游客会话。
func (m *Module) RegisterPublicRoutes(api *gin.RouterGroup) {
	api.GET("/guest/config", m.Handler.GetConfig)
	api.POST("/guest/session", m.Handler.CreateSession)
}

// RegisterSessionRoutes 注册需要游客会话凭证的路由。
func (m *Module) RegisterSessionRoutes(api *gin.RouterGroup) {
	group := api.Group("/guest")
	group.Use(SessionMiddleware(m.Handler.service))
	group.GET("/me", m.Handler.GetMe)
}
