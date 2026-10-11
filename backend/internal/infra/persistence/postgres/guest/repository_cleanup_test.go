package guest

import (
	"context"
	"testing"
	"time"

	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
)

func TestDeleteGuestRecordsBeforeKeepsPendingAndCurrentUsage(t *testing.T) {
	repo := newGuestTestRepo(t, 1)
	ctx := context.Background()
	db := repo.db

	oldDate := "2026-09-01"
	today := "2026-10-11"
	cutoffTime := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)

	sessions := []model.GuestSession{
		{PublicID: "old-session", CredentialHash: "hash-old", Status: domainguest.SessionStatusActive, LastSeenAt: cutoffTime.AddDate(0, 0, -40), ExpiresAt: cutoffTime.AddDate(0, 0, -1)},
		{PublicID: "active-session", CredentialHash: "hash-active", Status: domainguest.SessionStatusActive, LastSeenAt: cutoffTime, ExpiresAt: cutoffTime.AddDate(0, 0, 7)},
	}
	if err := db.Create(&sessions).Error; err != nil {
		t.Fatalf("create sessions: %v", err)
	}
	oldID := sessions[0].ID
	activeID := sessions[1].ID

	requests := []model.GuestRequest{
		{RequestID: "old-settled", SessionID: oldID, BudgetDate: oldDate, Status: domainguest.RequestStatusSettled, ReservedNanousd: 10, SettledNanousd: 10},
		{RequestID: "old-pending", SessionID: oldID, BudgetDate: oldDate, Status: domainguest.RequestStatusPending, ReservedNanousd: 10},
		{RequestID: "old-needs-review", SessionID: oldID, BudgetDate: oldDate, Status: domainguest.RequestStatusNeedsReview, ReservedNanousd: 10},
		{RequestID: "today-settled", SessionID: activeID, BudgetDate: today, Status: domainguest.RequestStatusSettled, ReservedNanousd: 10, SettledNanousd: 10},
	}
	if err := db.Create(&requests).Error; err != nil {
		t.Fatalf("create requests: %v", err)
	}

	usage := []model.GuestDailyUsage{
		{SessionID: oldID, BudgetDate: oldDate, SettledNanousd: 10, RequestCount: 1},
		{SessionID: activeID, BudgetDate: today, SettledNanousd: 10, RequestCount: 1},
	}
	if err := db.Create(&usage).Error; err != nil {
		t.Fatalf("create usage: %v", err)
	}
	global := []model.GuestGlobalDailyUsage{
		{BudgetDate: oldDate, SettledNanousd: 10, RequestCount: 1},
		{BudgetDate: today, SettledNanousd: 10, RequestCount: 1},
	}
	if err := db.Create(&global).Error; err != nil {
		t.Fatalf("create global usage: %v", err)
	}

	result, err := repo.DeleteGuestRecordsBefore(ctx, cutoffTime.Format("2006-01-02"), cutoffTime)
	if err != nil {
		t.Fatalf("delete before: %v", err)
	}
	if result.DeletedRequests != 1 {
		t.Fatalf("expected only the old settled request to be deleted, got %d", result.DeletedRequests)
	}
	if result.DeletedUsage != 2 {
		t.Fatalf("expected one daily + one global usage row deleted, got %d", result.DeletedUsage)
	}
	if result.DeletedSessions != 1 {
		t.Fatalf("expected only the expired session to be deleted, got %d", result.DeletedSessions)
	}

	var remainingRequests []string
	if err := db.Model(&model.GuestRequest{}).Pluck("request_id", &remainingRequests).Error; err != nil {
		t.Fatalf("list requests: %v", err)
	}
	if len(remainingRequests) != 3 {
		t.Fatalf("expected pending, needs_review and today's request to survive, got %v", remainingRequests)
	}
	for _, id := range remainingRequests {
		if id == "old-settled" {
			t.Fatalf("old settled request must be deleted, got %v", remainingRequests)
		}
	}

	var usageCount int64
	if err := db.Model(&model.GuestDailyUsage{}).Count(&usageCount).Error; err != nil {
		t.Fatalf("count usage: %v", err)
	}
	if usageCount != 1 {
		t.Fatalf("expected today's daily usage to survive, got %d", usageCount)
	}
	var globalCount int64
	if err := db.Model(&model.GuestGlobalDailyUsage{}).Count(&globalCount).Error; err != nil {
		t.Fatalf("count global usage: %v", err)
	}
	if globalCount != 1 {
		t.Fatalf("expected today's global usage to survive, got %d", globalCount)
	}
	var sessionCount int64
	if err := db.Model(&model.GuestSession{}).Count(&sessionCount).Error; err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessionCount != 1 {
		t.Fatalf("expected the active session to survive, got %d", sessionCount)
	}
}
