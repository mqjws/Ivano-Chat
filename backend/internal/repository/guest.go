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
}
