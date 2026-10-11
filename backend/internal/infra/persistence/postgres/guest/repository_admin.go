package guest

import (
	"context"
	"strings"

	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/dberror"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/gorm"
)

// ListGuestDailyUsage 按预算日返回各游客的用量（按已结算金额倒序）。
func (r *Repo) ListGuestDailyUsage(ctx context.Context, budgetDate string, limit int) ([]domainguest.UsageRow, error) {
	date := strings.TrimSpace(budgetDate)
	if date == "" {
		return nil, repository.ErrInvalidInput
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	type usageRow struct {
		SessionID       uint
		GuestPublicID   string
		BudgetDate      string
		SettledNanousd  int64
		ReservedNanousd int64
		RequestCount    int
	}
	rows := make([]usageRow, 0, limit)
	if err := r.db.WithContext(ctx).
		Model(&model.GuestDailyUsage{}).
		Select("guest_daily_usage.session_id, guest_sessions.public_id AS guest_public_id, guest_daily_usage.budget_date, guest_daily_usage.settled_nanousd, guest_daily_usage.reserved_nanousd, guest_daily_usage.request_count").
		Joins("LEFT JOIN guest_sessions ON guest_sessions.id = guest_daily_usage.session_id").
		Where("guest_daily_usage.budget_date = ?", date).
		Order("guest_daily_usage.settled_nanousd DESC, guest_daily_usage.reserved_nanousd DESC, guest_daily_usage.id ASC").
		Limit(limit).
		Scan(&rows).Error; err != nil {
		return nil, dberror.Translate(err)
	}
	results := make([]domainguest.UsageRow, 0, len(rows))
	for _, row := range rows {
		results = append(results, domainguest.UsageRow{
			SessionID:       row.SessionID,
			GuestPublicID:   row.GuestPublicID,
			BudgetDate:      row.BudgetDate,
			SettledNanousd:  row.SettledNanousd,
			ReservedNanousd: row.ReservedNanousd,
			RequestCount:    row.RequestCount,
		})
	}
	return results, nil
}

// ListGuestRequests 分页返回匿名账单记录（不含正文）与总数。
func (r *Repo) ListGuestRequests(ctx context.Context, filter domainguest.RequestFilter) ([]domainguest.RequestRecord, int64, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	buildQuery := func() *gorm.DB {
		query := r.db.WithContext(ctx).Model(&model.GuestRequest{})
		if filter.SessionID != 0 {
			query = query.Where("session_id = ?", filter.SessionID)
		}
		if status := strings.TrimSpace(filter.Status); status != "" {
			query = query.Where("status = ?", status)
		}
		if date := strings.TrimSpace(filter.BudgetDate); date != "" {
			query = query.Where("budget_date = ?", date)
		}
		return query
	}
	var total int64
	if err := buildQuery().Count(&total).Error; err != nil {
		return nil, 0, dberror.Translate(err)
	}
	type requestRow struct {
		model.GuestRequest
		GuestPublicID string `gorm:"column:guest_public_id"`
	}
	rows := make([]requestRow, 0, limit)
	if err := buildQuery().
		Select("guest_requests.*, guest_sessions.public_id AS guest_public_id").
		Joins("LEFT JOIN guest_sessions ON guest_sessions.id = guest_requests.session_id").
		Order("guest_requests.id DESC").
		Limit(limit).
		Offset(offset).
		Scan(&rows).Error; err != nil {
		return nil, 0, dberror.Translate(err)
	}
	results := make([]domainguest.RequestRecord, 0, len(rows))
	for _, row := range rows {
		record := toDomainRequest(row.GuestRequest)
		record.GuestPublicID = row.GuestPublicID
		results = append(results, record)
	}
	return results, total, nil
}
