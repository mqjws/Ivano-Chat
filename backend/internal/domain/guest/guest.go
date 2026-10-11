// Package guest 定义匿名游客会话与用量账本的领域类型。
//
// 设计边界（实施文档 §6-§8）：
//   - 浏览器只持有随机凭证原文，服务端只保存摘要，无法从摘要反推原文。
//   - 服务端不保存对话正文；计费请求记录只保留输入摘要与状态。
//   - 额度按配置时区的日历日统计，限额判定使用“已结算 + 进行中预留”。
package guest

import "time"

// 游客会话状态。
const (
	SessionStatusActive  = "active"
	SessionStatusRevoked = "revoked"
)

// 游客计费请求状态，覆盖预留、结算、失败与待核对。
const (
	RequestStatusPending     = "pending"
	RequestStatusRunning     = "running"
	RequestStatusSettled     = "settled"
	RequestStatusFailed      = "failed"
	RequestStatusNeedsReview = "needs_review"
	RequestStatusRejected    = "rejected"
)

// SessionMeta 是创建会话时允许记录的非身份元数据摘要。
type SessionMeta struct {
	IPHash        string
	UserAgentHash string
}

// Session 表示一次匿名游客会话。CredentialHash 只保存凭证摘要。
type Session struct {
	ID             uint
	PublicID       string
	CredentialHash string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastSeenAt     time.Time
	ExpiresAt      time.Time
	RevokedAt      *time.Time
	IPHash         string
	UserAgentHash  string
}

// Usable 判断会话在指定时间是否可用于游客接口。
func (s Session) Usable(now time.Time) bool {
	return s.ID != 0 && s.Status == SessionStatusActive && now.Before(s.ExpiresAt)
}

// DailyUsage 是单个游客在一个预算日内的用量。
type DailyUsage struct {
	SessionID       uint
	BudgetDate      string
	SettledNanousd  int64
	ReservedNanousd int64
	RequestCount    int
	UpdatedAt       time.Time
}

// ConsumedNanousd 返回已经结算与仍在占用的额度之和，用于限额判定。
func (u DailyUsage) ConsumedNanousd() int64 {
	return u.SettledNanousd + u.ReservedNanousd
}

// GlobalDailyUsage 是全站游客在一个预算日内的用量。
type GlobalDailyUsage struct {
	BudgetDate      string
	SettledNanousd  int64
	ReservedNanousd int64
	RequestCount    int
	UpdatedAt       time.Time
}

// ConsumedNanousd 返回全站当日已经结算与仍在占用的额度之和。
func (u GlobalDailyUsage) ConsumedNanousd() int64 {
	return u.SettledNanousd + u.ReservedNanousd
}

// RequestRecord 是一次游客计费请求的服务端记录。InputDigest 是请求内容摘要，
// 只用于幂等比较，不能从中还原正文。
type RequestRecord struct {
	ID              uint
	RequestID       string
	SessionID       uint
	BudgetDate      string
	Status          string
	ModelRef        string
	InputDigest     string
	ReservedNanousd int64
	SettledNanousd  int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SettledAt       *time.Time
}
