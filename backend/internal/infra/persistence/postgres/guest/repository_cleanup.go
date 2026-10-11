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
)

// terminalGuestStatuses 是可以被保留期清理的终态；进行中与待核对状态永不清理。
var terminalGuestStatuses = []string{
	domainguest.RequestStatusSettled,
	domainguest.RequestStatusFailed,
	domainguest.RequestStatusRejected,
}

// DeleteGuestRecordsBefore 清理超过保留期的游客记录。
//
// 规则（对应实施文档“日志清理不删除待结算数据，也不破坏当前额度统计”）：
//   - 只删除终态请求；pending / running / needs_review 一律保留，等待结算或对账；
//   - 只删除早于保留窗口的用量行，当日与仍在保留期内的用量统计不受影响；
//   - 只删除早已过期的会话，活跃会话不受影响。
//
// 三个删除在同一事务中完成，失败时整体回滚。
func (r *Repo) DeleteGuestRecordsBefore(ctx context.Context, cutoffDate string, cutoffTime time.Time) (domainguest.CleanupResult, error) {
	date := strings.TrimSpace(cutoffDate)
	result := domainguest.CleanupResult{CutoffDate: date}
	if date == "" || cutoffTime.IsZero() {
		return result, repository.ErrInvalidInput
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		requests := tx.Where("budget_date < ? AND status IN ?", date, terminalGuestStatuses).Delete(&model.GuestRequest{})
		if requests.Error != nil {
			return dberror.Translate(requests.Error)
		}
		result.DeletedRequests = requests.RowsAffected

		usage := tx.Where("budget_date < ?", date).Delete(&model.GuestDailyUsage{})
		if usage.Error != nil {
			return dberror.Translate(usage.Error)
		}
		global := tx.Where("budget_date < ?", date).Delete(&model.GuestGlobalDailyUsage{})
		if global.Error != nil {
			return dberror.Translate(global.Error)
		}
		result.DeletedUsage = usage.RowsAffected + global.RowsAffected

		sessions := tx.Where("expires_at < ?", cutoffTime).Delete(&model.GuestSession{})
		if sessions.Error != nil {
			return dberror.Translate(sessions.Error)
		}
		result.DeletedSessions = sessions.RowsAffected
		return nil
	})
	if err != nil {
		return domainguest.CleanupResult{CutoffDate: date}, err
	}
	return result, nil
}
