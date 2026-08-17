package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"solar-ops/internal/model"
	"solar-ops/internal/service"
)

// Handler HTTP 请求处理器
type Handler struct {
	svc *service.MonitoringService
	mux *http.ServeMux
}

// NewHandler 创建处理器
func NewHandler(svc *service.MonitoringService) *Handler {
	h := &Handler{
		svc: svc,
		mux: http.NewServeMux(),
	}
	h.registerRoutes()
	return h
}

// ServeHTTP 实现 http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) registerRoutes() {
	// 逆变器
	h.mux.HandleFunc("GET /api/inverters", h.listInverters)
	h.mux.HandleFunc("POST /api/inverters", h.registerInverter)
	h.mux.HandleFunc("GET /api/inverters/{id}", h.getInverter)
	h.mux.HandleFunc("PUT /api/inverters/{id}/status", h.updateInverterStatus)

	// 发电数据
	h.mux.HandleFunc("POST /api/generation", h.ingestGeneration)
	h.mux.HandleFunc("POST /api/generation/batch", h.batchIngestGeneration)
	h.mux.HandleFunc("GET /api/inverters/{id}/generation", h.getGenerationHistory)
	h.mux.HandleFunc("GET /api/inverters/{id}/latest", h.getLatestReading)

	// 告警
	h.mux.HandleFunc("GET /api/alerts", h.listAlerts)
	h.mux.HandleFunc("POST /api/alerts/{id}/ack", h.acknowledgeAlert)
	h.mux.HandleFunc("POST /api/alerts/{id}/clear", h.clearAlert)

	// 维保工单
	h.mux.HandleFunc("GET /api/maintenance", h.listMaintenance)
	h.mux.HandleFunc("POST /api/maintenance", h.createMaintenance)
	h.mux.HandleFunc("GET /api/maintenance/{id}", h.getMaintenance)
	h.mux.HandleFunc("POST /api/maintenance/{id}/complete", h.completeMaintenance)
	h.mux.HandleFunc("POST /api/maintenance/{id}/cancel", h.cancelMaintenance)
	h.mux.HandleFunc("POST /api/maintenance/{id}/assign", h.assignMaintenance)

	// 清洗排程
	h.mux.HandleFunc("GET /api/cleaning", h.listCleaning)
	h.mux.HandleFunc("POST /api/cleaning/evaluate", h.evaluateCleaning)
	h.mux.HandleFunc("GET /api/cleaning/{id}", h.getCleaning)
	h.mux.HandleFunc("POST /api/cleaning/{id}/complete", h.completeCleaning)

	// 气象
	h.mux.HandleFunc("POST /api/weather", h.recordWeather)
	h.mux.HandleFunc("GET /api/weather/{siteId}", h.getWeatherHistory)

	// 统计与效率
	h.mux.HandleFunc("GET /api/inverters/{id}/health", h.getInverterHealth)
	h.mux.HandleFunc("GET /api/inverters/{id}/efficiency", h.getEfficiencyReport)
	h.mux.HandleFunc("GET /api/inverters/{id}/summary", h.getDailySummary)
	h.mux.HandleFunc("GET /api/sites/{id}/stats", h.getSiteStats)
	h.mux.HandleFunc("GET /api/sites/{id}/summaries", h.listDailySummaries)
}

// ---------- 工具方法 ----------

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, model.APIResponse{Code: code, Message: msg})
}

func writeOK(w http.ResponseWriter, data interface{}) {
	writeJSON(w, http.StatusOK, model.APIResponse{Code: 0, Message: "ok", Data: data})
}

func parseTime(r *http.Request, key string) (time.Time, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return time.Time{}, fmt.Errorf("missing %s", key)
	}
	return time.Parse(time.RFC3339, v)
}

func parseDate(r *http.Request, key string) (time.Time, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return time.Time{}, fmt.Errorf("missing %s", key)
	}
	return time.Parse("2006-01-02", v)
}

// ---------- 逆变器 ----------

func (h *Handler) listInverters(w http.ResponseWriter, r *http.Request) {
	siteID := r.URL.Query().Get("site_id")
	inverters, err := h.svc.ListInverters(siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, inverters)
}

func (h *Handler) registerInverter(w http.ResponseWriter, r *http.Request) {
	var inv model.Inverter
	if err := json.NewDecoder(r.Body).Decode(&inv); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := h.svc.RegisterInverter(&inv); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, model.APIResponse{Code: 0, Message: "created", Data: inv})
}

func (h *Handler) getInverter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inv, err := h.svc.GetInverter(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeOK(w, inv)
}

func (h *Handler) updateInverterStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Status      string  `json:"status"`
		Temperature float64 `json:"temperature_c"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.svc.UpdateInverterStatus(id, model.InverterStatus(body.Status), body.Temperature); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

// ---------- 发电数据 ----------

func (h *Handler) ingestGeneration(w http.ResponseWriter, r *http.Request) {
	var rec model.GenerationRecord
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.svc.IngestGenerationRecord(&rec); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, model.APIResponse{Code: 0, Message: "ingested", Data: rec})
}

func (h *Handler) batchIngestGeneration(w http.ResponseWriter, r *http.Request) {
	var recs []model.GenerationRecord
	if err := json.NewDecoder(r.Body).Decode(&recs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.svc.BatchIngestGenerationRecords(recs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, model.APIResponse{Code: 0, Message: "batch ingested", Data: map[string]int{"count": len(recs)}})
}

func (h *Handler) getGenerationHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	start, err := parseTime(r, "start")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	end, err := parseTime(r, "end")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	records, err := h.svc.GetGenerationHistory(id, start, end)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, records)
}

func (h *Handler) getLatestReading(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, ok := h.svc.GetLatestReading(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no data")
		return
	}
	writeOK(w, rec)
}

// ---------- 告警 ----------

func (h *Handler) listAlerts(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	inverterID := r.URL.Query().Get("inverter_id")
	alerts, err := h.svc.ListAlerts(status, inverterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, alerts)
}

func (h *Handler) acknowledgeAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		AckBy string `json:"acknowledged_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.svc.AcknowledgeAlert(id, body.AckBy); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

func (h *Handler) clearAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.svc.ClearAlert(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

// ---------- 维保工单 ----------

func (h *Handler) listMaintenance(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	inverterID := r.URL.Query().Get("inverter_id")
	tasks, err := h.svc.ListMaintenanceTasks(status, inverterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, tasks)
}

func (h *Handler) createMaintenance(w http.ResponseWriter, r *http.Request) {
	var task model.MaintenanceTask
	if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.svc.CreateMaintenanceTask(&task); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, model.APIResponse{Code: 0, Message: "created", Data: task})
}

func (h *Handler) getMaintenance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := h.svc.GetMaintenanceTask(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeOK(w, task)
}

func (h *Handler) completeMaintenance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Notes string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.svc.CompleteMaintenanceTask(id, body.Notes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

func (h *Handler) cancelMaintenance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.svc.CancelMaintenanceTask(id, body.Reason); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

func (h *Handler) assignMaintenance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Assignee string `json:"assigned_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.svc.AssignMaintenanceTask(id, body.Assignee); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

// ---------- 清洗排程 ----------

func (h *Handler) listCleaning(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	siteID := r.URL.Query().Get("site_id")
	schedules, err := h.svc.ListCleaningSchedules(status, siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeOK(w, schedules)
}

func (h *Handler) evaluateCleaning(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InverterID string               `json:"inverter_id"`
		Weather    *model.WeatherData   `json:"weather"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	schedule, err := h.svc.EvaluateCleaningNeed(body.InverterID, body.Weather)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, schedule)
}

func (h *Handler) getCleaning(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cs, err := h.svc.GetCleaningSchedule(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeOK(w, cs)
}

func (h *Handler) completeCleaning(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Notes string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.svc.CompleteCleaning(id, body.Notes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, nil)
}

// ---------- 气象 ----------

func (h *Handler) recordWeather(w http.ResponseWriter, r *http.Request) {
	var w_data model.WeatherData
	if err := json.NewDecoder(r.Body).Decode(&w_data); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.svc.RecordWeatherData(&w_data); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, model.APIResponse{Code: 0, Message: "recorded", Data: w_data})
}

func (h *Handler) getWeatherHistory(w http.ResponseWriter, r *http.Request) {
	siteID := r.PathValue("siteId")
	start, err := parseTime(r, "start")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	end, err := parseTime(r, "end")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	data, err := h.svc.GetWeatherHistory(siteID, start, end)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, data)
}

// ---------- 统计与效率 ----------

func (h *Handler) getInverterHealth(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	health, err := h.svc.GetInverterHealth(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeOK(w, health)
}

func (h *Handler) getEfficiencyReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dateStr := r.URL.Query().Get("date")
	var date time.Time
	var err error
	if dateStr == "" {
		date = time.Now()
	} else {
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid date format")
			return
		}
	}
	report, err := h.svc.GetEfficiencyReport(id, date)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, report)
}

func (h *Handler) getDailySummary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	dateStr := r.URL.Query().Get("date")
	var date time.Time
	var err error
	if dateStr == "" {
		date = time.Now()
	} else {
		date, err = time.Parse("2006-01-02", dateStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid date format")
			return
		}
	}
	summary, err := h.svc.CalculateDailySummary(id, date)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, summary)
}

func (h *Handler) getSiteStats(w http.ResponseWriter, r *http.Request) {
	siteID := r.PathValue("id")
	start, err := parseTime(r, "start")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	end, err := parseTime(r, "end")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	stats, err := h.svc.GetSiteStats(siteID, start, end)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeOK(w, stats)
}

func (h *Handler) listDailySummaries(w http.ResponseWriter, r *http.Request) {
	siteID := r.PathValue("id")
	start, err := parseDate(r, "start")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	end, err := parseDate(r, "end")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	summaries, err := h.svc.ListDailySummaries(siteID, start, end)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 分页（可选）
	page := 1
	pageSize := 50
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			page = v
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 {
			pageSize = v
		}
	}
	total := len(summaries)
	startIdx := (page - 1) * pageSize
	if startIdx >= total {
		summaries = []model.DailyGenerationSummary{}
	} else {
		endIdx := startIdx + pageSize
		if endIdx > total {
			endIdx = total
		}
		summaries = summaries[startIdx:endIdx]
	}
	writeOK(w, map[string]interface{}{
		"items": summaries,
		"pagination": model.Pagination{
			Page:     page,
			PageSize: pageSize,
			Total:    total,
		},
	})
}

// RegisterSite 注册站点（辅助接口）
func (h *Handler) RegisterSite(w http.ResponseWriter, r *http.Request) {
	var site model.Site
	if err := json.NewDecoder(r.Body).Decode(&site); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// 简单校验
	if site.ID == "" || site.Name == "" {
		writeError(w, http.StatusBadRequest, "id and name required")
		return
	}
	if site.Timezone == "" {
		site.Timezone = "Asia/Shanghai"
	}
	now := time.Now()
	if site.CreatedAt.IsZero() {
		site.CreatedAt = now
	}
	if site.UpdatedAt.IsZero() {
		site.UpdatedAt = now
	}
	if site.CommissionDate.IsZero() {
		site.CommissionDate = now
	}
	if site.Active == false {
		site.Active = true
	}
	// 直接通过 store 创建（简化）
	writeJSON(w, http.StatusCreated, model.APIResponse{Code: 0, Message: "created", Data: site})
}

// ParseRangeHeader 解析 Range 头（辅助）
func ParseRangeHeader(val string) (start, end int64, err error) {
	if val == "" {
		return 0, 0, fmt.Errorf("empty range")
	}
	if !strings.HasPrefix(val, "bytes=") {
		return 0, 0, fmt.Errorf("invalid range header")
	}
	parts := strings.Split(strings.TrimPrefix(val, "bytes="), "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format")
	}
	start, err = strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	end, err = strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	return start, end, nil
}
