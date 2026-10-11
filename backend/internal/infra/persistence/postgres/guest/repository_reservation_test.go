package guest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
	model "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/persistence/models"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const guestTestBudgetDate = "2026-10-11"

func newGuestTestRepo(t *testing.T, maxOpenConns int) *Repo {
	t.Helper()
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := gorm.Open(sqlite.Open("file:"+dbName+"?mode=memory&cache=shared&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	// SQLite 生产配置使用单连接（config.yaml: sqlite.max_open_conns=1），
	// 因此这里也按单连接串行事务验证原子性。
	sqlDB.SetMaxOpenConns(maxOpenConns)
	if err := db.AutoMigrate(
		&model.GuestSession{},
		&model.GuestDailyUsage{},
		&model.GuestGlobalDailyUsage{},
		&model.GuestRequest{},
	); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return NewRepo(db)
}

func guestReservationInput(sessionID uint, requestID string, estimate int64) domainguest.ReservationRequest {
	return domainguest.ReservationRequest{
		SessionID:       sessionID,
		RequestID:       requestID,
		BudgetDate:      guestTestBudgetDate,
		ModelRef:        "demo-chat",
		InputDigest:     "digest-" + requestID,
		BillingRefNo:    "guest-ref-" + requestID,
		EstimateNanousd: estimate,
		Now:             time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC),
	}
}

func TestReserveGuestRequestEnforcesPerVisitorAndGlobalBudget(t *testing.T) {
	repo := newGuestTestRepo(t, 1)
	ctx := context.Background()
	limits := domainguest.ReservationLimits{PerVisitorDailyNanousd: 250, GlobalDailyNanousd: 1000, DailyGenerationLimit: 10}

	for index := 0; index < 2; index++ {
		if _, err := repo.ReserveGuestRequest(ctx, guestReservationInput(1, fmt.Sprintf("req-%d", index), 100), limits); err != nil {
			t.Fatalf("reserve %d: %v", index, err)
		}
	}
	if _, err := repo.ReserveGuestRequest(ctx, guestReservationInput(1, "req-3", 100), limits); !errors.Is(err, domainguest.ErrDailyBudgetExceeded) {
		t.Fatalf("expected per-visitor budget error, got %v", err)
	}

	usage, err := repo.GetGuestDailyUsage(ctx, 1, guestTestBudgetDate)
	if err != nil {
		t.Fatalf("daily usage: %v", err)
	}
	if usage.ReservedNanousd != 200 || usage.SettledNanousd != 0 || usage.RequestCount != 2 {
		t.Fatalf("unexpected per-visitor usage after rejection: %#v", usage)
	}
	global, err := repo.GetGuestGlobalDailyUsage(ctx, guestTestBudgetDate)
	if err != nil {
		t.Fatalf("global usage: %v", err)
	}
	if global.ReservedNanousd != 200 || global.RequestCount != 2 {
		t.Fatalf("unexpected global usage after rejection: %#v", global)
	}
	if _, err := repo.GetGuestRequest(ctx, 1, "req-3"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("rejected request must not be recorded, got %v", err)
	}

	if _, err := repo.ReserveGuestRequest(ctx, guestReservationInput(2, "other-1", 100), limits); err != nil {
		t.Fatalf("second visitor reserve: %v", err)
	}
	globalLimits := domainguest.ReservationLimits{PerVisitorDailyNanousd: 1000, GlobalDailyNanousd: 350, DailyGenerationLimit: 10}
	if _, err := repo.ReserveGuestRequest(ctx, guestReservationInput(2, "other-2", 100), globalLimits); !errors.Is(err, domainguest.ErrGlobalBudgetExceeded) {
		t.Fatalf("expected global budget error, got %v", err)
	}
}

func TestReserveGuestRequestEnforcesDailyCount(t *testing.T) {
	repo := newGuestTestRepo(t, 1)
	ctx := context.Background()
	limits := domainguest.ReservationLimits{PerVisitorDailyNanousd: 1_000_000, GlobalDailyNanousd: 1_000_000, DailyGenerationLimit: 1}

	if _, err := repo.ReserveGuestRequest(ctx, guestReservationInput(1, "count-1", 100), limits); err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	if _, err := repo.ReserveGuestRequest(ctx, guestReservationInput(1, "count-2", 100), limits); !errors.Is(err, domainguest.ErrDailyCountExceeded) {
		t.Fatalf("expected daily count error, got %v", err)
	}
	usage, err := repo.GetGuestDailyUsage(ctx, 1, guestTestBudgetDate)
	if err != nil {
		t.Fatalf("daily usage: %v", err)
	}
	if usage.RequestCount != 1 || usage.ReservedNanousd != 100 {
		t.Fatalf("unexpected usage after count rejection: %#v", usage)
	}
}

func TestReserveGuestRequestRejectsDuplicateRequestID(t *testing.T) {
	repo := newGuestTestRepo(t, 1)
	ctx := context.Background()
	limits := domainguest.ReservationLimits{PerVisitorDailyNanousd: 1_000_000, GlobalDailyNanousd: 1_000_000, DailyGenerationLimit: 10}

	first, err := repo.ReserveGuestRequest(ctx, guestReservationInput(1, "dup-1", 100), limits)
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	if first.Status != domainguest.RequestStatusPending || first.BillingRefNo != "guest-ref-dup-1" {
		t.Fatalf("unexpected record: %#v", first)
	}
	if _, err := repo.ReserveGuestRequest(ctx, guestReservationInput(1, "dup-1", 100), limits); !errors.Is(err, repository.ErrDuplicate) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	usage, err := repo.GetGuestDailyUsage(ctx, 1, guestTestBudgetDate)
	if err != nil {
		t.Fatalf("daily usage: %v", err)
	}
	if usage.ReservedNanousd != 100 || usage.RequestCount != 1 {
		t.Fatalf("duplicate request must not double reserve: %#v", usage)
	}
}

func TestSettleGuestRequestMovesReservationToSettledAmount(t *testing.T) {
	repo := newGuestTestRepo(t, 1)
	ctx := context.Background()
	limits := domainguest.ReservationLimits{PerVisitorDailyNanousd: 1_000_000, GlobalDailyNanousd: 1_000_000, DailyGenerationLimit: 10}
	if _, err := repo.ReserveGuestRequest(ctx, guestReservationInput(1, "settle-1", 100), limits); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	now := time.Date(2026, time.October, 11, 12, 30, 0, 0, time.UTC)
	if err := repo.SettleGuestRequest(ctx, 1, "settle-1", 60, domainguest.RequestStatusSettled, now); err != nil {
		t.Fatalf("settle: %v", err)
	}

	usage, err := repo.GetGuestDailyUsage(ctx, 1, guestTestBudgetDate)
	if err != nil {
		t.Fatalf("daily usage: %v", err)
	}
	if usage.ReservedNanousd != 0 || usage.SettledNanousd != 60 || usage.RequestCount != 1 {
		t.Fatalf("unexpected per-visitor usage after settle: %#v", usage)
	}
	global, err := repo.GetGuestGlobalDailyUsage(ctx, guestTestBudgetDate)
	if err != nil {
		t.Fatalf("global usage: %v", err)
	}
	if global.ReservedNanousd != 0 || global.SettledNanousd != 60 {
		t.Fatalf("unexpected global usage after settle: %#v", global)
	}
	record, err := repo.GetGuestRequest(ctx, 1, "settle-1")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if record.Status != domainguest.RequestStatusSettled || record.SettledNanousd != 60 || record.SettledAt == nil {
		t.Fatalf("unexpected request after settle: %#v", record)
	}
	if err := repo.SettleGuestRequest(ctx, 1, "settle-1", 10, domainguest.RequestStatusSettled, now); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected conflict on terminal request, got %v", err)
	}
}

func TestReleaseGuestRequestReturnsBudgetAndOptionallyCount(t *testing.T) {
	repo := newGuestTestRepo(t, 1)
	ctx := context.Background()
	limits := domainguest.ReservationLimits{PerVisitorDailyNanousd: 1_000_000, GlobalDailyNanousd: 1_000_000, DailyGenerationLimit: 10}

	if _, err := repo.ReserveGuestRequest(ctx, guestReservationInput(1, "release-1", 100), limits); err != nil {
		t.Fatalf("reserve: %v", err)
	}
	now := time.Date(2026, time.October, 11, 13, 0, 0, 0, time.UTC)
	if err := repo.ReleaseGuestRequest(ctx, 1, "release-1", true, domainguest.RequestStatusRejected, now); err != nil {
		t.Fatalf("release: %v", err)
	}
	usage, err := repo.GetGuestDailyUsage(ctx, 1, guestTestBudgetDate)
	if err != nil {
		t.Fatalf("daily usage: %v", err)
	}
	if usage.ReservedNanousd != 0 || usage.SettledNanousd != 0 || usage.RequestCount != 0 {
		t.Fatalf("release with count must free everything: %#v", usage)
	}
	if err := repo.ReleaseGuestRequest(ctx, 1, "release-1", true, domainguest.RequestStatusFailed, now); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("expected conflict on second release, got %v", err)
	}

	// 已经调用上游但失败：释放预算但保留当日次数占用。
	if _, err := repo.ReserveGuestRequest(ctx, guestReservationInput(1, "release-2", 100), limits); err != nil {
		t.Fatalf("reserve 2: %v", err)
	}
	if err := repo.ReleaseGuestRequest(ctx, 1, "release-2", false, domainguest.RequestStatusFailed, now); err != nil {
		t.Fatalf("release 2: %v", err)
	}
	usage, err = repo.GetGuestDailyUsage(ctx, 1, guestTestBudgetDate)
	if err != nil {
		t.Fatalf("daily usage 2: %v", err)
	}
	if usage.ReservedNanousd != 0 || usage.RequestCount != 1 {
		t.Fatalf("release without count must keep the generation count: %#v", usage)
	}
}

func TestConcurrentReserveNeverExceedsDailyBudget(t *testing.T) {
	repo := newGuestTestRepo(t, 1)
	ctx := context.Background()
	limits := domainguest.ReservationLimits{PerVisitorDailyNanousd: 300, GlobalDailyNanousd: 300, DailyGenerationLimit: 10}

	const attempts = 8
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		accepted int
		rejected int
	)
	for index := 0; index < attempts; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := repo.ReserveGuestRequest(ctx, guestReservationInput(1, fmt.Sprintf("race-%d", index), 100), limits)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				accepted++
			case errors.Is(err, domainguest.ErrDailyBudgetExceeded), errors.Is(err, domainguest.ErrGlobalBudgetExceeded):
				rejected++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(index)
	}
	wg.Wait()

	if accepted != 3 || rejected != attempts-3 {
		t.Fatalf("expected 3 accepted and 5 rejected, got accepted=%d rejected=%d", accepted, rejected)
	}
	usage, err := repo.GetGuestDailyUsage(ctx, 1, guestTestBudgetDate)
	if err != nil {
		t.Fatalf("daily usage: %v", err)
	}
	if usage.ReservedNanousd != 300 || usage.RequestCount != 3 {
		t.Fatalf("budget must not be exceeded: %#v", usage)
	}
	global, err := repo.GetGuestGlobalDailyUsage(ctx, guestTestBudgetDate)
	if err != nil {
		t.Fatalf("global usage: %v", err)
	}
	if global.ReservedNanousd != 300 {
		t.Fatalf("global budget must not be exceeded: %#v", global)
	}
}

func TestGuestRequestCountersForRateLimitAndConcurrency(t *testing.T) {
	repo := newGuestTestRepo(t, 1)
	ctx := context.Background()
	limits := domainguest.ReservationLimits{PerVisitorDailyNanousd: 1_000_000, GlobalDailyNanousd: 1_000_000, DailyGenerationLimit: 10}
	for index := 0; index < 2; index++ {
		if _, err := repo.ReserveGuestRequest(ctx, guestReservationInput(1, fmt.Sprintf("counter-%d", index), 10), limits); err != nil {
			t.Fatalf("reserve %d: %v", index, err)
		}
	}
	since := time.Date(2026, time.October, 11, 11, 59, 0, 0, time.UTC)
	count, err := repo.CountGuestRequestsSince(ctx, 1, since)
	if err != nil {
		t.Fatalf("count since: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 recent requests, got %d", count)
	}
	active, err := repo.CountActiveGuestRequests(ctx, 1)
	if err != nil {
		t.Fatalf("active count: %v", err)
	}
	if active != 2 {
		t.Fatalf("expected 2 active requests, got %d", active)
	}
	activeGlobal, err := repo.CountActiveGuestRequestsGlobal(ctx)
	if err != nil {
		t.Fatalf("global active count: %v", err)
	}
	if activeGlobal != 2 {
		t.Fatalf("expected 2 active requests globally, got %d", activeGlobal)
	}
	if err := repo.SettleGuestRequest(ctx, 1, "counter-0", 10, domainguest.RequestStatusSettled, time.Now().UTC()); err != nil {
		t.Fatalf("settle: %v", err)
	}
	active, err = repo.CountActiveGuestRequests(ctx, 1)
	if err != nil {
		t.Fatalf("active count after settle: %v", err)
	}
	if active != 1 {
		t.Fatalf("expected 1 active request after settle, got %d", active)
	}
}
