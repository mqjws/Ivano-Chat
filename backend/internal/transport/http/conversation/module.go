package conversation

import "github.com/gin-gonic/gin"

// Module 聚合会话 HTTP 处理器。
type Module struct {
	Handler *Handler
}

// RegisterGuestRoutes 注册游客流式聊天路由；调用方需先挂载游客会话中间件。
func (m *Module) RegisterGuestRoutes(api *gin.RouterGroup) {
	api.POST("/guest/messages/stream", m.Handler.StreamGuestChatMessage)
}

// NewModule 创建会话 HTTP 模块。
func NewModule(handler *Handler) *Module {
	return &Module{Handler: handler}
}
