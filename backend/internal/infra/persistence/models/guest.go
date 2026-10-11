package model

import "time"

// GuestSession 保存匿名游客会话。凭证只保存摘要，无法从摘要还原原文。
type GuestSession struct {
	ControlPlaneModel
	PublicID       string     `gorm:"size:64;not null;uniqueIndex:idx_guest_sessions_public_id;comment:游客公开ID"`
	CredentialHash string     `gorm:"size:64;not null;uniqueIndex:idx_guest_sessions_credential_hash;comment:会话凭证摘要(sha256 hex)"`
	Status         string     `gorm:"size:32;not null;default:'active';index:idx_guest_sessions_status;comment:会话状态(active/revoked)"`
	LastSeenAt     time.Time  `gorm:"not null;comment:最近使用时间"`
	ExpiresAt      time.Time  `gorm:"not null;index:idx_guest_sessions_expires_at;comment:会话过期时间"`
	RevokedAt      *time.Time `gorm:"comment:撤销时间"`
	IPHash         string     `gorm:"size:64;not null;default:'';index:idx_guest_sessions_ip_hash;comment:来源IP摘要，仅用于限流"`
	UserAgentHash  string     `gorm:"size:64;not null;default:'';comment:User-Agent 摘要"`
}

// TableName 指定表名。
func (GuestSession) TableName() string {
	return "guest_sessions"
}

// GuestDailyUsage 保存单个游客在一个预算日内的用量与预留。
type GuestDailyUsage struct {
	ControlPlaneModel
	SessionID       uint   `gorm:"not null;uniqueIndex:idx_guest_daily_usage_session_day;index:idx_guest_daily_usage_session;comment:游客会话ID"`
	BudgetDate      string `gorm:"size:10;not null;uniqueIndex:idx_guest_daily_usage_session_day;comment:预算日(YYYY-MM-DD，按预算时区)"`
	SettledNanousd  int64  `gorm:"not null;default:0;comment:已结算金额(nanousd)"`
	ReservedNanousd int64  `gorm:"not null;default:0;comment:进行中预留金额(nanousd)"`
	RequestCount    int    `gorm:"not null;default:0;comment:当日已占用生成次数"`
}

// TableName 指定表名。
func (GuestDailyUsage) TableName() string {
	return "guest_daily_usage"
}

// GuestGlobalDailyUsage 保存全站游客在一个预算日内的用量与预留。
type GuestGlobalDailyUsage struct {
	ControlPlaneModel
	BudgetDate      string `gorm:"size:10;not null;uniqueIndex:idx_guest_global_daily_usage_day;comment:预算日(YYYY-MM-DD，按预算时区)"`
	SettledNanousd  int64  `gorm:"not null;default:0;comment:已结算金额(nanousd)"`
	ReservedNanousd int64  `gorm:"not null;default:0;comment:进行中预留金额(nanousd)"`
	RequestCount    int    `gorm:"not null;default:0;comment:当日游客生成请求数"`
}

// TableName 指定表名。
func (GuestGlobalDailyUsage) TableName() string {
	return "guest_global_daily_usage"
}

// GuestRequest 保存游客计费请求的服务端状态，用于幂等与结算恢复；不保存对话正文。
type GuestRequest struct {
	ControlPlaneModel
	RequestID       string     `gorm:"size:64;not null;uniqueIndex:idx_guest_requests_session_request;comment:客户端请求ID"`
	SessionID       uint       `gorm:"not null;uniqueIndex:idx_guest_requests_session_request;index:idx_guest_requests_session;comment:游客会话ID"`
	BudgetDate      string     `gorm:"size:10;not null;index:idx_guest_requests_budget_date;comment:预算日(YYYY-MM-DD)"`
	Status          string     `gorm:"size:32;not null;index:idx_guest_requests_status;comment:pending/running/settled/failed/needs_review/rejected"`
	ModelRef        string     `gorm:"size:128;not null;default:'';comment:平台模型名称"`
	InputDigest     string     `gorm:"size:64;not null;default:'';comment:请求输入摘要，仅用于幂等比较"`
	ReservedNanousd int64      `gorm:"not null;default:0;comment:预留金额(nanousd)"`
	SettledNanousd  int64      `gorm:"not null;default:0;comment:结算金额(nanousd)"`
	SettledAt       *time.Time `gorm:"comment:结算时间"`
}

// TableName 指定表名。
func (GuestRequest) TableName() string {
	return "guest_requests"
}
