package repository

import (
	"context"
	"time"

	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
)

// GuestRepository 封装游客会话与用量账本的数据访问。
type GuestRepository interface {
	// CreateGuestSession 写入新的游客会话，并回填自增 ID 与时间戳。
	CreateGuestSession(ctx context.Context, item *domainguest.Session) error
	// GetGuestSessionByCredentialHash 按凭证摘要查询会话。
	GetGuestSessionByCredentialHash(ctx context.Context, credentialHash string) (*domainguest.Session, error)
	// TouchGuestSession 更新会话最近使用时间。
	TouchGuestSession(ctx context.Context, sessionID uint, seenAt time.Time) error
	// GetGuestDailyUsage 返回单个游客在预算日内的用量；没有记录时返回零值而不是 ErrNotFound。
	GetGuestDailyUsage(ctx context.Context, sessionID uint, budgetDate string) (*domainguest.DailyUsage, error)
	// GetGuestGlobalDailyUsage 返回全站游客在预算日内的用量；没有记录时返回零值。
	GetGuestGlobalDailyUsage(ctx context.Context, budgetDate string) (*domainguest.GlobalDailyUsage, error)

	// ReserveGuestRequest 在单个事务内校验并占用次数、个人预算与全站预算，同时写入请求记录。
	// 相同 (session_id, request_id) 已存在时返回 ErrDuplicate。
	// 超出上限时返回 domainguest.ErrDailyCountExceeded / ErrDailyBudgetExceeded / ErrGlobalBudgetExceeded。
	ReserveGuestRequest(ctx context.Context, input domainguest.ReservationRequest, limits domainguest.ReservationLimits) (*domainguest.RequestRecord, error)
	// GetGuestRequest 读取一次游客请求记录。
	GetGuestRequest(ctx context.Context, sessionID uint, requestID string) (*domainguest.RequestRecord, error)
	// SettleGuestRequest 把预留转为实际结算：释放预留、累加已结算金额并更新请求状态。
	SettleGuestRequest(ctx context.Context, sessionID uint, requestID string, settledNanousd int64, status string, now time.Time) error
	// ReleaseGuestRequest 释放预留；releaseCount 为真时同时归还当日次数（用于未发生上游调用的失败）。
	ReleaseGuestRequest(ctx context.Context, sessionID uint, requestID string, releaseCount bool, status string, now time.Time) error
	// CountGuestRequestsSince 统计某个游客在给定时间之后创建的请求数，用于频率限制。
	CountGuestRequestsSince(ctx context.Context, sessionID uint, since time.Time) (int, error)
	// CountActiveGuestRequests 统计某个游客仍在进行中的请求数，用于单游客并发限制。
	CountActiveGuestRequests(ctx context.Context, sessionID uint) (int, error)
	// CountActiveGuestRequestsGlobal 统计全站仍在进行中的游客请求数，用于全站并发限制。
	CountActiveGuestRequestsGlobal(ctx context.Context) (int, error)

	// ListGuestDailyUsage 按预算日返回各游客的用量（按已结算金额倒序），用于后台统计。
	ListGuestDailyUsage(ctx context.Context, budgetDate string, limit int) ([]domainguest.UsageRow, error)
	// ListGuestRequests 分页返回匿名账单记录（不含正文），并返回总数。
	ListGuestRequests(ctx context.Context, filter domainguest.RequestFilter) ([]domainguest.RequestRecord, int64, error)
}
