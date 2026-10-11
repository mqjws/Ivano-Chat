package guest

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	appchannel "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/repository"
)

type repoStub struct {
	sessions map[string]*domainguest.Session
	byID     map[uint]*domainguest.Session
	daily    map[uint]*domainguest.DailyUsage
	global   map[string]*domainguest.GlobalDailyUsage
	requests map[string]*domainguest.RequestRecord
	nextID   uint
	created  int

	// 以下字段用于在服务层测试中构造仓储侧行为。
	reserveErr    error
	reserveCalls  int
	lastLimits    domainguest.ReservationLimits
	settleCalls   []string
	releaseCalls  []string
	recentCount   int
	activeCount   int
	activeGlobal  int
	lastReserveIn domainguest.ReservationRequest
}

func newRepoStub() *repoStub {
	return &repoStub{
		sessions: map[string]*domainguest.Session{},
		byID:     map[uint]*domainguest.Session{},
		daily:    map[uint]*domainguest.DailyUsage{},
		global:   map[string]*domainguest.GlobalDailyUsage{},
		requests: map[string]*domainguest.RequestRecord{},
	}
}

func requestKey(sessionID uint, requestID string) string {
	return strconv.FormatUint(uint64(sessionID), 10) + ":" + requestID
}

func (r *repoStub) ReserveGuestRequest(_ context.Context, input domainguest.ReservationRequest, limits domainguest.ReservationLimits) (*domainguest.RequestRecord, error) {
	r.reserveCalls++
	r.lastLimits = limits
	r.lastReserveIn = input
	if r.reserveErr != nil {
		return nil, r.reserveErr
	}
	key := requestKey(input.SessionID, input.RequestID)
	if _, exists := r.requests[key]; exists {
		return nil, repository.ErrDuplicate
	}
	r.nextID++
	record := &domainguest.RequestRecord{
		ID:              r.nextID,
		RequestID:       input.RequestID,
		SessionID:       input.SessionID,
		BudgetDate:      input.BudgetDate,
		Status:          domainguest.RequestStatusPending,
		ModelRef:        input.ModelRef,
		InputDigest:     input.InputDigest,
		BillingRefNo:    input.BillingRefNo,
		ReservedNanousd: input.EstimateNanousd,
		CreatedAt:       input.Now,
		UpdatedAt:       input.Now,
	}
	r.requests[key] = record
	copied := *record
	return &copied, nil
}

func (r *repoStub) GetGuestRequest(_ context.Context, sessionID uint, requestID string) (*domainguest.RequestRecord, error) {
	record, ok := r.requests[requestKey(sessionID, requestID)]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copied := *record
	return &copied, nil
}

func (r *repoStub) SettleGuestRequest(_ context.Context, sessionID uint, requestID string, settledNanousd int64, status string, now time.Time) error {
	record, ok := r.requests[requestKey(sessionID, requestID)]
	if !ok {
		return repository.ErrNotFound
	}
	record.Status = status
	record.SettledNanousd = settledNanousd
	record.SettledAt = &now
	r.settleCalls = append(r.settleCalls, requestID)
	return nil
}

func (r *repoStub) ReleaseGuestRequest(_ context.Context, sessionID uint, requestID string, _ bool, status string, _ time.Time) error {
	record, ok := r.requests[requestKey(sessionID, requestID)]
	if !ok {
		return repository.ErrNotFound
	}
	record.Status = status
	r.releaseCalls = append(r.releaseCalls, requestID)
	return nil
}

func (r *repoStub) CountGuestRequestsSince(_ context.Context, _ uint, _ time.Time) (int, error) {
	return r.recentCount, nil
}

func (r *repoStub) CountActiveGuestRequests(_ context.Context, _ uint) (int, error) {
	return r.activeCount, nil
}

func (r *repoStub) CountActiveGuestRequestsGlobal(_ context.Context) (int, error) {
	return r.activeGlobal, nil
}

func (r *repoStub) ListGuestDailyUsage(_ context.Context, _ string, _ int) ([]domainguest.UsageRow, error) {
	return nil, nil
}

func (r *repoStub) ListGuestRequests(_ context.Context, _ domainguest.RequestFilter) ([]domainguest.RequestRecord, int64, error) {
	return nil, 0, nil
}

func (r *repoStub) CreateGuestSession(_ context.Context, item *domainguest.Session) error {
	r.nextID++
	item.ID = r.nextID
	item.CreatedAt = item.LastSeenAt
	item.UpdatedAt = item.LastSeenAt
	copied := *item
	r.sessions[item.CredentialHash] = &copied
	r.byID[item.ID] = &copied
	r.created++
	return nil
}

func (r *repoStub) GetGuestSessionByCredentialHash(_ context.Context, credentialHash string) (*domainguest.Session, error) {
	session, ok := r.sessions[credentialHash]
	if !ok {
		return nil, repository.ErrNotFound
	}
	copied := *session
	return &copied, nil
}

func (r *repoStub) TouchGuestSession(_ context.Context, sessionID uint, seenAt time.Time) error {
	session, ok := r.byID[sessionID]
	if !ok {
		return repository.ErrNotFound
	}
	session.LastSeenAt = seenAt
	return nil
}

func (r *repoStub) GetGuestDailyUsage(_ context.Context, sessionID uint, budgetDate string) (*domainguest.DailyUsage, error) {
	if usage, ok := r.daily[sessionID]; ok && usage.BudgetDate == budgetDate {
		copied := *usage
		return &copied, nil
	}
	return &domainguest.DailyUsage{SessionID: sessionID, BudgetDate: budgetDate}, nil
}

func (r *repoStub) GetGuestGlobalDailyUsage(_ context.Context, budgetDate string) (*domainguest.GlobalDailyUsage, error) {
	if usage, ok := r.global[budgetDate]; ok {
		copied := *usage
		return &copied, nil
	}
	return &domainguest.GlobalDailyUsage{BudgetDate: budgetDate}, nil
}

type settingsStub struct {
	values map[string]string
	err    error
}

func (s settingsStub) RuntimeValuesByNamespace(_ context.Context, namespace string) (map[string]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	if namespace != SettingsNamespace {
		return map[string]string{}, nil
	}
	copied := make(map[string]string, len(s.values))
	for key, value := range s.values {
		copied[key] = value
	}
	return copied, nil
}

type modelStub struct {
	refs  []string
	views []appchannel.ModelView
	err   error
}

func (m *modelStub) ListGuestModels(_ context.Context, refs []string) ([]appchannel.ModelView, error) {
	m.refs = append([]string(nil), refs...)
	if m.err != nil {
		return nil, m.err
	}
	return append([]appchannel.ModelView(nil), m.views...), nil
}

func newTestService(repo repository.GuestRepository, settings settingsReader, models modelCatalog, now time.Time) *Service {
	service := NewService(repo, settings, models)
	service.now = func() time.Time { return now }
	service.entropy = strings.NewReader(strings.Repeat("x", 128))
	return service
}

func enabledSettings(extra map[string]string) settingsStub {
	values := map[string]string{SettingEnabled: "true"}
	for key, value := range extra {
		values[key] = value
	}
	return settingsStub{values: values}
}

func TestEnsureSessionCreatesHashedCredentialAndRestores(t *testing.T) {
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	repo := newRepoStub()
	service := newTestService(repo, enabledSettings(nil), &modelStub{}, now)

	result, err := service.EnsureSession(context.Background(), "", domainguest.SessionMeta{IPHash: "ip-hash", UserAgentHash: "ua-hash"})
	if err != nil {
		t.Fatalf("EnsureSession() error = %v", err)
	}
	if !result.Created || result.Credential == "" || result.Session == nil {
		t.Fatalf("expected a new session with credential, got %#v", result)
	}
	if result.Session.Status != domainguest.SessionStatusActive {
		t.Fatalf("unexpected status %q", result.Session.Status)
	}
	if result.Session.PublicID == "" || result.Session.PublicID == result.Credential {
		t.Fatalf("public id must be独立于凭证，got %q", result.Session.PublicID)
	}
	if !result.Session.ExpiresAt.Equal(now.Add(SessionTTL)) {
		t.Fatalf("expires at = %s, want %s", result.Session.ExpiresAt, now.Add(SessionTTL))
	}
	if _, exists := repo.sessions[result.Credential]; exists {
		t.Fatal("plaintext credential must never be stored")
	}
	if _, exists := repo.sessions[HashCredential(result.Credential)]; !exists {
		t.Fatal("expected session to be stored under the credential digest")
	}
	if result.Session.IPHash != "ip-hash" || result.Session.UserAgentHash != "ua-hash" {
		t.Fatalf("unexpected meta: %#v", result.Session)
	}

	later := now.Add(2 * time.Hour)
	service.now = func() time.Time { return later }
	restored, err := service.EnsureSession(context.Background(), result.Credential, domainguest.SessionMeta{})
	if err != nil {
		t.Fatalf("restore error = %v", err)
	}
	if restored.Created || restored.Credential != "" {
		t.Fatalf("expected restore without new credential, got %#v", restored)
	}
	if restored.Session.PublicID != result.Session.PublicID {
		t.Fatalf("restored session mismatch: %s vs %s", restored.Session.PublicID, result.Session.PublicID)
	}
	if !restored.Session.LastSeenAt.Equal(later) {
		t.Fatalf("last seen = %s, want %s", restored.Session.LastSeenAt, later)
	}
	if !restored.Session.ExpiresAt.Equal(result.Session.ExpiresAt) {
		t.Fatal("restore must not extend or shorten the session expiry")
	}
	if repo.created != 1 {
		t.Fatalf("expected exactly one created session, got %d", repo.created)
	}
}

func TestEnsureSessionRejectsDisabledAndInvalidCredentials(t *testing.T) {
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	disabled := newTestService(newRepoStub(), settingsStub{values: map[string]string{SettingEnabled: "false"}}, &modelStub{}, now)
	if _, err := disabled.EnsureSession(context.Background(), "", domainguest.SessionMeta{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}
	// 未配置 guest:enabled 时必须按默认值关闭。
	defaulted := newTestService(newRepoStub(), settingsStub{values: map[string]string{}}, &modelStub{}, now)
	if _, err := defaulted.EnsureSession(context.Background(), "", domainguest.SessionMeta{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected default disabled, got %v", err)
	}

	// 失效凭证按“无凭证”处理：必须签发新会话，而不是把访客挡在门外。
	repo := newRepoStub()
	service := newTestService(repo, enabledSettings(nil), &modelStub{}, now)
	refreshed, err := service.EnsureSession(context.Background(), "unknown-credential", domainguest.SessionMeta{})
	if err != nil {
		t.Fatalf("stale credential must be replaced with a new session, got %v", err)
	}
	if !refreshed.Created || refreshed.Credential == "" || refreshed.Session == nil {
		t.Fatalf("expected a newly issued guest session, got %#v", refreshed)
	}
	if repo.created != 1 {
		t.Fatalf("expected exactly one new session, got %d", repo.created)
	}
}

func TestResolveSessionRejectsExpiredAndRevoked(t *testing.T) {
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	repo := newRepoStub()
	service := newTestService(repo, enabledSettings(nil), &modelStub{}, now)
	result, err := service.EnsureSession(context.Background(), "", domainguest.SessionMeta{})
	if err != nil {
		t.Fatalf("EnsureSession() error = %v", err)
	}

	service.now = func() time.Time { return now.Add(SessionTTL) }
	if _, err := service.ResolveSession(context.Background(), result.Credential); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expected expired session to be rejected, got %v", err)
	}

	service.now = func() time.Time { return now }
	repo.byID[result.Session.ID].Status = domainguest.SessionStatusRevoked
	if _, err := service.ResolveSession(context.Background(), result.Credential); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expected revoked session to be rejected, got %v", err)
	}
	if _, err := service.ResolveSession(context.Background(), "  "); !errors.Is(err, ErrSessionRequired) {
		t.Fatalf("expected empty credential to be rejected, got %v", err)
	}
}

func TestPublicConfigGatesModelsOnSwitchAndWhitelist(t *testing.T) {
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	catalog := &modelStub{views: []appchannel.ModelView{{PlatformModelName: "gpt-4o"}}}

	disabled := newTestService(newRepoStub(), settingsStub{values: map[string]string{
		SettingEnabled:        "false",
		SettingModelWhitelist: "gpt-4o",
	}}, catalog, now)
	view, err := disabled.PublicConfig(context.Background())
	if err != nil {
		t.Fatalf("PublicConfig() error = %v", err)
	}
	if view.Enabled || len(view.Models) != 0 {
		t.Fatalf("disabled guest mode must not expose models: %#v", view)
	}

	noWhitelist := newTestService(newRepoStub(), enabledSettings(nil), catalog, now)
	view, err = noWhitelist.PublicConfig(context.Background())
	if err != nil {
		t.Fatalf("PublicConfig() error = %v", err)
	}
	if len(view.Models) != 0 {
		t.Fatalf("empty whitelist must return no models, got %#v", view.Models)
	}

	enabled := newTestService(newRepoStub(), enabledSettings(map[string]string{
		SettingModelWhitelist: " gpt-4o , claude-sonnet ",
	}), catalog, now)
	view, err = enabled.PublicConfig(context.Background())
	if err != nil {
		t.Fatalf("PublicConfig() error = %v", err)
	}
	if !view.Enabled || len(view.Models) != 1 {
		t.Fatalf("unexpected config: %#v", view)
	}
	if len(catalog.refs) != 2 || catalog.refs[0] != "gpt-4o" || catalog.refs[1] != "claude-sonnet" {
		t.Fatalf("whitelist must be trimmed and forwarded, got %#v", catalog.refs)
	}
	if view.BudgetTimezone != DefaultBudgetTimezone {
		t.Fatalf("unexpected budget timezone %q", view.BudgetTimezone)
	}
	if !view.ResetAt.After(now) {
		t.Fatalf("reset time must be in the future, got %s", view.ResetAt)
	}
}

func TestUsageReportsConsumedReservedAndAvailable(t *testing.T) {
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	repo := newRepoStub()
	service := newTestService(repo, enabledSettings(map[string]string{
		SettingPerVisitorDailyBudget: "0.10",
		SettingGlobalDailyBudget:     "0.05",
	}), &modelStub{}, now)

	session := &domainguest.Session{ID: 7, PublicID: "guest-1", Status: domainguest.SessionStatusActive}
	budgetDate, _, err := BudgetWindow(now, DefaultBudgetTimezone)
	if err != nil {
		t.Fatalf("BudgetWindow() error = %v", err)
	}
	repo.daily[7] = &domainguest.DailyUsage{
		SessionID:       7,
		BudgetDate:      budgetDate,
		SettledNanousd:  60_000_000,
		ReservedNanousd: 10_000_000,
		RequestCount:    3,
	}
	repo.global[budgetDate] = &domainguest.GlobalDailyUsage{
		BudgetDate:      budgetDate,
		SettledNanousd:  900_000_000,
		ReservedNanousd: 200_000_000,
		RequestCount:    9,
	}

	snapshot, err := service.Usage(context.Background(), session)
	if err != nil {
		t.Fatalf("Usage() error = %v", err)
	}
	if snapshot.PerVisitor.ConsumedNanousd != 70_000_000 || snapshot.PerVisitor.AvailableNanousd != 30_000_000 {
		t.Fatalf("unexpected per-visitor usage: %#v", snapshot.PerVisitor)
	}
	if snapshot.PerVisitor.RequestCount != 3 {
		t.Fatalf("unexpected request count: %#v", snapshot.PerVisitor)
	}
	// 全站预算 0.05 已被超出时可用额度必须归零，且保留原始消耗。
	if snapshot.Global.AvailableNanousd != 0 || snapshot.Global.ConsumedNanousd != 1_100_000_000 {
		t.Fatalf("unexpected global usage: %#v", snapshot.Global)
	}
	if _, err := service.Usage(context.Background(), nil); !errors.Is(err, ErrSessionRequired) {
		t.Fatalf("expected ErrSessionRequired, got %v", err)
	}
}

func TestBudgetWindowUsesCalendarDaysInBudgetTimezone(t *testing.T) {
	cases := []struct {
		name      string
		now       time.Time
		timezone  string
		wantDate  string
		wantReset string
	}{
		{
			name:      "summer time",
			now:       time.Date(2026, time.July, 1, 21, 30, 0, 0, time.UTC),
			timezone:  "Europe/Berlin",
			wantDate:  "2026-07-01",
			wantReset: "2026-07-02T00:00:00+02:00",
		},
		{
			name:      "winter time",
			now:       time.Date(2026, time.December, 1, 23, 30, 0, 0, time.UTC),
			timezone:  "Europe/Berlin",
			wantDate:  "2026-12-02",
			wantReset: "2026-12-03T00:00:00+01:00",
		},
		{
			name:      "daylight saving switch day is still one calendar day",
			now:       time.Date(2026, time.March, 29, 10, 0, 0, 0, time.UTC),
			timezone:  "Europe/Berlin",
			wantDate:  "2026-03-29",
			wantReset: "2026-03-30T00:00:00+02:00",
		},
		{
			name:      "utc",
			now:       time.Date(2026, time.January, 5, 0, 30, 0, 0, time.UTC),
			timezone:  "UTC",
			wantDate:  "2026-01-05",
			wantReset: "2026-01-06T00:00:00Z",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			date, resetAt, err := BudgetWindow(tc.now, tc.timezone)
			if err != nil {
				t.Fatalf("BudgetWindow() error = %v", err)
			}
			if date != tc.wantDate {
				t.Fatalf("budget date = %q, want %q", date, tc.wantDate)
			}
			if formatted := resetAt.Format(time.RFC3339); formatted != tc.wantReset {
				t.Fatalf("reset at = %q, want %q", formatted, tc.wantReset)
			}
		})
	}

	if _, _, err := BudgetWindow(time.Now(), "Mars/Olympus"); !errors.Is(err, ErrInvalidTimezone) {
		t.Fatalf("expected ErrInvalidTimezone, got %v", err)
	}
}

func TestApplySettingsParsesOverridesAndFallsBack(t *testing.T) {
	settings := DefaultSettings()
	applySettings(&settings, map[string]string{
		SettingEnabled:               "true",
		SettingModelWhitelist:        "gpt-4o, claude-sonnet ,, ",
		SettingPerVisitorDailyBudget: "0.25",
		SettingGlobalDailyBudget:     "2.5",
		SettingDailyGenerationLimit:  "5",
		SettingRateLimitPerMinute:    "7",
		SettingPerVisitorConcurrency: "3",
		SettingGlobalConcurrency:     "4",
		SettingInputTokenLimit:       "16000",
		SettingOutputTokenLimit:      "1024",
		SettingBudgetTimezone:        "Not/AZone",
		SettingBillingRetentionDays:  "14",
	})
	if !settings.Enabled {
		t.Fatal("expected enabled")
	}
	if len(settings.ModelWhitelist) != 2 || settings.ModelWhitelist[0] != "gpt-4o" || settings.ModelWhitelist[1] != "claude-sonnet" {
		t.Fatalf("unexpected whitelist: %#v", settings.ModelWhitelist)
	}
	if settings.PerVisitorDailyBudgetNanousd != 250_000_000 || settings.GlobalDailyBudgetNanousd != 2_500_000_000 {
		t.Fatalf("unexpected budgets: %#v", settings)
	}
	if settings.DailyGenerationLimit != 5 || settings.RateLimitPerMinute != 7 ||
		settings.PerVisitorConcurrency != 3 || settings.GlobalConcurrency != 4 ||
		settings.InputTokenLimit != 16000 || settings.OutputTokenLimit != 1024 ||
		settings.BillingRetentionDays != 14 {
		t.Fatalf("unexpected limits: %#v", settings)
	}
	if settings.BudgetTimezone != DefaultBudgetTimezone {
		t.Fatalf("invalid timezone must fall back to %q, got %q", DefaultBudgetTimezone, settings.BudgetTimezone)
	}

	broken := DefaultSettings()
	applySettings(&broken, map[string]string{
		SettingEnabled:               "maybe",
		SettingPerVisitorDailyBudget: "abc",
		SettingGlobalDailyBudget:     "-1",
		SettingDailyGenerationLimit:  "-5",
		SettingRateLimitPerMinute:    "0",
		SettingBudgetTimezone:        "",
	})
	if !reflect.DeepEqual(broken, DefaultSettings()) {
		t.Fatalf("invalid values must keep defaults, got %#v", broken)
	}
}

func TestNanousdHelpers(t *testing.T) {
	parsed := map[string]int64{
		"0":           0,
		"0.1":         100_000_000,
		"0.10":        100_000_000,
		"1":           1_000_000_000,
		"1.234567891": 1_234_567_891,
		"12.5":        12_500_000_000,
	}
	for raw, want := range parsed {
		got, ok := parseNanousd(raw)
		if !ok || got != want {
			t.Fatalf("parseNanousd(%q) = %d/%v, want %d", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", "abc", "-1", "1.2.3", "1x"} {
		if _, ok := parseNanousd(raw); ok {
			t.Fatalf("parseNanousd(%q) must fail", raw)
		}
	}
	formatted := map[int64]string{
		0:             "0",
		100_000_000:   "0.1",
		1_000_000_000: "1",
		2_500_000_000: "2.5",
	}
	for value, want := range formatted {
		if got := FormatUSD(value); got != want {
			t.Fatalf("FormatUSD(%d) = %q, want %q", value, got, want)
		}
	}
}

func TestReserveEnforcesSwitchWhitelistRateAndConcurrency(t *testing.T) {
	session := &domainguest.Session{ID: 1, PublicID: "guest-1", Status: domainguest.SessionStatusActive}
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)

	disabled := newTestService(newRepoStub(), settingsStub{values: map[string]string{SettingEnabled: "false"}}, &modelStub{}, now)
	if _, err := disabled.Reserve(context.Background(), RequestInput{Session: session, RequestID: "r1", ModelRef: "gpt-4o"}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("expected ErrDisabled, got %v", err)
	}

	noWhitelist := newTestService(newRepoStub(), enabledSettings(nil), &modelStub{}, now)
	if _, err := noWhitelist.Reserve(context.Background(), RequestInput{Session: session, RequestID: "r1", ModelRef: "gpt-4o"}); !errors.Is(err, ErrModelNotAllowed) {
		t.Fatalf("expected ErrModelNotAllowed, got %v", err)
	}

	repo := newRepoStub()
	service := newTestService(repo, enabledSettings(map[string]string{
		SettingModelWhitelist:     "gpt-4o, claude-sonnet",
		SettingRateLimitPerMinute: "2",
	}), &modelStub{}, now)
	repo.recentCount = 2
	if _, err := service.Reserve(context.Background(), RequestInput{Session: session, RequestID: "r1", ModelRef: "GPT-4O"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}

	repo = newRepoStub()
	repo.recentCount = 0
	repo.activeCount = 1
	service = newTestService(repo, enabledSettings(map[string]string{SettingModelWhitelist: "gpt-4o"}), &modelStub{}, now)
	if _, err := service.Reserve(context.Background(), RequestInput{Session: session, RequestID: "r1", ModelRef: "gpt-4o"}); !errors.Is(err, ErrConcurrencyLimited) {
		t.Fatalf("expected per-visitor concurrency error, got %v", err)
	}

	repo = newRepoStub()
	repo.activeGlobal = 2
	service = newTestService(repo, enabledSettings(map[string]string{SettingModelWhitelist: "gpt-4o"}), &modelStub{}, now)
	if _, err := service.Reserve(context.Background(), RequestInput{Session: session, RequestID: "r1", ModelRef: "gpt-4o"}); !errors.Is(err, ErrConcurrencyLimited) {
		t.Fatalf("expected global concurrency error, got %v", err)
	}
}

func TestReserveUsesDailyLimitsAndBillingRef(t *testing.T) {
	session := &domainguest.Session{ID: 7, PublicID: "guest-public-7", Status: domainguest.SessionStatusActive}
	now := time.Date(2026, time.October, 11, 22, 30, 0, 0, time.UTC)
	repo := newRepoStub()
	service := newTestService(repo, enabledSettings(map[string]string{
		SettingModelWhitelist:        "gpt-4o",
		SettingPerVisitorDailyBudget: "0.10",
		SettingGlobalDailyBudget:     "1.00",
		SettingDailyGenerationLimit:  "20",
	}), &modelStub{}, now)

	reservation, err := service.Reserve(context.Background(), RequestInput{
		Session: session, RequestID: "run-1", ModelRef: "gpt-4o", InputDigest: "digest-1", EstimateNanousd: 100,
	})
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if reservation.Existing || reservation.Record == nil || reservation.Record.Status != domainguest.RequestStatusPending {
		t.Fatalf("unexpected reservation: %#v", reservation)
	}
	if reservation.BudgetDate != "2026-10-12" {
		t.Fatalf("budget date must use the configured timezone, got %q", reservation.BudgetDate)
	}
	if reservation.BillingRefNo == "" || !strings.HasPrefix(reservation.BillingRefNo, "guest-") {
		t.Fatalf("unexpected billing ref: %q", reservation.BillingRefNo)
	}
	if repo.lastLimits.PerVisitorDailyNanousd != 100_000_000 || repo.lastLimits.GlobalDailyNanousd != 1_000_000_000 || repo.lastLimits.DailyGenerationLimit != 20 {
		t.Fatalf("unexpected limits passed to repository: %#v", repo.lastLimits)
	}
	if repo.lastReserveIn.BillingRefNo != reservation.BillingRefNo || repo.lastReserveIn.BudgetDate != reservation.BudgetDate {
		t.Fatalf("unexpected repository input: %#v", repo.lastReserveIn)
	}

	// 相同请求 ID 与摘要：命中幂等，不再新增预留。
	again, err := service.Reserve(context.Background(), RequestInput{
		Session: session, RequestID: "run-1", ModelRef: "gpt-4o", InputDigest: "digest-1", EstimateNanousd: 100,
	})
	if err != nil {
		t.Fatalf("idempotent reserve error = %v", err)
	}
	if !again.Existing || repo.reserveCalls != 1 {
		t.Fatalf("expected idempotent reuse, got %#v calls=%d", again, repo.reserveCalls)
	}

	// 相同请求 ID 不同摘要：拒绝。
	if _, err := service.Reserve(context.Background(), RequestInput{
		Session: session, RequestID: "run-1", ModelRef: "gpt-4o", InputDigest: "digest-2", EstimateNanousd: 100,
	}); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("expected ErrRequestConflict, got %v", err)
	}
}

func TestReserveTranslatesRepositoryPolicyErrors(t *testing.T) {
	session := &domainguest.Session{ID: 3, PublicID: "guest-3", Status: domainguest.SessionStatusActive}
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		repoErr  error
		expected error
	}{
		{name: "daily budget", repoErr: domainguest.ErrDailyBudgetExceeded, expected: ErrDailyBudgetExceeded},
		{name: "global budget", repoErr: domainguest.ErrGlobalBudgetExceeded, expected: ErrGlobalBudgetExceeded},
		{name: "daily count", repoErr: domainguest.ErrDailyCountExceeded, expected: ErrDailyCountExceeded},
		{name: "duplicate", repoErr: repository.ErrDuplicate, expected: ErrRequestConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newRepoStub()
			repo.reserveErr = tc.repoErr
			service := newTestService(repo, enabledSettings(map[string]string{SettingModelWhitelist: "gpt-4o"}), &modelStub{}, now)
			_, err := service.Reserve(context.Background(), RequestInput{Session: session, RequestID: "r1", ModelRef: "gpt-4o", InputDigest: "d"})
			if !errors.Is(err, tc.expected) {
				t.Fatalf("expected %v, got %v", tc.expected, err)
			}
		})
	}
}

func TestSettleAndReleaseForwardToRepository(t *testing.T) {
	session := &domainguest.Session{ID: 5, PublicID: "guest-5", Status: domainguest.SessionStatusActive}
	now := time.Date(2026, time.October, 11, 12, 0, 0, 0, time.UTC)
	repo := newRepoStub()
	service := newTestService(repo, enabledSettings(map[string]string{SettingModelWhitelist: "gpt-4o"}), &modelStub{}, now)

	reservation, err := service.Reserve(context.Background(), RequestInput{Session: session, RequestID: "run-9", ModelRef: "gpt-4o", InputDigest: "d", EstimateNanousd: 100})
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	if err := service.Settle(context.Background(), reservation, 42, false); err != nil {
		t.Fatalf("Settle() error = %v", err)
	}
	record, err := repo.GetGuestRequest(context.Background(), 5, "run-9")
	if err != nil {
		t.Fatalf("GetGuestRequest() error = %v", err)
	}
	if record.Status != domainguest.RequestStatusSettled || record.SettledNanousd != 42 {
		t.Fatalf("unexpected settled record: %#v", record)
	}

	second, err := service.Reserve(context.Background(), RequestInput{Session: session, RequestID: "run-10", ModelRef: "gpt-4o", InputDigest: "d2", EstimateNanousd: 100})
	if err != nil {
		t.Fatalf("Reserve() second error = %v", err)
	}
	if err := service.Release(context.Background(), second, true, true); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	record, err = repo.GetGuestRequest(context.Background(), 5, "run-10")
	if err != nil {
		t.Fatalf("GetGuestRequest() second error = %v", err)
	}
	if record.Status != domainguest.RequestStatusRejected {
		t.Fatalf("unexpected released record: %#v", record)
	}

	// 已有幂等记录时结算/释放是空操作，不能重复改动账本。
	if err := service.Settle(context.Background(), &Reservation{Existing: true}, 999, false); err != nil {
		t.Fatalf("Settle() on existing reservation must be a no-op, got %v", err)
	}
	if len(repo.settleCalls) != 1 || len(repo.releaseCalls) != 1 {
		t.Fatalf("unexpected calls: settle=%v release=%v", repo.settleCalls, repo.releaseCalls)
	}
}
