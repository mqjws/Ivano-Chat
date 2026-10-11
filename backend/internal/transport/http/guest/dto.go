package guest

import (
	"time"

	appchannel "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/channel"
	appguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/guest"
	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
)

// ModelResponse 是游客可见的模型信息，只包含前端展示所需字段。
type ModelResponse struct {
	PlatformModelName string `json:"platformModelName"`
	Vendor            string `json:"vendor"`
	VendorName        string `json:"vendorName"`
	VendorIcon        string `json:"vendorIcon"`
	DisplayGroupID    *uint  `json:"displayGroupID,omitempty"`
	DisplayGroupName  string `json:"displayGroupName"`
	DisplayGroupIcon  string `json:"displayGroupIcon"`
	KindsJSON         string `json:"kindsJSON"`
	Icon              string `json:"icon"`
	CapabilitiesJSON  string `json:"capabilitiesJSON"`
	Description       string `json:"description"`
	SortOrder         int    `json:"sortOrder"`
}

// LimitsResponse 是游客限额配置快照。
type LimitsResponse struct {
	PerVisitorDailyBudgetNanousd int64  `json:"perVisitorDailyBudgetNanousd"`
	PerVisitorDailyBudgetUSD     string `json:"perVisitorDailyBudgetUSD"`
	GlobalDailyBudgetNanousd     int64  `json:"globalDailyBudgetNanousd"`
	GlobalDailyBudgetUSD         string `json:"globalDailyBudgetUSD"`
	DailyGenerationLimit         int    `json:"dailyGenerationLimit"`
	RateLimitPerMinute           int    `json:"rateLimitPerMinute"`
	PerVisitorConcurrency        int    `json:"perVisitorConcurrency"`
	GlobalConcurrency            int    `json:"globalConcurrency"`
	InputTokenLimit              int    `json:"inputTokenLimit"`
	OutputTokenLimit             int    `json:"outputTokenLimit"`
	BillingRetentionDays         int    `json:"billingRetentionDays"`
}

// ConfigResponse 是公开游客配置响应。
type ConfigResponse struct {
	Enabled         bool            `json:"enabled"`
	SessionTTLHours int             `json:"sessionTTLHours"`
	BudgetTimezone  string          `json:"budgetTimezone"`
	BudgetDate      string          `json:"budgetDate"`
	ResetAt         string          `json:"resetAt"`
	Limits          LimitsResponse  `json:"limits"`
	Models          []ModelResponse `json:"models"`
}

// SessionResponse 是创建或恢复游客会话的响应。
type SessionResponse struct {
	GuestID   string `json:"guestId"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt"`
	Restored  bool   `json:"restored"`
}

// UsageResponse 是单个游客或全站在当前预算日的用量。
type UsageResponse struct {
	BudgetNanousd    int64  `json:"budgetNanousd"`
	BudgetUSD        string `json:"budgetUSD"`
	SettledNanousd   int64  `json:"settledNanousd"`
	SettledUSD       string `json:"settledUSD"`
	ReservedNanousd  int64  `json:"reservedNanousd"`
	ReservedUSD      string `json:"reservedUSD"`
	ConsumedNanousd  int64  `json:"consumedNanousd"`
	AvailableNanousd int64  `json:"availableNanousd"`
	AvailableUSD     string `json:"availableUSD"`
	RequestCount     int    `json:"requestCount"`
}

// MeResponse 是当前游客会话与额度响应。
type MeResponse struct {
	GuestID        string         `json:"guestId"`
	CreatedAt      string         `json:"createdAt"`
	ExpiresAt      string         `json:"expiresAt"`
	BudgetTimezone string         `json:"budgetTimezone"`
	BudgetDate     string         `json:"budgetDate"`
	ResetAt        string         `json:"resetAt"`
	PerVisitor     UsageResponse  `json:"perVisitor"`
	Global         UsageResponse  `json:"global"`
	Limits         LimitsResponse `json:"limits"`
}

func toLimitsResponse(settings appguest.Settings) LimitsResponse {
	return LimitsResponse{
		PerVisitorDailyBudgetNanousd: settings.PerVisitorDailyBudgetNanousd,
		PerVisitorDailyBudgetUSD:     appguest.FormatUSD(settings.PerVisitorDailyBudgetNanousd),
		GlobalDailyBudgetNanousd:     settings.GlobalDailyBudgetNanousd,
		GlobalDailyBudgetUSD:         appguest.FormatUSD(settings.GlobalDailyBudgetNanousd),
		DailyGenerationLimit:         settings.DailyGenerationLimit,
		RateLimitPerMinute:           settings.RateLimitPerMinute,
		PerVisitorConcurrency:        settings.PerVisitorConcurrency,
		GlobalConcurrency:            settings.GlobalConcurrency,
		InputTokenLimit:              settings.InputTokenLimit,
		OutputTokenLimit:             settings.OutputTokenLimit,
		BillingRetentionDays:         settings.BillingRetentionDays,
	}
}

func toModelResponses(models []appchannel.ModelView) []ModelResponse {
	results := make([]ModelResponse, 0, len(models))
	for _, item := range models {
		results = append(results, ModelResponse{
			PlatformModelName: item.PlatformModelName,
			Vendor:            item.Vendor,
			VendorName:        item.VendorName,
			VendorIcon:        item.VendorIcon,
			DisplayGroupID:    item.DisplayGroupID,
			DisplayGroupName:  item.DisplayGroupName,
			DisplayGroupIcon:  item.DisplayGroupIcon,
			KindsJSON:         item.KindsJSON,
			Icon:              item.Icon,
			CapabilitiesJSON:  item.CapabilitiesJSON,
			Description:       item.Description,
			SortOrder:         item.SortOrder,
		})
	}
	return results
}

func toConfigResponse(view *appguest.ConfigView) ConfigResponse {
	if view == nil {
		return ConfigResponse{Models: []ModelResponse{}}
	}
	return ConfigResponse{
		Enabled:         view.Enabled,
		SessionTTLHours: int(view.SessionTTL / time.Hour),
		BudgetTimezone:  view.BudgetTimezone,
		BudgetDate:      view.BudgetDate,
		ResetAt:         view.ResetAt.UTC().Format(time.RFC3339),
		Limits:          toLimitsResponse(view.Settings),
		Models:          toModelResponses(view.Models),
	}
}

func toSessionResponse(result *appguest.SessionResult) SessionResponse {
	if result == nil || result.Session == nil {
		return SessionResponse{}
	}
	return SessionResponse{
		GuestID:   result.Session.PublicID,
		CreatedAt: result.Session.CreatedAt.UTC().Format(time.RFC3339),
		ExpiresAt: result.Session.ExpiresAt.UTC().Format(time.RFC3339),
		Restored:  !result.Created,
	}
}

func toUsageResponse(view appguest.UsageView) UsageResponse {
	return UsageResponse{
		BudgetNanousd:    view.BudgetNanousd,
		BudgetUSD:        appguest.FormatUSD(view.BudgetNanousd),
		SettledNanousd:   view.SettledNanousd,
		SettledUSD:       appguest.FormatUSD(view.SettledNanousd),
		ReservedNanousd:  view.ReservedNanousd,
		ReservedUSD:      appguest.FormatUSD(view.ReservedNanousd),
		ConsumedNanousd:  view.ConsumedNanousd,
		AvailableNanousd: view.AvailableNanousd,
		AvailableUSD:     appguest.FormatUSD(view.AvailableNanousd),
		RequestCount:     view.RequestCount,
	}
}

func toMeResponse(session *domainguest.Session, snapshot *appguest.UsageSnapshot) MeResponse {
	response := MeResponse{
		PerVisitor: toUsageResponse(snapshot.PerVisitor),
		Global:     toUsageResponse(snapshot.Global),
		Limits:     toLimitsResponse(snapshot.Settings),
	}
	if session != nil {
		response.GuestID = session.PublicID
		response.CreatedAt = session.CreatedAt.UTC().Format(time.RFC3339)
		response.ExpiresAt = session.ExpiresAt.UTC().Format(time.RFC3339)
	}
	response.BudgetTimezone = snapshot.BudgetTimezone
	response.BudgetDate = snapshot.BudgetDate
	response.ResetAt = snapshot.ResetAt.UTC().Format(time.RFC3339)
	return response
}
