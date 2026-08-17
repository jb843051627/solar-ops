package service

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"solar-ops/internal/model"
	"solar-ops/internal/store"
)

// 错误哨兵
var (
	ErrInverterNotFound = errors.New("inverter not found")
	ErrSiteNotFound     = errors.New("site not found")
	ErrAlertNotFound    = errors.New("alert not found")
	ErrMaintenanceNotFound = errors.New("maintenance task not found")
	ErrCleaningNotFound  = errors.New("cleaning schedule not found")
	ErrInvalidThreshold  = errors.New("invalid threshold value")
	ErrInvalidDateRange  = errors.New("invalid date range")
)

// 告警阈值常量
const (
	ThresholdTempHigh     = 65.0  // 逆变器过温阈值 °C
	ThresholdTempCritical = 80.0  // 逆变器临界温度 °C
	ThresholdVoltageLow   = 200.0 // DC电压过低 V
	ThresholdVoltageHigh  = 1000.0 // DC电压过高 V
	ThresholdEfficiency   = 0.80  // 最低效率阈值
	ThresholdDustDays     = 30    // 积尘天数阈值
	ThresholdRainfall     = 5.0   // 有效降雨量 mm
	DeratingTempStart     = 50.0  // 温度降额起始点 °C
	DeratingTempFull      = 85.0  // 温度降额满载点 °C
)

// MonitoringService 光伏监控服务
type MonitoringService struct {
	store *store.Store

	// 缓存：最新发电记录（并发安全）
	mu            sync.RWMutex
	latestReadings map[string]model.GenerationRecord // inverterID -> latest

	// 告警缓存（并发安全）
	alertMu     sync.Mutex
	alertCache  map[string][]model.Alert // inverterID -> active alerts

	// 日发电汇总缓存
	summaryMu   sync.Mutex
	summaryCache map[string]model.DailyGenerationSummary // inverterID+date -> summary

	// 通知通道
	notifyCh chan model.Alert
}

// NewMonitoringService 创建监控服务
func NewMonitoringService(s *store.Store) *MonitoringService {
	return &MonitoringService{
		store:          s,
		latestReadings: make(map[string]model.GenerationRecord),
		alertCache:     make(map[string][]model.Alert),
		summaryCache:   make(map[string]model.DailyGenerationSummary),
		notifyCh:       make(chan model.Alert, 100),
	}
}

// NotifyChannel 返回告警通知通道
func (svc *MonitoringService) NotifyChannel() <-chan model.Alert {
	return svc.notifyCh
}

// ---------- 逆变器管理 ----------

// RegisterInverter 注册逆变器
func (svc *MonitoringService) RegisterInverter(inv *model.Inverter) error {
	if inv.RatedPowerKW <= 0 {
		return fmt.Errorf("rated power must be positive: %w", ErrInvalidThreshold)
	}
	if inv.ID == "" || inv.Name == "" || inv.SiteID == "" {
		return errors.New("inverter id, name, site_id are required")
	}
	now := time.Now()
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = now
	}
	if inv.UpdatedAt.IsZero() {
		inv.UpdatedAt = now
	}
	if inv.Status == "" {
		inv.Status = model.StatusOffline
	}
	if inv.CommissionDate.IsZero() {
		inv.CommissionDate = now
	}
	return svc.store.CreateInverter(inv)
}

// GetInverter 获取逆变器信息
func (svc *MonitoringService) GetInverter(id string) (*model.Inverter, error) {
	inv, err := svc.store.GetInverter(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInverterNotFound, id)
	}
	return inv, nil
}

// ListInverters 列出逆变器
func (svc *MonitoringService) ListInverters(siteID string) ([]model.Inverter, error) {
	return svc.store.ListInverters(siteID)
}

// UpdateInverterStatus 更新逆变器状态
func (svc *MonitoringService) UpdateInverterStatus(id string, status model.InverterStatus, tempC float64) error {
	inv, err := svc.store.GetInverter(id)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInverterNotFound, id)
	}
	inv.Status = status
	inv.TemperatureC = tempC
	inv.UpdatedAt = time.Now()
	return svc.store.UpdateInverter(inv)
}

// ---------- 发电数据采集 ----------

// IngestGenerationRecord 采集发电数据
func (svc *MonitoringService) IngestGenerationRecord(rec *model.GenerationRecord) error {
	if rec.InverterID == "" || rec.SiteID == "" {
		return errors.New("inverter_id and site_id are required")
	}
	// 验证逆变器存在
	inv, err := svc.store.GetInverter(rec.InverterID)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInverterNotFound, rec.InverterID)
	}
	if rec.ID == "" {
		rec.ID = fmt.Sprintf("gen-%s-%d", rec.InverterID, rec.Timestamp.UnixMilli())
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	if rec.Timestamp.IsZero() {
		rec.Timestamp = time.Now()
	}
	// 落库
	if err := svc.store.InsertGenerationRecord(rec); err != nil {
		return err
	}
	// 更新缓存
	svc.mu.Lock()
	svc.latestReadings[rec.InverterID] = *rec
	svc.mu.Unlock()

	// 更新逆变器状态和温度
	newStatus := model.StatusOnline
	if rec.FaultCode != "" {
		newStatus = model.StatusFault
	}
	_ = svc.UpdateInverterStatus(inv.ID, newStatus, rec.TemperatureC)

	// 检查告警条件
	svc.checkAlertConditions(inv, rec)

	return nil
}

// BatchIngestGenerationRecords 批量采集
func (svc *MonitoringService) BatchIngestGenerationRecords(recs []model.GenerationRecord) error {
	if len(recs) == 0 {
		return nil
	}
	now := time.Now()
	for i := range recs {
		if recs[i].ID == "" {
			recs[i].ID = fmt.Sprintf("gen-%s-%d-%d", recs[i].InverterID, recs[i].Timestamp.UnixMilli(), i)
		}
		if recs[i].CreatedAt.IsZero() {
			recs[i].CreatedAt = now
		}
	}
	if err := svc.store.BatchInsertGenerationRecords(recs); err != nil {
		return err
	}
	// 更新缓存（最后一条）
	for _, rec := range recs {
		svc.mu.Lock()
		svc.latestReadings[rec.InverterID] = rec
		svc.mu.Unlock()
	}
	return nil
}

// GetLatestReading 获取最新发电读数
func (svc *MonitoringService) GetLatestReading(inverterID string) (model.GenerationRecord, bool) {
	svc.mu.RLock()
	rec, ok := svc.latestReadings[inverterID]
	svc.mu.RUnlock()
	if !ok {
		// 缓存未命中，查 DB
		recs, err := svc.store.GetLatestRecords(inverterID, 1)
		if err == nil && len(recs) > 0 {
			svc.mu.Lock()
			svc.latestReadings[inverterID] = recs[0]
			svc.mu.Unlock()
			return recs[0], true
		}
		return model.GenerationRecord{}, false
	}
	return rec, true
}

// GetGenerationHistory 获取发电历史
func (svc *MonitoringService) GetGenerationHistory(inverterID string, start, end time.Time) ([]model.GenerationRecord, error) {
	if start.After(end) {
		return nil, ErrInvalidDateRange
	}
	return svc.store.GetGenerationRecords(inverterID, start, end)
}

// ---------- 告警管理 ----------

// checkAlertConditions 检查告警条件
func (svc *MonitoringService) checkAlertConditions(inv *model.Inverter, rec *model.GenerationRecord) {
	// 温度过高告警
	if rec.TemperatureC >= ThresholdTempCritical {
		svc.createAlert(inv, rec, model.AlertCritical, "F003",
			fmt.Sprintf("逆变器 %s 临界温度 %.1f°C", inv.Name, rec.TemperatureC),
			rec.TemperatureC, ThresholdTempCritical)
	} else if rec.TemperatureC >= ThresholdTempHigh {
		svc.createAlert(inv, rec, model.AlertWarning, "F003",
			fmt.Sprintf("逆变器 %s 温度过高 %.1f°C", inv.Name, rec.TemperatureC),
			rec.TemperatureC, ThresholdTempHigh)
	}

	// 电压异常告警
	if rec.Voltage > 0 && rec.Voltage >= ThresholdVoltageHigh {
		svc.createAlert(inv, rec, model.AlertCritical, "F002",
			fmt.Sprintf("逆变器 %s DC电压过高 %.1fV", inv.Name, rec.Voltage),
			rec.Voltage, ThresholdVoltageHigh)
	} else if rec.Voltage > 0 && rec.Voltage < ThresholdVoltageLow {
		svc.createAlert(inv, rec, model.AlertWarning, "F001",
			fmt.Sprintf("逆变器 %s DC电压过低 %.1fV", inv.Name, rec.Voltage),
			rec.Voltage, ThresholdVoltageLow)
	}

	// 故障码告警
	if rec.FaultCode != "" {
		fc := model.GetFaultCode(rec.FaultCode)
		if fc != nil {
			svc.createAlert(inv, rec, fc.Severity, fc.Code,
				fmt.Sprintf("逆变器 %s 故障: %s", inv.Name, fc.Description),
				0, 0)
		}
	}
}

// createAlert 创建告警
func (svc *MonitoringService) createAlert(inv *model.Inverter, rec *model.GenerationRecord, level model.AlertLevel, code, msg string, value, threshold float64) {
	alert := model.Alert{
		ID:         fmt.Sprintf("alert-%s-%d", inv.ID, time.Now().UnixNano()),
		InverterID: inv.ID,
		SiteID:     inv.SiteID,
		Level:      level,
		Status:     model.AlertActive,
		Code:       code,
		Message:    msg,
		Value:      value,
		Threshold:  threshold,
		Timestamp:  rec.Timestamp,
		CreatedAt:  time.Now(),
	}
	_ = svc.store.CreateAlert(&alert)

	// 更新缓存
	svc.alertMu.Lock()
	svc.alertCache[inv.ID] = append(svc.alertCache[inv.ID], alert)
	svc.alertMu.Unlock()

	// 非阻塞发送通知
	select {
	case svc.notifyCh <- alert:
	default:
	}
}

// AcknowledgeAlert 确认告警
func (svc *MonitoringService) AcknowledgeAlert(alertID string, ackBy string) error {
	alert, err := svc.store.GetAlert(alertID)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrAlertNotFound, alertID)
	}
	if alert.Status != model.AlertActive {
		return fmt.Errorf("alert %s is not active (current: %s)", alertID, alert.Status)
	}
	return svc.store.AcknowledgeAlert(alertID, ackBy, time.Now())
}

// ClearAlert 清除告警
func (svc *MonitoringService) ClearAlert(alertID string) error {
	_, err := svc.store.GetAlert(alertID)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrAlertNotFound, alertID)
	}
	return svc.store.ClearAlert(alertID, time.Now())
}

// ListAlerts 列出告警
func (svc *MonitoringService) ListAlerts(status string, inverterID string) ([]model.Alert, error) {
	return svc.store.ListAlerts(status, inverterID)
}

// ---------- 维保工单 ----------

// CreateMaintenanceTask 创建维保工单
func (svc *MonitoringService) CreateMaintenanceTask(task *model.MaintenanceTask) error {
	if task.InverterID == "" || task.Type == "" {
		return errors.New("inverter_id and type are required")
	}
	// 验证逆变器存在
	_, err := svc.store.GetInverter(task.InverterID)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrInverterNotFound, task.InverterID)
	}
	now := time.Now()
	if task.ID == "" {
		task.ID = fmt.Sprintf("maint-%s-%d", task.InverterID, now.UnixNano())
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = now
	}
	if task.Status == "" {
		task.Status = model.MaintPending
	}
	if task.Priority < 1 || task.Priority > 10 {
		task.Priority = 5
	}
	return svc.store.CreateMaintenanceTask(task)
}

// GetMaintenanceTask 获取维保工单
func (svc *MonitoringService) GetMaintenanceTask(id string) (*model.MaintenanceTask, error) {
	task, err := svc.store.GetMaintenanceTask(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrMaintenanceNotFound, id)
	}
	return task, nil
}

// ListMaintenanceTasks 列出维保工单
func (svc *MonitoringService) ListMaintenanceTasks(status string, inverterID string) ([]model.MaintenanceTask, error) {
	return svc.store.ListMaintenanceTasks(status, inverterID)
}

// UpdateMaintenanceTask 更新维保工单
func (svc *MonitoringService) UpdateMaintenanceTask(task *model.MaintenanceTask) error {
	task.UpdatedAt = time.Now()
	return svc.store.UpdateMaintenanceTask(task)
}

// CompleteMaintenanceTask 完成维保工单
func (svc *MonitoringService) CompleteMaintenanceTask(id string, notes string) error {
	task, err := svc.store.GetMaintenanceTask(id)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrMaintenanceNotFound, id)
	}
	if task.Status == model.MaintCompleted {
		return fmt.Errorf("task %s already completed", id)
	}
	now := time.Now()
	task.Status = model.MaintCompleted
	task.CompletedAt = &now
	task.Notes = notes
	task.UpdatedAt = now
	if err := svc.store.UpdateMaintenanceTask(task); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	return nil
}

// CancelMaintenanceTask 取消维保工单
func (svc *MonitoringService) CancelMaintenanceTask(id string, reason string) error {
	task, err := svc.store.GetMaintenanceTask(id)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrMaintenanceNotFound, id)
	}
	if task.Status == model.MaintCompleted {
		return fmt.Errorf("cannot cancel completed task %s", id)
	}
	task.Status = model.MaintCancelled
	task.Notes = reason
	task.UpdatedAt = time.Now()
	return svc.store.UpdateMaintenanceTask(task)
}

// AssignMaintenanceTask 分配维保工单
func (svc *MonitoringService) AssignMaintenanceTask(id string, assignee string) error {
	task, err := svc.store.GetMaintenanceTask(id)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrMaintenanceNotFound, id)
	}
	if task.Status == model.MaintCompleted || task.Status == model.MaintCancelled {
		return fmt.Errorf("cannot assign task %s in status %s", id, task.Status)
	}
	task.AssignedTo = assignee
	if task.Status == model.MaintPending {
		task.Status = model.MaintScheduled
	}
	task.UpdatedAt = time.Now()
	return svc.store.UpdateMaintenanceTask(task)
}

// ---------- 清洗排程 ----------

// EvaluateCleaningNeed 评估清洗需求
func (svc *MonitoringService) EvaluateCleaningNeed(inverterID string, weatherData *model.WeatherData) (*model.CleaningSchedule, error) {
	inv, err := svc.store.GetInverter(inverterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInverterNotFound, inverterID)
	}

	// 获取最后维保（清洗）时间
	lastMaint, _ := svc.store.GetLastMaintenance(inverterID)
	daysSinceClean := 0
	if lastMaint != nil {
		daysSinceClean = int(time.Since(*lastMaint).Hours() / 24)
	}

	// 如果有降雨数据，更新天数（降雨大于阈值视为自然清洗）
	rainfall := 0.0
	if weatherData != nil {
		rainfall = weatherData.Rainfall
	}
	if rainfall >= ThresholdRainfall {
		// 大雨清洗后重置天数
		daysSinceClean = 0
	}

	// 评估积尘等级
	dustGrade := model.DustLight
	if daysSinceClean >= 45 {
		dustGrade = model.DustHeavy
	} else if daysSinceClean >= ThresholdDustDays {
		dustGrade = model.DustMedium
	}

	// 计算效率下降百分比
	efficiencyDrop := 0.0
	if daysSinceClean > 0 {
		// 每天约 0.3% 效率下降，重积尘后加速
		dailyRate := 0.3
		if dustGrade == model.DustHeavy {
			dailyRate = 0.5
		} else if dustGrade == model.DustMedium {
			dailyRate = 0.4
		}
		efficiencyDrop = float64(daysSinceClean) * dailyRate
		if efficiencyDrop > 20 {
			efficiencyDrop = 20
		}
	}

	// 判断是否需要清洗
	needsClean := daysSinceClean >= ThresholdDustDays || efficiencyDrop >= 5.0

	schedule := &model.CleaningSchedule{
		InverterID:      inverterID,
		SiteID:          inv.SiteID,
		DustGrade:       dustGrade,
		LastRainfall:    rainfall,
		DaysSinceClean:  daysSinceClean,
		EfficiencyDrop:  efficiencyDrop,
	}

	if needsClean {
		schedule.Status = model.CleaningPending
		// 根据积尘等级排程
		scheduledDays := 14
		if dustGrade == model.DustMedium {
			scheduledDays = 7
		} else if dustGrade == model.DustHeavy {
			scheduledDays = 3
		}
		schedule.ScheduledFor = time.Now().AddDate(0, 0, scheduledDays)
	} else {
		schedule.Status = model.CleaningSkipped
		schedule.ScheduledFor = time.Now().AddDate(0, 0, 30)
	}

	now := time.Now()
	schedule.ID = fmt.Sprintf("clean-%s-%d", inverterID, now.UnixNano())
	schedule.CreatedAt = now
	schedule.UpdatedAt = now

	if err := svc.store.CreateCleaningSchedule(schedule); err != nil {
		return nil, err
	}
	return schedule, nil
}

// GetCleaningSchedule 获取清洗排程
func (svc *MonitoringService) GetCleaningSchedule(id string) (*model.CleaningSchedule, error) {
	cs, err := svc.store.GetCleaningSchedule(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrCleaningNotFound, id)
	}
	return cs, nil
}

// ListCleaningSchedules 列出清洗排程
func (svc *MonitoringService) ListCleaningSchedules(status string, siteID string) ([]model.CleaningSchedule, error) {
	return svc.store.ListCleaningSchedules(status, siteID)
}

// CompleteCleaning 完成清洗
func (svc *MonitoringService) CompleteCleaning(id string, notes string) error {
	cs, err := svc.store.GetCleaningSchedule(id)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrCleaningNotFound, id)
	}
	now := time.Now()
	cs.Status = model.CleaningCompleted
	cs.CompletedAt = &now
	cs.Notes = notes
	cs.UpdatedAt = now
	return svc.store.UpdateCleaningSchedule(cs)
}

// ---------- 气象数据 ----------

// RecordWeatherData 记录气象数据
func (svc *MonitoringService) RecordWeatherData(w *model.WeatherData) error {
	if w.SiteID == "" {
		return errors.New("site_id is required")
	}
	if w.ID == "" {
		w.ID = fmt.Sprintf("weather-%s-%d", w.SiteID, w.Timestamp.UnixMilli())
	}
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now()
	}
	if w.Timestamp.IsZero() {
		w.Timestamp = time.Now()
	}
	return svc.store.InsertWeatherData(w)
}

// GetWeatherHistory 获取气象历史
func (svc *MonitoringService) GetWeatherHistory(siteID string, start, end time.Time) ([]model.WeatherData, error) {
	if start.After(end) {
		return nil, ErrInvalidDateRange
	}
	return svc.store.GetWeatherData(siteID, start, end)
}

// ---------- 发电统计与效率分析 ----------

// CalculateDailySummary 计算日发电汇总
func (svc *MonitoringService) CalculateDailySummary(inverterID string, date time.Time) (*model.DailyGenerationSummary, error) {
	inv, err := svc.store.GetInverter(inverterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInverterNotFound, inverterID)
	}

	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.Add(24 * time.Hour)

	records, err := svc.store.GetGenerationRecords(inverterID, start, end)
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return &model.DailyGenerationSummary{
			InverterID: inverterID,
			SiteID:     inv.SiteID,
			Date:       start,
		}, nil
	}

	var totalEnergy, peakPower, sumPower, downtime float64
	for i, rec := range records {
		totalEnergy += rec.EnergyKWh
		if rec.PowerKW > peakPower {
			peakPower = rec.PowerKW
		}
		sumPower += rec.PowerKW
		// 间隔超 10 分钟视为停机
		if i > 0 {
			gap := records[i].Timestamp.Sub(records[i-1].Timestamp).Hours()
			if gap > 10.0/60.0 {
				downtime += gap
			}
		}
	}
	avgPower := sumPower / float64(len(records))

	// 理论发电量 = 额定功率 × 有效日照小时数 × 系统效率
	daylightHours := svc.estimateDaylightHours(inv.SiteID, date)
	expectedEnergy := inv.RatedPowerKW * daylightHours * 0.75 // 系统效率 75%

	// 性能比 PR = 实际发电 / 理论发电
	pr := 0.0
	if expectedEnergy > 0 {
		pr = totalEnergy / expectedEnergy
	}

	summary := &model.DailyGenerationSummary{
		ID:              fmt.Sprintf("summary-%s-%s", inverterID, start.Format("2006-01-02")),
		SiteID:          inv.SiteID,
		InverterID:      inverterID,
		Date:            start,
		TotalEnergy:     totalEnergy,
		PeakPower:       peakPower,
		AvgPower:        avgPower,
		ExpectedEnergy:  expectedEnergy,
		PerformanceRatio: pr,
		Downtime:        downtime,
		RecordCount:     len(records),
		CreatedAt:       time.Now(),
	}

	// 缓存
	svc.summaryMu.Lock()
	svc.summaryCache[inverterID+start.Format("2006-01-02")] = *summary
	svc.summaryMu.Unlock()

	if err := svc.store.SaveDailySummary(summary); err != nil {
		return nil, err
	}
	return summary, nil
}

// estimateDaylightHours 估算有效日照小时数（简化的季节模型）
func (svc *MonitoringService) estimateDaylightHours(siteID string, date time.Time) float64 {
	// 简化模型：按月份估算（北纬 30-40 度）
	month := int(date.Month())
	// 春分秋分附近 ~12h，夏至 ~14h，冬至 ~10h
	hours := []float64{10.5, 11.0, 11.8, 12.5, 13.3, 14.0, 13.8, 13.0, 12.2, 11.5, 10.8, 10.3}
	return hours[month-1]
}

// CalculatePerformanceRatio 计算性能比
func (svc *MonitoringService) CalculatePerformanceRatio(inverterID string, start, end time.Time) (float64, error) {
	inv, err := svc.store.GetInverter(inverterID)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrInverterNotFound, inverterID)
	}
	records, err := svc.store.GetGenerationRecords(inverterID, start, end)
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}
	var actualEnergy float64
	for _, rec := range records {
		actualEnergy += rec.EnergyKWh
	}
	// 理论发电量 = 额定功率 × 天数 × 日照小时 × 系统效率
	days := end.Sub(start).Hours() / 24
	daylight := svc.estimateDaylightHours(inv.SiteID, start)
	expected := inv.RatedPowerKW * days * daylight * 0.75
	if expected == 0 {
		return 0, nil
	}
	return actualEnergy / expected, nil
}

// GetEfficiencyReport 获取效率报告
func (svc *MonitoringService) GetEfficiencyReport(inverterID string, date time.Time) (*model.EfficiencyReport, error) {
	inv, err := svc.store.GetInverter(inverterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInverterNotFound, inverterID)
	}

	summary, err := svc.CalculateDailySummary(inverterID, date)
	if err != nil {
		return nil, err
	}

	// 计算各类损失
	tempLoss := 0.0
	records, _ := svc.store.GetGenerationRecords(inverterID,
		time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location()),
		time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location()).Add(24*time.Hour))
	for _, rec := range records {
		if rec.TemperatureC > DeratingTempStart {
			deratingFactor := (rec.TemperatureC - DeratingTempStart) / (DeratingTempFull - DeratingTempStart)
			if deratingFactor > 1 {
				deratingFactor = 1
			}
			tempLoss += rec.EnergyKWh * deratingFactor * 0.1 // 温度降额损失约 10%
		}
	}

	// 积尘损失
	dustLoss := summary.ExpectedEnergy * 0.03 // 默认 3% 积尘损失

	// 逆变器转换损失
	inverterLoss := summary.ExpectedEnergy * 0.04 // 逆变器效率 ~96%

	totalLoss := tempLoss + dustLoss + inverterLoss
	efficiency := 0.0
	if summary.ExpectedEnergy > 0 {
		efficiency = (summary.TotalEnergy / summary.ExpectedEnergy) * 100
		if efficiency > 100 {
			efficiency = 100
		}
	}

	return &model.EfficiencyReport{
		InverterID:       inverterID,
		Name:             inv.Name,
		Date:             date,
		ActualEnergy:     summary.TotalEnergy,
		ExpectedEnergy:   summary.ExpectedEnergy,
		PerformanceRatio: summary.PerformanceRatio,
		TempDeratingLoss: tempLoss,
		DustLoss:         dustLoss,
		InverterLoss:     inverterLoss,
		TotalLoss:        totalLoss,
		EfficiencyPct:    efficiency,
	}, nil
}

// ---------- 逆变器健康度 ----------

// GetInverterHealth 获取逆变器健康度
func (svc *MonitoringService) GetInverterHealth(inverterID string) (*model.InverterHealth, error) {
	inv, err := svc.store.GetInverter(inverterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInverterNotFound, inverterID)
	}

	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// 今日发电
	todayEnergy, _ := svc.store.GetInverterDailyEnergy(inverterID, today)

	// 活跃告警数
	activeAlerts, _ := svc.store.CountActiveAlerts(inverterID)

	// 30天故障数
	faultCount, _ := svc.store.CountFaults(inverterID, now.AddDate(0, 0, -30))

	// 最后维保时间
	lastMaint, _ := svc.store.GetLastMaintenance(inverterID)
	daysSinceMaint := 0
	if lastMaint != nil {
		daysSinceMaint = int(now.Sub(*lastMaint).Hours() / 24)
	}

	// 温度降额
	tempDerating := inv.TemperatureC > DeratingTempStart

	// 健康度评分（0-100）
	healthScore := 100.0
	healthScore -= float64(activeAlerts) * 10           // 每个告警扣 10
	healthScore -= float64(faultCount) * 5               // 每个故障扣 5
	if daysSinceMaint > 180 {
		healthScore -= 10                                   // 超半年未维保扣 10
	}
	if tempDerating {
		deratingPct := (inv.TemperatureC - DeratingTempStart) / (DeratingTempFull - DeratingTempStart) * 100
		healthScore -= deratingPct * 0.2
	}
	if healthScore < 0 {
		healthScore = 0
	}

	// 效率
	efficiency := 0.0
	if inv.RatedPowerKW > 0 && todayEnergy > 0 {
		efficiency = (todayEnergy / (inv.RatedPowerKW * svc.estimateDaylightHours(inv.SiteID, today))) * 100
		if efficiency > 100 {
			efficiency = 100
		}
	}

	return &model.InverterHealth{
		InverterID:       inv.ID,
		Name:             inv.Name,
		Status:           inv.Status,
		HealthScore:      math.Round(healthScore*100) / 100,
		EfficiencyPct:    math.Round(efficiency*100) / 100,
		TempDerating:     tempDerating,
		ActiveAlerts:     activeAlerts,
		LastMaintenance:  lastMaint,
		DaysSinceMaint:   daysSinceMaint,
		TotalEnergyToday: todayEnergy,
		FaultCount30d:    faultCount,
	}, nil
}

// GetSiteStats 获取电站统计
func (svc *MonitoringService) GetSiteStats(siteID string, start, end time.Time) (*model.GenerationStats, error) {
	_, err := svc.store.GetSite(siteID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSiteNotFound, siteID)
	}
	if start.After(end) {
		return nil, ErrInvalidDateRange
	}
	stats, err := svc.store.GetSiteGenerationStats(siteID, start, end)
	if err != nil {
		return nil, err
	}
	// 计算性能比
	inveters, _ := svc.store.ListInverters(siteID)
	totalExpected := 0.0
	for _, inv := range inveters {
		days := end.Sub(start).Hours() / 24
		daylight := svc.estimateDaylightHours(siteID, start)
		totalExpected += inv.RatedPowerKW * days * daylight * 0.75
	}
	if totalExpected > 0 {
		stats.ExpectedTotal = totalExpected
		stats.PerformanceRatio = stats.TotalEnergy / totalExpected
	}
	stats.Period = fmt.Sprintf("%s ~ %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	return stats, nil
}

// ListDailySummaries 列出日发电汇总
func (svc *MonitoringService) ListDailySummaries(siteID string, start, end time.Time) ([]model.DailyGenerationSummary, error) {
	_, err := svc.store.GetSite(siteID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrSiteNotFound, siteID)
	}
	return svc.store.GetDailySummaries(siteID, start, end)
}

// ---------- 后台监控 goroutine ----------

// StartBackgroundMonitor 启动后台监控
func (svc *MonitoringService) StartBackgroundMonitor(ctx interface{ Done() <-chan struct{} }) {
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				svc.runPeriodicCheck()
			case alert := <-svc.notifyCh:
				// 异步处理告警通知（写入维保建议等）
				svc.handleAlertNotification(alert)
			}
		}
	}()
}

// runPeriodicCheck 周期检查
func (svc *MonitoringService) runPeriodicCheck() {
	// 检查所有在线逆变器是否有数据上报超时
	// 检查所有活跃告警是否需要自动升级
	// 检查清洗排程是否到期
	// 实际实现简化
}

// handleAlertNotification 处理告警通知
func (svc *MonitoringService) handleAlertNotification(alert model.Alert) {
	// critical 告警自动创建维保工单
	if alert.Level == model.AlertCritical {
		task := &model.MaintenanceTask{
			InverterID: alert.InverterID,
			SiteID:     alert.SiteID,
			Type:       "fault_repair",
			Description: fmt.Sprintf("自动创建: %s", alert.Message),
			Status:     model.MaintPending,
			Priority:   1,
			ScheduledFor: time.Now().Add(2 * time.Hour),
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		task.ID = fmt.Sprintf("maint-auto-%s-%d", alert.InverterID, time.Now().UnixNano())
		_ = svc.store.CreateMaintenanceTask(task)
	}
}
