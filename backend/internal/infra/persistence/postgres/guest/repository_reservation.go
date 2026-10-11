package guest

import (
	"context"
	"strings"
	"time"

	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/dberror"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReserveGuestRequest 在单个事务内校验并占用次数、个人预算与全站预算。
//
// 请求记录与两个预算日行共享同一事务：任一步失败都会整体回滚，避免“扣了个人预算没扣全站预算”
// 或“写了请求记录没占额度”。并发请求通过行锁（PostgreSQL）或单连接串行（SQLite）互斥，
// 因此同一份预算不会被多个请求同时穿透。
func (r *Repo) ReserveGuestRequest(ctx context.Context, input domainguest.ReservationRequest, limits domainguest.ReservationLimits) (*domainguest.RequestRecord, error) {
	requestID := strings.TrimSpace(input.RequestID)
	budgetDate := strings.TrimSpace(input.BudgetDate)
	if input.SessionID == 0 || requestID == "" || budgetDate == "" || input.EstimateNanousd < 0 {
		return nil, repository.ErrInvalidInput
	}
	now := normalizeGuestNow(input.Now)
	record := model.GuestRequest{
		RequestID:       requestID,
		SessionID:       input.SessionID,
		BudgetDate:      budgetDate,
		Status:          domainguest.RequestStatusPending,
		ModelRef:        strings.TrimSpace(input.ModelRef),
		InputDigest:     strings.TrimSpace(input.InputDigest),
		BillingRefNo:    strings.TrimSpace(input.BillingRefNo),
		ReservedNanousd: input.EstimateNanousd,
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&record).Error; err != nil {
			if dberror.IsUniqueConstraint(err) {
				return repository.ErrDuplicate
			}
			return dberror.Translate(err)
		}
		personal, err := lockGuestDailyUsage(tx, input.SessionID, budgetDate)
		if err != nil {
			return err
		}
		global, err := lockGuestGlobalDailyUsage(tx, budgetDate)
		if err != nil {
			return err
		}
		if personal.RequestCount+1 > limits.DailyGenerationLimit {
			return domainguest.ErrDailyCountExceeded
		}
		if personal.SettledNanousd+personal.ReservedNanousd+input.EstimateNanousd > limits.PerVisitorDailyNanousd {
			return domainguest.ErrDailyBudgetExceeded
		}
		if global.SettledNanousd+global.ReservedNanousd+input.EstimateNanousd > limits.GlobalDailyNanousd {
			return domainguest.ErrGlobalBudgetExceeded
		}
		if err := tx.Model(&model.GuestDailyUsage{}).Where("id = ?", personal.ID).Updates(map[string]any{
			"reserved_nanousd": personal.ReservedNanousd + input.EstimateNanousd,
			"request_count":    personal.RequestCount + 1,
			"updated_at":       now,
		}).Error; err != nil {
			return dberror.Translate(err)
		}
		if err := tx.Model(&model.GuestGlobalDailyUsage{}).Where("id = ?", global.ID).Updates(map[string]any{
			"reserved_nanousd": global.ReservedNanousd + input.EstimateNanousd,
			"request_count":    global.RequestCount + 1,
			"updated_at":       now,
		}).Error; err != nil {
			return dberror.Translate(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	record.UpdatedAt = now
	result := toDomainRequest(record)
	return &result, nil
}

// GetGuestRequest 读取一次游客请求记录。
func (r *Repo) GetGuestRequest(ctx context.Context, sessionID uint, requestID string) (*domainguest.RequestRecord, error) {
	if sessionID == 0 || strings.TrimSpace(requestID) == "" {
		return nil, repository.ErrInvalidInput
	}
	var record model.GuestRequest
	if err := r.db.WithContext(ctx).
		Where("session_id = ? AND request_id = ?", sessionID, strings.TrimSpace(requestID)).
		First(&record).Error; err != nil {
		return nil, dberror.Translate(err)
	}
	result := toDomainRequest(record)
	return &result, nil
}

// SettleGuestRequest 把预留转为实际结算。
func (r *Repo) SettleGuestRequest(ctx context.Context, sessionID uint, requestID string, settledNanousd int64, status string, now time.Time) error {
	if sessionID == 0 || strings.TrimSpace(requestID) == "" || settledNanousd < 0 {
		return repository.ErrInvalidInput
	}
	normalized := normalizeGuestNow(now)
	requestStatus := strings.TrimSpace(status)
	if requestStatus == "" {
		requestStatus = domainguest.RequestStatusSettled
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record, err := lockGuestRequest(tx, sessionID, requestID)
		if err != nil {
			return err
		}
		if isTerminalGuestRequestStatus(record.Status) {
			return repository.ErrConflict
		}
		personal, err := lockGuestDailyUsage(tx, record.SessionID, record.BudgetDate)
		if err != nil {
			return err
		}
		global, err := lockGuestGlobalDailyUsage(tx, record.BudgetDate)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.GuestDailyUsage{}).Where("id = ?", personal.ID).Updates(map[string]any{
			"reserved_nanousd": remainingGuestReservation(personal.ReservedNanousd, record.ReservedNanousd),
			"settled_nanousd":  personal.SettledNanousd + settledNanousd,
			"updated_at":       normalized,
		}).Error; err != nil {
			return dberror.Translate(err)
		}
		if err := tx.Model(&model.GuestGlobalDailyUsage{}).Where("id = ?", global.ID).Updates(map[string]any{
			"reserved_nanousd": remainingGuestReservation(global.ReservedNanousd, record.ReservedNanousd),
			"settled_nanousd":  global.SettledNanousd + settledNanousd,
			"updated_at":       normalized,
		}).Error; err != nil {
			return dberror.Translate(err)
		}
		return tx.Model(&model.GuestRequest{}).Where("id = ?", record.ID).Updates(map[string]any{
			"status":          requestStatus,
			"settled_nanousd": settledNanousd,
			"settled_at":      normalized,
			"updated_at":      normalized,
		}).Error
	})
}

// ReleaseGuestRequest 释放预留；releaseCount 为真时同时归还当日次数。
func (r *Repo) ReleaseGuestRequest(ctx context.Context, sessionID uint, requestID string, releaseCount bool, status string, now time.Time) error {
	if sessionID == 0 || strings.TrimSpace(requestID) == "" {
		return repository.ErrInvalidInput
	}
	normalized := normalizeGuestNow(now)
	requestStatus := strings.TrimSpace(status)
	if requestStatus == "" {
		requestStatus = domainguest.RequestStatusFailed
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record, err := lockGuestRequest(tx, sessionID, requestID)
		if err != nil {
			return err
		}
		if isTerminalGuestRequestStatus(record.Status) {
			return repository.ErrConflict
		}
		personal, err := lockGuestDailyUsage(tx, record.SessionID, record.BudgetDate)
		if err != nil {
			return err
		}
		global, err := lockGuestGlobalDailyUsage(tx, record.BudgetDate)
		if err != nil {
			return err
		}
		personalCount := personal.RequestCount
		globalCount := global.RequestCount
		if releaseCount {
			if personalCount > 0 {
				personalCount--
			}
			if globalCount > 0 {
				globalCount--
			}
		}
		if err := tx.Model(&model.GuestDailyUsage{}).Where("id = ?", personal.ID).Updates(map[string]any{
			"reserved_nanousd": remainingGuestReservation(personal.ReservedNanousd, record.ReservedNanousd),
			"request_count":    personalCount,
			"updated_at":       normalized,
		}).Error; err != nil {
			return dberror.Translate(err)
		}
		if err := tx.Model(&model.GuestGlobalDailyUsage{}).Where("id = ?", global.ID).Updates(map[string]any{
			"reserved_nanousd": remainingGuestReservation(global.ReservedNanousd, record.ReservedNanousd),
			"request_count":    globalCount,
			"updated_at":       normalized,
		}).Error; err != nil {
			return dberror.Translate(err)
		}
		return tx.Model(&model.GuestRequest{}).Where("id = ?", record.ID).Updates(map[string]any{
			"status":     requestStatus,
			"updated_at": normalized,
		}).Error
	})
}

// CountGuestRequestsSince 统计某游客在给定时间之后创建的请求数（频率限制）。
func (r *Repo) CountGuestRequestsSince(ctx context.Context, sessionID uint, since time.Time) (int, error) {
	if sessionID == 0 || since.IsZero() {
		return 0, repository.ErrInvalidInput
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.GuestRequest{}).
		Where("session_id = ? AND created_at >= ?", sessionID, since).
		Count(&count).Error; err != nil {
		return 0, dberror.Translate(err)
	}
	return int(count), nil
}

// CountActiveGuestRequests 统计某游客仍在进行中的请求数（单游客并发限制）。
func (r *Repo) CountActiveGuestRequests(ctx context.Context, sessionID uint) (int, error) {
	if sessionID == 0 {
		return 0, repository.ErrInvalidInput
	}
	return r.countActiveGuestRequests(ctx, "session_id = ?", sessionID)
}

// CountActiveGuestRequestsGlobal 统计全站进行中的游客请求数（全站并发限制）。
func (r *Repo) CountActiveGuestRequestsGlobal(ctx context.Context) (int, error) {
	return r.countActiveGuestRequests(ctx, "", nil)
}

func (r *Repo) countActiveGuestRequests(ctx context.Context, condition string, args ...any) (int, error) {
	query := r.db.WithContext(ctx).Model(&model.GuestRequest{}).
		Where("status IN ?", []string{domainguest.RequestStatusPending, domainguest.RequestStatusRunning})
	if strings.TrimSpace(condition) != "" {
		query = query.Where(condition, args...)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, dberror.Translate(err)
	}
	return int(count), nil
}

func lockGuestRequest(tx *gorm.DB, sessionID uint, requestID string) (*model.GuestRequest, error) {
	var record model.GuestRequest
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("session_id = ? AND request_id = ?", sessionID, strings.TrimSpace(requestID)).
		First(&record).Error; err != nil {
		return nil, dberror.Translate(err)
	}
	return &record, nil
}

func lockGuestDailyUsage(tx *gorm.DB, sessionID uint, budgetDate string) (*model.GuestDailyUsage, error) {
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}, {Name: "budget_date"}},
		DoNothing: true,
	}).Create(&model.GuestDailyUsage{SessionID: sessionID, BudgetDate: budgetDate}).Error; err != nil {
		return nil, dberror.Translate(err)
	}
	var record model.GuestDailyUsage
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("session_id = ? AND budget_date = ?", sessionID, budgetDate).
		First(&record).Error; err != nil {
		return nil, dberror.Translate(err)
	}
	return &record, nil
}

func lockGuestGlobalDailyUsage(tx *gorm.DB, budgetDate string) (*model.GuestGlobalDailyUsage, error) {
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "budget_date"}},
		DoNothing: true,
	}).Create(&model.GuestGlobalDailyUsage{BudgetDate: budgetDate}).Error; err != nil {
		return nil, dberror.Translate(err)
	}
	var record model.GuestGlobalDailyUsage
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("budget_date = ?", budgetDate).
		First(&record).Error; err != nil {
		return nil, dberror.Translate(err)
	}
	return &record, nil
}

func toDomainRequest(record model.GuestRequest) domainguest.RequestRecord {
	return domainguest.RequestRecord{
		ID:              record.ID,
		RequestID:       record.RequestID,
		SessionID:       record.SessionID,
		BudgetDate:      record.BudgetDate,
		Status:          record.Status,
		ModelRef:        record.ModelRef,
		InputDigest:     record.InputDigest,
		BillingRefNo:    record.BillingRefNo,
		ReservedNanousd: record.ReservedNanousd,
		SettledNanousd:  record.SettledNanousd,
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
		SettledAt:       record.SettledAt,
	}
}

func normalizeGuestNow(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func remainingGuestReservation(current int64, released int64) int64 {
	next := current - released
	if next < 0 {
		return 0
	}
	return next
}

func isTerminalGuestRequestStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case domainguest.RequestStatusSettled,
		domainguest.RequestStatusFailed,
		domainguest.RequestStatusNeedsReview,
		domainguest.RequestStatusRejected:
		return true
	default:
		return false
	}
}
