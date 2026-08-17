package model

import "time"

// InverterStatus 逆变器状态
type InverterStatus string

const (
	StatusOnline    InverterStatus = "online"
	StatusOffline   InverterStatus = "offline"
	StatusFault     InverterStatus = "fault"
	StatusStandby   InverterStatus = "standby"
	StatusMaintain  InverterStatus = "maintain"
)

// AlertLevel 告警等级
type AlertLevel string

const (
	AlertInfo     AlertLevel = "info"
	AlertWarning  AlertLevel = "warning"
	AlertCritical AlertLevel = "critical"
)

// AlertStatus 告警状态
type AlertStatus string

const (
	AlertActive    AlertStatus = "active"
	AlertAcknowledged AlertStatus = "acknowledged"
	AlertCleared   AlertStatus = "cleared"
)

// MaintenanceStatus 维保状态
type MaintenanceStatus string

const (
	MaintPending   MaintenanceStatus = "pending"
	MaintScheduled MaintenanceStatus = "scheduled"
	MaintInProgress MaintenanceStatus = "in_progress"
	MaintCompleted MaintenanceStatus = "completed"
	MaintCancelled MaintenanceStatus = "cancelled"
)

// CleaningStatus 清洗状态
type CleaningStatus string

const (
	CleaningPending   CleaningStatus = "pending"
	CleaningScheduled CleaningStatus = "scheduled"
	CleaningCompleted CleaningStatus = "completed"
	CleaningSkipped   CleaningStatus = "skipped"
)

// DustGrade 积尘等级
type DustGrade string

const (
	DustLight DustGrade = "light"
	DustMedium DustGrade = "medium"
	DustHeavy  DustGrade = "heavy"
)

// Inverter 逆变器
type Inverter struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	SiteID       string         `json:"site_id"`
	Model        string         `json:"model"`
	Manufacturer string         `json:"manufacturer"`
	RatedPowerKW float64        `json:"rated_power_kw"`
	Status       InverterStatus `json:"status"`
	TemperatureC float64        `json:"temperature_c"`
	FirmwareVer  string         `json:"firmware_ver"`
	CommissionDate time.Time    `json:"commission_date"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// GenerationRecord 发电记录
type GenerationRecord struct {
	ID           string    `json:"id"`
	InverterID   string    `json:"inverter_id"`
	SiteID       string    `json:"site_id"`
	Timestamp    time.Time `json:"timestamp"`
	PowerKW      float64   `json:"power_kw"`
	EnergyKWh    float64   `json:"energy_kwh"`
	Voltage      float64   `json:"voltage"`
	Current      float64   `json:"current"`
	TemperatureC float64   `json:"temperature_c"`
	Irradiance   float64   `json:"irradiance"`
	FaultCode    string    `json:"fault_code"`
	CreatedAt    time.Time `json:"created_at"`
}

// Site 电站
type Site struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Location     string    `json:"location"`
	Latitude     float64   `json:"latitude"`
	Longitude    float64   `json:"longitude"`
	CapacityKW   float64   `json:"capacity_kw"`
	Timezone     string    `json:"timezone"`
	CommissionDate time.Time `json:"commission_date"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// Alert 告警
type Alert struct {
	ID           string      `json:"id"`
	InverterID   string      `json:"inverter_id"`
	SiteID       string      `json:"site_id"`
	Level        AlertLevel  `json:"level"`
	Status       AlertStatus `json:"status"`
	Code         string      `json:"code"`
	Message      string      `json:"message"`
	Value        float64     `json:"value"`
	Threshold    float64     `json:"threshold"`
	Timestamp    time.Time   `json:"timestamp"`
	AcknowledgedBy string    `json:"acknowledged_by"`
	AcknowledgedAt *time.Time `json:"acknowledged_at"`
	ClearedAt    *time.Time  `json:"cleared_at"`
	CreatedAt    time.Time   `json:"created_at"`
}

// MaintenanceTask 维保工单
type MaintenanceTask struct {
	ID           string            `json:"id"`
	InverterID   string            `json:"inverter_id"`
	SiteID       string            `json:"site_id"`
	Type         string            `json:"type"`
	Description  string            `json:"description"`
	Status       MaintenanceStatus `json:"status"`
	Priority     int               `json:"priority"`
	ScheduledFor time.Time         `json:"scheduled_for"`
	AssignedTo   string            `json:"assigned_to"`
	CompletedAt  *time.Time        `json:"completed_at"`
	Notes        string            `json:"notes"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// CleaningSchedule 清洗排程
type CleaningSchedule struct {
	ID           string         `json:"id"`
	SiteID       string         `json:"site_id"`
	InverterID   string         `json:"inverter_id"`
	Status       CleaningStatus `json:"status"`
	DustGrade    DustGrade      `json:"dust_grade"`
	LastRainfall float64        `json:"last_rainfall_mm"`
	DaysSinceClean int          `json:"days_since_clean"`
	EfficiencyDrop float64      `json:"efficiency_drop_pct"`
	ScheduledFor time.Time      `json:"scheduled_for"`
	CompletedAt  *time.Time     `json:"completed_at"`
	Notes        string         `json:"notes"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// WeatherData 气象数据
type WeatherData struct {
	ID           string    `json:"id"`
	SiteID       string    `json:"site_id"`
	Timestamp    time.Time `json:"timestamp"`
	TemperatureC float64   `json:"temperature_c"`
	Humidity     float64   `json:"humidity"`
	Irradiance   float64   `json:"irradiance"`
	WindSpeed    float64   `json:"wind_speed"`
	Rainfall     float64   `json:"rainfall_mm"`
	CloudCover   float64   `json:"cloud_cover_pct"`
	CreatedAt    time.Time `json:"created_at"`
}

// DailyGenerationSummary 日发电汇总
type DailyGenerationSummary struct {
	ID            string    `json:"id"`
	SiteID        string    `json:"site_id"`
	InverterID    string    `json:"inverter_id"`
	Date          time.Time `json:"date"`
	TotalEnergy   float64   `json:"total_energy_kwh"`
	PeakPower     float64   `json:"peak_power_kw"`
	AvgPower      float64   `json:"avg_power_kw"`
	ExpectedEnergy float64  `json:"expected_energy_kwh"`
	PerformanceRatio float64 `json:"performance_ratio"`
	Downtime      float64   `json:"downtime_hours"`
	RecordCount   int       `json:"record_count"`
	CreatedAt     time.Time `json:"created_at"`
}

// InverterHealth 逆变器健康度
type InverterHealth struct {
	InverterID       string  `json:"inverter_id"`
	Name             string  `json:"name"`
	Status           InverterStatus `json:"status"`
	HealthScore      float64 `json:"health_score"`
	EfficiencyPct    float64 `json:"efficiency_pct"`
	TempDerating     bool    `json:"temp_derating"`
	ActiveAlerts     int     `json:"active_alerts"`
	LastMaintenance  *time.Time `json:"last_maintenance"`
	DaysSinceMaint   int     `json:"days_since_maintenance"`
	TotalEnergyToday float64 `json:"total_energy_today_kwh"`
	FaultCount30d    int     `json:"fault_count_30d"`
}

// FaultCodeEntry 故障码定义
type FaultCodeEntry struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Severity    AlertLevel `json:"severity"`
	Action      string `json:"recommended_action"`
}

// FaultCodeCatalog 故障码目录
var FaultCodeCatalog = []FaultCodeEntry{
	{"F001", "DC电压过低", "electrical", AlertWarning, "检查光伏组串连接"},
	{"F002", "DC电压过高", "electrical", AlertCritical, "立即断开并检查组串"},
	{"F003", "逆变器过温", "thermal", AlertCritical, "检查散热系统"},
	{"F004", "电网频率异常", "grid", AlertWarning, "联系电网公司"},
	{"F005", "绝缘阻抗低", "safety", AlertCritical, "检查线缆绝缘"},
	{"F006", "PV组串失配", "electrical", AlertInfo, "检查组串电流"},
	{"F007", "通信中断", "communication", AlertWarning, "检查通信链路"},
	{"F008", "孤岛保护", "grid", AlertCritical, "电网恢复后自动复位"},
	{"F009", "ARC故障", "safety", AlertCritical, "检查直流连接器"},
	{"F010", "存储器错误", "hardware", AlertCritical, "联系厂家更换"},
}

// GetFaultCode 查找故障码定义
func GetFaultCode(code string) *FaultCodeEntry {
	for i := range FaultCodeCatalog {
		if FaultCodeCatalog[i].Code == code {
			return &FaultCodeCatalog[i]
		}
	}
	return nil
}

// GenerationStats 发电统计
type GenerationStats struct {
	SiteID           string  `json:"site_id"`
	Period           string  `json:"period"`
	TotalEnergy      float64 `json:"total_energy_kwh"`
	AvgDailyEnergy   float64 `json:"avg_daily_energy_kwh"`
	AvgPower         float64 `json:"avg_power_kw"`
	PeakPower        float64 `json:"peak_power_kw"`
	ExpectedTotal    float64 `json:"expected_total_kwh"`
	PerformanceRatio float64 `json:"performance_ratio"`
	Downtime         float64 `json:"downtime_hours"`
	FaultCount       int     `json:"fault_count"`
	RecordCount      int     `json:"record_count"`
}

// EfficiencyReport 效率报告
type EfficiencyReport struct {
	InverterID       string  `json:"inverter_id"`
	Name             string  `json:"name"`
	Date             time.Time `json:"date"`
	ActualEnergy     float64 `json:"actual_energy_kwh"`
	ExpectedEnergy   float64 `json:"expected_energy_kwh"`
	PerformanceRatio float64 `json:"performance_ratio"`
	TempDeratingLoss float64 `json:"temp_derating_loss_kwh"`
	DustLoss         float64 `json:"dust_loss_kwh"`
	InverterLoss     float64 `json:"inverter_loss_kwh"`
	TotalLoss        float64 `json:"total_loss_kwh"`
	EfficiencyPct    float64 `json:"efficiency_pct"`
}

// Pagination 分页
type Pagination struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
	Total    int `json:"total"`
}

// APIResponse 统一响应
type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}
