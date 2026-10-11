package guest

import (
	"strconv"
	"strings"
	"time"

	appguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/guest"
	domainguest "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/guest"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/pagination"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/shared/response"
	"github.com/gin-gonic/gin"
)

// AdminGuestUsageRow 是后台看到的单个游客当日用量（匿名，只有公开 ID）。
type AdminGuestUsageRow struct {
	GuestID         string `json:"guestId"`
	SessionID       uint   `json:"sessionId"`
	SettledNanousd  int64  `json:"settledNanousd"`
	SettledUSD      string `json:"settledUSD"`
	ReservedNanousd int64  `json:"reservedNanousd"`
	ReservedUSD     string `json:"reservedUSD"`
	RequestCount    int    `json:"requestCount"`
}

// AdminGuestOverviewResponse 是后台游客面板的概览响应。
type AdminGuestOverviewResponse struct {
	Enabled        bool                 `json:"enabled"`
	BudgetTimezone string               `json:"budgetTimezone"`
	BudgetDate     string               `json:"budgetDate"`
	ResetAt        string               `json:"resetAt"`
	Limits         LimitsResponse       `json:"limits"`
	Global         UsageResponse        `json:"global"`
	Guests         []AdminGuestUsageRow `json:"guests"`
}

// AdminGuestRequestResponse 是匿名账单条目；不包含任何对话正文。
type AdminGuestRequestResponse struct {
	RequestID       string `json:"requestId"`
	GuestID         string `json:"guestId"`
	Model           string `json:"model"`
	Status          string `json:"status"`
	BudgetDate      string `json:"budgetDate"`
	ReservedNanousd int64  `json:"reservedNanousd"`
	SettledNanousd  int64  `json:"settledNanousd"`
	SettledUSD      string `json:"settledUSD"`
	BillingRefNo    string `json:"billingRefNo"`
	CreatedAt       string `json:"createdAt"`
	SettledAt       string `json:"settledAt,omitempty"`
}

// AdminOverview godoc
// @Summary 后台游客概览
// @Description 返回游客开关、限额、当日全站用量与按游客排名的用量（匿名 public id）。
// @Tags guest
// @Produce json
// @Security BearerAuth
// @Success 200 {object} response.Envelope
// @Router /admin/guest/overview [get]
func (h *Handler) AdminOverview(c *gin.Context) {
	overview, err := h.service.AdminOverviewForAdmin(c.Request.Context(), 50)
	if err != nil {
		writeGuestError(c, err)
		return
	}
	response.Success(c, toAdminOverviewResponse(overview))
}

// AdminRequests godoc
// @Summary 后台游客匿名账单
// @Description 分页返回游客计费请求（模型、状态、金额、计费引用号），不包含任何对话正文。
// @Tags guest
// @Produce json
// @Security BearerAuth
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Param session_id query int false "按游客会话筛选"
// @Param status query string false "按状态筛选"
// @Param budget_date query string false "按预算日筛选"
// @Success 200 {object} response.Envelope
// @Router /admin/guest/requests [get]
func (h *Handler) AdminRequests(c *gin.Context) {
	page, pageSize := pagination.Parse(c.Query("page"), c.Query("page_size"))
	offset, limit := pagination.Offset(page, pageSize)
	filter := domainguest.RequestFilter{
		Status:     strings.TrimSpace(c.Query("status")),
		BudgetDate: strings.TrimSpace(c.Query("budget_date")),
		Limit:      limit,
		Offset:     offset,
	}
	if raw := strings.TrimSpace(c.Query("session_id")); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			response.InvalidQueryParam(c, "session_id")
			return
		}
		filter.SessionID = uint(parsed)
	}
	items, total, err := h.service.AdminRequests(c.Request.Context(), filter)
	if err != nil {
		writeGuestError(c, err)
		return
	}
	views := make([]AdminGuestRequestResponse, 0, len(items))
	for _, item := range items {
		views = append(views, toAdminRequestResponse(item))
	}
	response.SuccessPage(c, total, views)
}

func toAdminOverviewResponse(overview *appguest.AdminOverview) AdminGuestOverviewResponse {
	if overview == nil {
		return AdminGuestOverviewResponse{Guests: []AdminGuestUsageRow{}}
	}
	guests := make([]AdminGuestUsageRow, 0, len(overview.Guests))
	for _, item := range overview.Guests {
		guests = append(guests, AdminGuestUsageRow{
			GuestID:         item.GuestPublicID,
			SessionID:       item.SessionID,
			SettledNanousd:  item.SettledNanousd,
			SettledUSD:      appguest.FormatUSD(item.SettledNanousd),
			ReservedNanousd: item.ReservedNanousd,
			ReservedUSD:     appguest.FormatUSD(item.ReservedNanousd),
			RequestCount:    item.RequestCount,
		})
	}
	return AdminGuestOverviewResponse{
		Enabled:        overview.Enabled,
		BudgetTimezone: overview.BudgetTimezone,
		BudgetDate:     overview.BudgetDate,
		ResetAt:        overview.ResetAt.UTC().Format(time.RFC3339),
		Limits:         toLimitsResponse(overview.Settings),
		Global:         toUsageResponse(overview.GlobalUsage),
		Guests:         guests,
	}
}

func toAdminRequestResponse(item domainguest.RequestRecord) AdminGuestRequestResponse {
	result := AdminGuestRequestResponse{
		RequestID:       item.RequestID,
		GuestID:         item.GuestPublicID,
		Model:           item.ModelRef,
		Status:          item.Status,
		BudgetDate:      item.BudgetDate,
		ReservedNanousd: item.ReservedNanousd,
		SettledNanousd:  item.SettledNanousd,
		SettledUSD:      appguest.FormatUSD(item.SettledNanousd),
		BillingRefNo:    item.BillingRefNo,
	}
	if !item.CreatedAt.IsZero() {
		result.CreatedAt = item.CreatedAt.UTC().Format(time.RFC3339)
	}
	if item.SettledAt != nil {
		result.SettledAt = item.SettledAt.UTC().Format(time.RFC3339)
	}
	return result
}
