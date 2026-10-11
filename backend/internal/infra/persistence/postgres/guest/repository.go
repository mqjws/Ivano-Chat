package guest

import (
	"context"
	"errors"
	"strings"
	"time"

	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/dberror"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
)

// Repo 封装游客会话与用量账本的数据访问。SQLite 与 PostgreSQL 共用同一实现。
type Repo struct {
	db *gorm.DB
}

// NewRepo 创建游客仓储。
func NewRepo(db *gorm.DB) *Repo {
	return &Repo{db: db}
}

// CreateGuestSession 写入新的游客会话。
func (r *Repo) CreateGuestSession(ctx context.Context, item *domainguest.Session) error {
	if item == nil || strings.TrimSpace(item.PublicID) == "" || strings.TrimSpace(item.CredentialHash) == "" {
		return repository.ErrInvalidInput
	}
	record := model.GuestSession{
		PublicID:       strings.TrimSpace(item.PublicID),
		CredentialHash: strings.TrimSpace(item.CredentialHash),
		Status:         strings.TrimSpace(item.Status),
		LastSeenAt:     item.LastSeenAt,
		ExpiresAt:      item.ExpiresAt,
		RevokedAt:      item.RevokedAt,
		IPHash:         strings.TrimSpace(item.IPHash),
		UserAgentHash:  strings.TrimSpace(item.UserAgentHash),
	}
	if record.Status == "" {
		record.Status = domainguest.SessionStatusActive
	}
	if err := r.db.WithContext(ctx).Create(&record).Error; err != nil {
		return dberror.Translate(err)
	}
	item.ID = record.ID
	item.CreatedAt = record.CreatedAt
	item.UpdatedAt = record.UpdatedAt
	return nil
}

// GetGuestSessionByCredentialHash 按凭证摘要查询会话。
func (r *Repo) GetGuestSessionByCredentialHash(ctx context.Context, credentialHash string) (*domainguest.Session, error) {
	trimmed := strings.TrimSpace(credentialHash)
	if trimmed == "" {
		return nil, repository.ErrInvalidInput
	}
	var record model.GuestSession
	if err := r.db.WithContext(ctx).
		Where("credential_hash = ?", trimmed).
		First(&record).Error; err != nil {
		return nil, dberror.Translate(err)
	}
	result := toDomainSession(record)
	return &result, nil
}

// TouchGuestSession 更新会话最近使用时间。
func (r *Repo) TouchGuestSession(ctx context.Context, sessionID uint, seenAt time.Time) error {
	if sessionID == 0 || seenAt.IsZero() {
		return repository.ErrInvalidInput
	}
	result := r.db.WithContext(ctx).
		Model(&model.GuestSession{}).
		Where("id = ?", sessionID).
		Update("last_seen_at", seenAt)
	if result.Error != nil {
		return dberror.Translate(result.Error)
	}
	if result.RowsAffected == 0 {
		return repository.ErrNotFound
	}
	return nil
}

// GetGuestDailyUsage 返回单个游客在预算日内的用量。
func (r *Repo) GetGuestDailyUsage(ctx context.Context, sessionID uint, budgetDate string) (*domainguest.DailyUsage, error) {
	if sessionID == 0 {
		return nil, repository.ErrInvalidInput
	}
	date := strings.TrimSpace(budgetDate)
	if date == "" {
		return nil, repository.ErrInvalidInput
	}
	var record model.GuestDailyUsage
	err := r.db.WithContext(ctx).
		Where("session_id = ? AND budget_date = ?", sessionID, date).
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &domainguest.DailyUsage{SessionID: sessionID, BudgetDate: date}, nil
		}
		return nil, dberror.Translate(err)
	}
	return &domainguest.DailyUsage{
		SessionID:       record.SessionID,
		BudgetDate:      record.BudgetDate,
		SettledNanousd:  record.SettledNanousd,
		ReservedNanousd: record.ReservedNanousd,
		RequestCount:    record.RequestCount,
		UpdatedAt:       record.UpdatedAt,
	}, nil
}

// GetGuestGlobalDailyUsage 返回全站游客在预算日内的用量。
func (r *Repo) GetGuestGlobalDailyUsage(ctx context.Context, budgetDate string) (*domainguest.GlobalDailyUsage, error) {
	date := strings.TrimSpace(budgetDate)
	if date == "" {
		return nil, repository.ErrInvalidInput
	}
	var record model.GuestGlobalDailyUsage
	err := r.db.WithContext(ctx).
		Where("budget_date = ?", date).
		First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &domainguest.GlobalDailyUsage{BudgetDate: date}, nil
		}
		return nil, dberror.Translate(err)
	}
	return &domainguest.GlobalDailyUsage{
		BudgetDate:      record.BudgetDate,
		SettledNanousd:  record.SettledNanousd,
		ReservedNanousd: record.ReservedNanousd,
		RequestCount:    record.RequestCount,
		UpdatedAt:       record.UpdatedAt,
	}, nil
}

func toDomainSession(record model.GuestSession) domainguest.Session {
	return domainguest.Session{
		ID:             record.ID,
		PublicID:       record.PublicID,
		CredentialHash: record.CredentialHash,
		Status:         record.Status,
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
		LastSeenAt:     record.LastSeenAt,
		ExpiresAt:      record.ExpiresAt,
		RevokedAt:      record.RevokedAt,
		IPHash:         record.IPHash,
		UserAgentHash:  record.UserAgentHash,
	}
}
