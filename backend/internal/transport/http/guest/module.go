package guest

// Module 聚合游客公开接口与凭证保护接口。
type Module struct {
	Handler *Handler
}

// NewModule 创建游客 HTTP 模块。
func NewModule(handler *Handler) *Module {
	return &Module{Handler: handler}
}
