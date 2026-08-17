package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"solar-ops/internal/model"
)

// Store 数据存储层，基于 SQLite 文件模式
type Store struct {
	db *sql.DB
}

// NewStore 创建存储实例，dsn 指向 SQLite 文件路径
func NewStore(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// 启用外键约束
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("pragma foreign_keys: %w", err)
	}
	// WAL 模式提升并发读
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("pragma journal_mode: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close 关闭数据库
func (s *Store) Close() error {
	return s.db.Close()
}

// DB 暴露底层 *sql.DB（仅内部使用）
func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS sites (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			location TEXT NOT NULL,
			latitude REAL NOT NULL,
			longitude REAL NOT NULL,
			capacity_kw REAL NOT NULL,
			timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai',
			commission_date DATETIME NOT NULL,
			active INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS inverters (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			site_id TEXT NOT NULL,
			model TEXT NOT NULL,
			manufacturer TEXT NOT NULL,
			rated_power_kw REAL NOT NULL,
			status TEXT NOT NULL DEFAULT 'offline',
			temperature_c REAL NOT NULL DEFAULT 0,
			firmware_ver TEXT NOT NULL DEFAULT '',
			commission_date DATETIME NOT NULL,
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY (site_id) REFERENCES sites(id)
		)`,
		`CREATE TABLE IF NOT EXISTS generation_records (
			id TEXT PRIMARY KEY,
			inverter_id TEXT NOT NULL,
			site_id TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			power_kw REAL NOT NULL,
			energy_kwh REAL NOT NULL,
			voltage REAL NOT NULL,
			current REAL NOT NULL,
			temperature_c REAL NOT NULL,
			irradiance REAL NOT NULL,
			fault_code TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			FOREIGN KEY (inverter_id) REFERENCES inverters(id),
			FOREIGN KEY (site_id) REFERENCES sites(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_gen_inverter ON generation_records(inverter_id, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_gen_site ON generation_records(site_id, timestamp)`,
		`CREATE TABLE IF NOT EXISTS alerts (
			id TEXT PRIMARY KEY,
			inverter_id TEXT NOT NULL,
			site_id TEXT NOT NULL,
			level TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			code TEXT NOT NULL,
			message TEXT NOT NULL,
			value REAL NOT NULL DEFAULT 0,
			threshold REAL NOT NULL DEFAULT 0,
			timestamp DATETIME NOT NULL,
			acknowledged_by TEXT NOT NULL DEFAULT '',
			acknowledged_at DATETIME,
			cleared_at DATETIME,
			created_at DATETIME NOT NULL,
			FOREIGN KEY (inverter_id) REFERENCES inverters(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_alerts_status ON alerts(status, level)`,
		`CREATE INDEX IF NOT EXISTS idx_alerts_inverter ON alerts(inverter_id, status)`,
		`CREATE TABLE IF NOT EXISTS maintenance_tasks (
			id TEXT PRIMARY KEY,
			inverter_id TEXT NOT NULL,
			site_id TEXT NOT NULL,
			type TEXT NOT NULL,
			description TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			priority INTEGER NOT NULL DEFAULT 5,
			scheduled_for DATETIME NOT NULL,
			assigned_to TEXT NOT NULL DEFAULT '',
			completed_at DATETIME,
			notes TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY (inverter_id) REFERENCES inverters(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_maint_status ON maintenance_tasks(status, scheduled_for)`,
		`CREATE TABLE IF NOT EXISTS cleaning_schedules (
			id TEXT PRIMARY KEY,
			site_id TEXT NOT NULL,
			inverter_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			dust_grade TEXT NOT NULL DEFAULT 'light',
			last_rainfall_mm REAL NOT NULL DEFAULT 0,
			days_since_clean INTEGER NOT NULL DEFAULT 0,
			efficiency_drop_pct REAL NOT NULL DEFAULT 0,
			scheduled_for DATETIME NOT NULL,
			completed_at DATETIME,
			notes TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			FOREIGN KEY (inverter_id) REFERENCES inverters(id)
		)`,
		`CREATE TABLE IF NOT EXISTS weather_data (
			id TEXT PRIMARY KEY,
			site_id TEXT NOT NULL,
			timestamp DATETIME NOT NULL,
			temperature_c REAL NOT NULL,
			humidity REAL NOT NULL,
			irradiance REAL NOT NULL,
			wind_speed REAL NOT NULL,
			rainfall_mm REAL NOT NULL,
			cloud_cover_pct REAL NOT NULL,
			created_at DATETIME NOT NULL,
			FOREIGN KEY (site_id) REFERENCES sites(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_weather_site ON weather_data(site_id, timestamp)`,
		`CREATE TABLE IF NOT EXISTS daily_summaries (
			id TEXT PRIMARY KEY,
			site_id TEXT NOT NULL,
			inverter_id TEXT NOT NULL,
			date DATETIME NOT NULL,
			total_energy_kwh REAL NOT NULL,
			peak_power_kw REAL NOT NULL,
			avg_power_kw REAL NOT NULL,
			expected_energy_kwh REAL NOT NULL,
			performance_ratio REAL NOT NULL,
			downtime_hours REAL NOT NULL,
			record_count INTEGER NOT NULL,
			created_at DATETIME NOT NULL,
			FOREIGN KEY (inverter_id) REFERENCES inverters(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_summary_date ON daily_summaries(site_id, date)`,
	}
	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("exec migration: %w\nSQL: %s", err, m)
		}
	}
	return nil
}

// ---------- Site CRUD ----------

func (s *Store) CreateSite(site *model.Site) error {
	_, err := s.db.Exec(
		`INSERT INTO sites (id, name, location, latitude, longitude, capacity_kw, timezone, commission_date, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		site.ID, site.Name, site.Location, site.Latitude, site.Longitude,
		site.CapacityKW, site.Timezone, site.CommissionDate, site.Active, site.CreatedAt, site.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create site: %w", err)
	}
	return nil
}

func (s *Store) GetSite(id string) (*model.Site, error) {
	row := s.db.QueryRow(
		`SELECT id, name, location, latitude, longitude, capacity_kw, timezone, commission_date, active, created_at, updated_at
		 FROM sites WHERE id = ?`, id)
	var site model.Site
	var active int
	err := row.Scan(&site.ID, &site.Name, &site.Location, &site.Latitude, &site.Longitude,
		&site.CapacityKW, &site.Timezone, &site.CommissionDate, &active, &site.CreatedAt, &site.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get site %s: %w", id, err)
	}
	site.Active = active == 1
	return &site, nil
}

func (s *Store) ListSites() ([]model.Site, error) {
	rows, err := s.db.Query(
		`SELECT id, name, location, latitude, longitude, capacity_kw, timezone, commission_date, active, created_at, updated_at
		 FROM sites ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	defer rows.Close()
	var sites []model.Site
	for rows.Next() {
		var site model.Site
		var active int
		if err := rows.Scan(&site.ID, &site.Name, &site.Location, &site.Latitude, &site.Longitude,
			&site.CapacityKW, &site.Timezone, &site.CommissionDate, &active, &site.CreatedAt, &site.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan site: %w", err)
		}
		site.Active = active == 1
		sites = append(sites, site)
	}
	return sites, nil
}

func (s *Store) UpdateSite(site *model.Site) error {
	_, err := s.db.Exec(
		`UPDATE sites SET name=?, location=?, latitude=?, longitude=?, capacity_kw=?, active=?, updated_at=?
		 WHERE id = ?`,
		site.Name, site.Location, site.Latitude, site.Longitude, site.CapacityKW, site.Active, site.UpdatedAt, site.ID)
	if err != nil {
		return fmt.Errorf("update site: %w", err)
	}
	return nil
}

// ---------- Inverter CRUD ----------

func (s *Store) CreateInverter(inv *model.Inverter) error {
	_, err := s.db.Exec(
		`INSERT INTO inverters (id, name, site_id, model, manufacturer, rated_power_kw, status, temperature_c, firmware_ver, commission_date, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inv.ID, inv.Name, inv.SiteID, inv.Model, inv.Manufacturer, inv.RatedPowerKW,
		inv.Status, inv.TemperatureC, inv.FirmwareVer, inv.CommissionDate, inv.CreatedAt, inv.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create inverter: %w", err)
	}
	return nil
}

func (s *Store) GetInverter(id string) (*model.Inverter, error) {
	row := s.db.QueryRow(
		`SELECT id, name, site_id, model, manufacturer, rated_power_kw, status, temperature_c, firmware_ver, commission_date, created_at, updated_at
		 FROM inverters WHERE id = ?`, id)
	var inv model.Inverter
	err := row.Scan(&inv.ID, &inv.Name, &inv.SiteID, &inv.Model, &inv.Manufacturer, &inv.RatedPowerKW,
		&inv.Status, &inv.TemperatureC, &inv.FirmwareVer, &inv.CommissionDate, &inv.CreatedAt, &inv.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get inverter %s: %w", id, err)
	}
	return &inv, nil
}

func (s *Store) ListInverters(siteID string) ([]model.Inverter, error) {
	var rows *sql.Rows
	var err error
	if siteID != "" {
		rows, err = s.db.Query(
			`SELECT id, name, site_id, model, manufacturer, rated_power_kw, status, temperature_c, firmware_ver, commission_date, created_at, updated_at
			 FROM inverters WHERE site_id = ? ORDER BY name`, siteID)
	} else {
		rows, err = s.db.Query(
			`SELECT id, name, site_id, model, manufacturer, rated_power_kw, status, temperature_c, firmware_ver, commission_date, created_at, updated_at
			 FROM inverters ORDER BY name`)
	}
	if err != nil {
		return nil, fmt.Errorf("list inverters: %w", err)
	}
	defer rows.Close()
	var inverters []model.Inverter
	for rows.Next() {
		var inv model.Inverter
		if err := rows.Scan(&inv.ID, &inv.Name, &inv.SiteID, &inv.Model, &inv.Manufacturer, &inv.RatedPowerKW,
			&inv.Status, &inv.TemperatureC, &inv.FirmwareVer, &inv.CommissionDate, &inv.CreatedAt, &inv.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan inverter: %w", err)
		}
		inverters = append(inverters, inv)
	}
	return inverters, nil
}

func (s *Store) UpdateInverter(inv *model.Inverter) error {
	_, err := s.db.Exec(
		`UPDATE inverters SET name=?, site_id=?, model=?, manufacturer=?, rated_power_kw=?, status=?, temperature_c=?, firmware_ver=?, updated_at=?
		 WHERE id = ?`,
		inv.Name, inv.SiteID, inv.Model, inv.Manufacturer, inv.RatedPowerKW,
		inv.Status, inv.TemperatureC, inv.FirmwareVer, inv.UpdatedAt, inv.ID)
	if err != nil {
		return fmt.Errorf("update inverter: %w", err)
	}
	return nil
}

// ---------- Generation Record ----------

func (s *Store) InsertGenerationRecord(rec *model.GenerationRecord) error {
	_, err := s.db.Exec(
		`INSERT INTO generation_records (id, inverter_id, site_id, timestamp, power_kw, energy_kwh, voltage, current, temperature_c, irradiance, fault_code, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.InverterID, rec.SiteID, rec.Timestamp, rec.PowerKW, rec.EnergyKWh,
		rec.Voltage, rec.Current, rec.TemperatureC, rec.Irradiance, rec.FaultCode, rec.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert generation record: %w", err)
	}
	return nil
}

func (s *Store) BatchInsertGenerationRecords(recs []model.GenerationRecord) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	stmt, err := tx.Prepare(
		`INSERT INTO generation_records (id, inverter_id, site_id, timestamp, power_kw, energy_kwh, voltage, current, temperature_c, irradiance, fault_code, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare stmt: %w", err)
	}
	defer stmt.Close()
	for _, rec := range recs {
		if _, err := stmt.Exec(rec.ID, rec.InverterID, rec.SiteID, rec.Timestamp, rec.PowerKW, rec.EnergyKWh,
			rec.Voltage, rec.Current, rec.TemperatureC, rec.Irradiance, rec.FaultCode, rec.CreatedAt); err != nil {
			tx.Rollback()
			return fmt.Errorf("exec batch insert: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) GetGenerationRecords(inverterID string, start, end time.Time) ([]model.GenerationRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, inverter_id, site_id, timestamp, power_kw, energy_kwh, voltage, current, temperature_c, irradiance, fault_code, created_at
		 FROM generation_records WHERE inverter_id = ? AND timestamp >= ? AND timestamp < ?
		 ORDER BY timestamp ASC`, inverterID, start, end)
	if err != nil {
		return nil, fmt.Errorf("get generation records: %w", err)
	}
	defer rows.Close()
	var records []model.GenerationRecord
	for rows.Next() {
		var rec model.GenerationRecord
		if err := rows.Scan(&rec.ID, &rec.InverterID, &rec.SiteID, &rec.Timestamp, &rec.PowerKW, &rec.EnergyKWh,
			&rec.Voltage, &rec.Current, &rec.TemperatureC, &rec.Irradiance, &rec.FaultCode, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan generation record: %w", err)
		}
		records = append(records, rec)
	}
	return records, nil
}

func (s *Store) GetDailyRecords(siteID string, date time.Time) ([]model.GenerationRecord, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.Add(24 * time.Hour)
	rows, err := s.db.Query(
		`SELECT id, inverter_id, site_id, timestamp, power_kw, energy_kwh, voltage, current, temperature_c, irradiance, fault_code, created_at
		 FROM generation_records WHERE site_id = ? AND timestamp >= ? AND timestamp < ?
		 ORDER BY timestamp ASC`, siteID, start, end)
	if err != nil {
		return nil, fmt.Errorf("get daily records: %w", err)
	}
	defer rows.Close()
	var records []model.GenerationRecord
	for rows.Next() {
		var rec model.GenerationRecord
		if err := rows.Scan(&rec.ID, &rec.InverterID, &rec.SiteID, &rec.Timestamp, &rec.PowerKW, &rec.EnergyKWh,
			&rec.Voltage, &rec.Current, &rec.TemperatureC, &rec.Irradiance, &rec.FaultCode, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan daily record: %w", err)
		}
		records = append(records, rec)
	}
	return records, nil
}

func (s *Store) GetLatestRecords(inverterID string, limit int) ([]model.GenerationRecord, error) {
	rows, err := s.db.Query(
		`SELECT id, inverter_id, site_id, timestamp, power_kw, energy_kwh, voltage, current, temperature_c, irradiance, fault_code, created_at
		 FROM generation_records WHERE inverter_id = ?
		 ORDER BY timestamp DESC LIMIT ?`, inverterID, limit)
	if err != nil {
		return nil, fmt.Errorf("get latest records: %w", err)
	}
	defer rows.Close()
	var records []model.GenerationRecord
	for rows.Next() {
		var rec model.GenerationRecord
		if err := rows.Scan(&rec.ID, &rec.InverterID, &rec.SiteID, &rec.Timestamp, &rec.PowerKW, &rec.EnergyKWh,
			&rec.Voltage, &rec.Current, &rec.TemperatureC, &rec.Irradiance, &rec.FaultCode, &rec.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan latest record: %w", err)
		}
		records = append(records, rec)
	}
	return records, nil
}

// ---------- Alert CRUD ----------

func (s *Store) CreateAlert(alert *model.Alert) error {
	_, err := s.db.Exec(
		`INSERT INTO alerts (id, inverter_id, site_id, level, status, code, message, value, threshold, timestamp, acknowledged_by, acknowledged_at, cleared_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		alert.ID, alert.InverterID, alert.SiteID, alert.Level, alert.Status, alert.Code, alert.Message,
		alert.Value, alert.Threshold, alert.Timestamp, alert.AcknowledgedBy, alert.AcknowledgedAt, alert.ClearedAt, alert.CreatedAt)
	if err != nil {
		return fmt.Errorf("create alert: %w", err)
	}
	return nil
}

func (s *Store) GetAlert(id string) (*model.Alert, error) {
	row := s.db.QueryRow(
		`SELECT id, inverter_id, site_id, level, status, code, message, value, threshold, timestamp, acknowledged_by, acknowledged_at, cleared_at, created_at
		 FROM alerts WHERE id = ?`, id)
	var alert model.Alert
	var ackBy sql.NullString
	var ackAt, clearedAt sql.NullTime
	err := row.Scan(&alert.ID, &alert.InverterID, &alert.SiteID, &alert.Level, &alert.Status, &alert.Code, &alert.Message,
		&alert.Value, &alert.Threshold, &alert.Timestamp, &ackBy, &ackAt, &clearedAt, &alert.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get alert %s: %w", id, err)
	}
	alert.AcknowledgedBy = ackBy.String
	if ackAt.Valid {
		alert.AcknowledgedAt = &ackAt.Time
	}
	if clearedAt.Valid {
		alert.ClearedAt = &clearedAt.Time
	}
	return &alert, nil
}

func (s *Store) ListAlerts(status string, inverterID string) ([]model.Alert, error) {
	q := `SELECT id, inverter_id, site_id, level, status, code, message, value, threshold, timestamp, acknowledged_by, acknowledged_at, cleared_at, created_at FROM alerts WHERE 1=1`
	var args []interface{}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	if inverterID != "" {
		q += ` AND inverter_id = ?`
		args = append(args, inverterID)
	}
	q += ` ORDER BY timestamp DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	defer rows.Close()
	var alerts []model.Alert
	for rows.Next() {
		var alert model.Alert
		var ackBy sql.NullString
		var ackAt, clearedAt sql.NullTime
		if err := rows.Scan(&alert.ID, &alert.InverterID, &alert.SiteID, &alert.Level, &alert.Status, &alert.Code, &alert.Message,
			&alert.Value, &alert.Threshold, &alert.Timestamp, &ackBy, &ackAt, &clearedAt, &alert.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}
		alert.AcknowledgedBy = ackBy.String
		if ackAt.Valid {
			alert.AcknowledgedAt = &ackAt.Time
		}
		if clearedAt.Valid {
			alert.ClearedAt = &clearedAt.Time
		}
		alerts = append(alerts, alert)
	}
	return alerts, nil
}

func (s *Store) AcknowledgeAlert(id string, ackBy string, ackAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE alerts SET status = 'acknowledged', acknowledged_by = ?, acknowledged_at = ? WHERE id = ? AND status = 'active'`,
		ackBy, ackAt, id)
	if err != nil {
		return fmt.Errorf("acknowledge alert: %w", err)
	}
	return nil
}

func (s *Store) ClearAlert(id string, clearedAt time.Time) error {
	_, err := s.db.Exec(
		`UPDATE alerts SET status = 'cleared', cleared_at = ? WHERE id = ? AND status IN ('active', 'acknowledged')`,
		clearedAt, id)
	if err != nil {
		return fmt.Errorf("clear alert: %w", err)
	}
	return nil
}

func (s *Store) CountActiveAlerts(inverterID string) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM alerts WHERE inverter_id = ? AND status = 'active'`, inverterID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active alerts: %w", err)
	}
	return count, nil
}

func (s *Store) CountFaults(inverterID string, since time.Time) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM generation_records WHERE inverter_id = ? AND timestamp >= ? AND fault_code != ''`,
		inverterID, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count faults: %w", err)
	}
	return count, nil
}

// ---------- Maintenance Task ----------

func (s *Store) CreateMaintenanceTask(task *model.MaintenanceTask) error {
	_, err := s.db.Exec(
		`INSERT INTO maintenance_tasks (id, inverter_id, site_id, type, description, status, priority, scheduled_for, assigned_to, completed_at, notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.InverterID, task.SiteID, task.Type, task.Description, task.Status, task.Priority,
		task.ScheduledFor, task.AssignedTo, task.CompletedAt, task.Notes, task.CreatedAt, task.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create maintenance task: %w", err)
	}
	return nil
}

func (s *Store) GetMaintenanceTask(id string) (*model.MaintenanceTask, error) {
	row := s.db.QueryRow(
		`SELECT id, inverter_id, site_id, type, description, status, priority, scheduled_for, assigned_to, completed_at, notes, created_at, updated_at
		 FROM maintenance_tasks WHERE id = ?`, id)
	var task model.MaintenanceTask
	var completedAt sql.NullTime
	err := row.Scan(&task.ID, &task.InverterID, &task.SiteID, &task.Type, &task.Description, &task.Status, &task.Priority,
		&task.ScheduledFor, &task.AssignedTo, &completedAt, &task.Notes, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get maintenance task %s: %w", id, err)
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}
	return &task, nil
}

func (s *Store) ListMaintenanceTasks(status string, inverterID string) ([]model.MaintenanceTask, error) {
	q := `SELECT id, inverter_id, site_id, type, description, status, priority, scheduled_for, assigned_to, completed_at, notes, created_at, updated_at FROM maintenance_tasks WHERE 1=1`
	var args []interface{}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	if inverterID != "" {
		q += ` AND inverter_id = ?`
		args = append(args, inverterID)
	}
	q += ` ORDER BY priority ASC, scheduled_for ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list maintenance tasks: %w", err)
	}
	defer rows.Close()
	var tasks []model.MaintenanceTask
	for rows.Next() {
		var task model.MaintenanceTask
		var completedAt sql.NullTime
		if err := rows.Scan(&task.ID, &task.InverterID, &task.SiteID, &task.Type, &task.Description, &task.Status, &task.Priority,
			&task.ScheduledFor, &task.AssignedTo, &completedAt, &task.Notes, &task.CreatedAt, &task.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan maintenance task: %w", err)
		}
		if completedAt.Valid {
			task.CompletedAt = &completedAt.Time
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func (s *Store) UpdateMaintenanceTask(task *model.MaintenanceTask) error {
	_, err := s.db.Exec(
		`UPDATE maintenance_tasks SET status=?, priority=?, scheduled_for=?, assigned_to=?, completed_at=?, notes=?, updated_at=?
		 WHERE id = ?`,
		task.Status, task.Priority, task.ScheduledFor, task.AssignedTo, task.CompletedAt, task.Notes, task.UpdatedAt, task.ID)
	if err != nil {
		return fmt.Errorf("update maintenance task: %w", err)
	}
	return nil
}

func (s *Store) GetLastMaintenance(inverterID string) (*time.Time, error) {
	var t time.Time
	err := s.db.QueryRow(
		`SELECT MAX(completed_at) FROM maintenance_tasks WHERE inverter_id = ? AND status = 'completed'`, inverterID).Scan(&t)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get last maintenance: %w", err)
	}
	if t.IsZero() {
		return nil, nil
	}
	return &t, nil
}

// ---------- Cleaning Schedule ----------

func (s *Store) CreateCleaningSchedule(cs *model.CleaningSchedule) error {
	_, err := s.db.Exec(
		`INSERT INTO cleaning_schedules (id, site_id, inverter_id, status, dust_grade, last_rainfall_mm, days_since_clean, efficiency_drop_pct, scheduled_for, completed_at, notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cs.ID, cs.SiteID, cs.InverterID, cs.Status, cs.DustGrade, cs.LastRainfall, cs.DaysSinceClean,
		cs.EfficiencyDrop, cs.ScheduledFor, cs.CompletedAt, cs.Notes, cs.CreatedAt, cs.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create cleaning schedule: %w", err)
	}
	return nil
}

func (s *Store) GetCleaningSchedule(id string) (*model.CleaningSchedule, error) {
	row := s.db.QueryRow(
		`SELECT id, site_id, inverter_id, status, dust_grade, last_rainfall_mm, days_since_clean, efficiency_drop_pct, scheduled_for, completed_at, notes, created_at, updated_at
		 FROM cleaning_schedules WHERE id = ?`, id)
	var cs model.CleaningSchedule
	var completedAt sql.NullTime
	err := row.Scan(&cs.ID, &cs.SiteID, &cs.InverterID, &cs.Status, &cs.DustGrade, &cs.LastRainfall, &cs.DaysSinceClean,
		&cs.EfficiencyDrop, &cs.ScheduledFor, &completedAt, &cs.Notes, &cs.CreatedAt, &cs.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get cleaning schedule %s: %w", id, err)
	}
	if completedAt.Valid {
		cs.CompletedAt = &completedAt.Time
	}
	return &cs, nil
}

func (s *Store) ListCleaningSchedules(status string, siteID string) ([]model.CleaningSchedule, error) {
	q := `SELECT id, site_id, inverter_id, status, dust_grade, last_rainfall_mm, days_since_clean, efficiency_drop_pct, scheduled_for, completed_at, notes, created_at, updated_at FROM cleaning_schedules WHERE 1=1`
	var args []interface{}
	if status != "" {
		q += ` AND status = ?`
		args = append(args, status)
	}
	if siteID != "" {
		q += ` AND site_id = ?`
		args = append(args, siteID)
	}
	q += ` ORDER BY scheduled_for ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list cleaning schedules: %w", err)
	}
	defer rows.Close()
	var schedules []model.CleaningSchedule
	for rows.Next() {
		var cs model.CleaningSchedule
		var completedAt sql.NullTime
		if err := rows.Scan(&cs.ID, &cs.SiteID, &cs.InverterID, &cs.Status, &cs.DustGrade, &cs.LastRainfall, &cs.DaysSinceClean,
			&cs.EfficiencyDrop, &cs.ScheduledFor, &completedAt, &cs.Notes, &cs.CreatedAt, &cs.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan cleaning schedule: %w", err)
		}
		if completedAt.Valid {
			cs.CompletedAt = &completedAt.Time
		}
		schedules = append(schedules, cs)
	}
	return schedules, nil
}

func (s *Store) UpdateCleaningSchedule(cs *model.CleaningSchedule) error {
	_, err := s.db.Exec(
		`UPDATE cleaning_schedules SET status=?, dust_grade=?, last_rainfall_mm=?, days_since_clean=?, efficiency_drop_pct=?, scheduled_for=?, completed_at=?, notes=?, updated_at=?
		 WHERE id = ?`,
		cs.Status, cs.DustGrade, cs.LastRainfall, cs.DaysSinceClean, cs.EfficiencyDrop,
		cs.ScheduledFor, cs.CompletedAt, cs.Notes, cs.UpdatedAt, cs.ID)
	if err != nil {
		return fmt.Errorf("update cleaning schedule: %w", err)
	}
	return nil
}

// ---------- Weather Data ----------

func (s *Store) InsertWeatherData(w *model.WeatherData) error {
	_, err := s.db.Exec(
		`INSERT INTO weather_data (id, site_id, timestamp, temperature_c, humidity, irradiance, wind_speed, rainfall_mm, cloud_cover_pct, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.SiteID, w.Timestamp, w.TemperatureC, w.Humidity, w.Irradiance, w.WindSpeed, w.Rainfall, w.CloudCover, w.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert weather data: %w", err)
	}
	return nil
}

func (s *Store) GetWeatherData(siteID string, start, end time.Time) ([]model.WeatherData, error) {
	rows, err := s.db.Query(
		`SELECT id, site_id, timestamp, temperature_c, humidity, irradiance, wind_speed, rainfall_mm, cloud_cover_pct, created_at
		 FROM weather_data WHERE site_id = ? AND timestamp >= ? AND timestamp < ?
		 ORDER BY timestamp ASC`, siteID, start, end)
	if err != nil {
		return nil, fmt.Errorf("get weather data: %w", err)
	}
	defer rows.Close()
	var data []model.WeatherData
	for rows.Next() {
		var w model.WeatherData
		if err := rows.Scan(&w.ID, &w.SiteID, &w.Timestamp, &w.TemperatureC, &w.Humidity, &w.Irradiance, &w.WindSpeed, &w.Rainfall, &w.CloudCover, &w.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan weather data: %w", err)
		}
		data = append(data, w)
	}
	return data, nil
}

func (s *Store) GetLatestWeather(siteID string) (*model.WeatherData, error) {
	row := s.db.QueryRow(
		`SELECT id, site_id, timestamp, temperature_c, humidity, irradiance, wind_speed, rainfall_mm, cloud_cover_pct, created_at
		 FROM weather_data WHERE site_id = ? ORDER BY timestamp DESC LIMIT 1`, siteID)
	var w model.WeatherData
	err := row.Scan(&w.ID, &w.SiteID, &w.Timestamp, &w.TemperatureC, &w.Humidity, &w.Irradiance, &w.WindSpeed, &w.Rainfall, &w.CloudCover, &w.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get latest weather: %w", err)
	}
	return &w, nil
}

// ---------- Daily Summary ----------

func (s *Store) SaveDailySummary(sum *model.DailyGenerationSummary) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO daily_summaries (id, site_id, inverter_id, date, total_energy_kwh, peak_power_kw, avg_power_kw, expected_energy_kwh, performance_ratio, downtime_hours, record_count, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sum.ID, sum.SiteID, sum.InverterID, sum.Date, sum.TotalEnergy, sum.PeakPower, sum.AvgPower,
		sum.ExpectedEnergy, sum.PerformanceRatio, sum.Downtime, sum.RecordCount, sum.CreatedAt)
	if err != nil {
		return fmt.Errorf("save daily summary: %w", err)
	}
	return nil
}

func (s *Store) GetDailySummaries(siteID string, start, end time.Time) ([]model.DailyGenerationSummary, error) {
	rows, err := s.db.Query(
		`SELECT id, site_id, inverter_id, date, total_energy_kwh, peak_power_kw, avg_power_kw, expected_energy_kwh, performance_ratio, downtime_hours, record_count, created_at
		 FROM daily_summaries WHERE site_id = ? AND date >= ? AND date < ?
		 ORDER BY date ASC`, siteID, start, end)
	if err != nil {
		return nil, fmt.Errorf("get daily summaries: %w", err)
	}
	defer rows.Close()
	var summaries []model.DailyGenerationSummary
	for rows.Next() {
		var sum model.DailyGenerationSummary
		if err := rows.Scan(&sum.ID, &sum.SiteID, &sum.InverterID, &sum.Date, &sum.TotalEnergy, &sum.PeakPower, &sum.AvgPower,
			&sum.ExpectedEnergy, &sum.PerformanceRatio, &sum.Downtime, &sum.RecordCount, &sum.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan daily summary: %w", err)
		}
		summaries = append(summaries, sum)
	}
	return summaries, nil
}

// ---------- Aggregate Queries ----------

func (s *Store) GetSiteGenerationStats(siteID string, start, end time.Time) (*model.GenerationStats, error) {
	stats := &model.GenerationStats{SiteID: siteID}
	err := s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(energy_kwh), 0), COALESCE(MAX(power_kw), 0), COALESCE(AVG(power_kw), 0)
		 FROM generation_records WHERE site_id = ? AND timestamp >= ? AND timestamp < ?`,
		siteID, start, end).Scan(&stats.RecordCount, &stats.TotalEnergy, &stats.PeakPower, &stats.AvgPower)
	if err != nil {
		return nil, fmt.Errorf("get site generation stats: %w", err)
	}
	// 计算平均日发电
	days := end.Sub(start).Hours() / 24
	if days > 0 {
		stats.AvgDailyEnergy = stats.TotalEnergy / days
	}
	// 故障计数
	err = s.db.QueryRow(
		`SELECT COUNT(*) FROM generation_records WHERE site_id = ? AND timestamp >= ? AND timestamp < ? AND fault_code != ''`,
		siteID, start, end).Scan(&stats.FaultCount)
	if err != nil {
		return nil, fmt.Errorf("get fault count: %w", err)
	}
	return stats, nil
}

func (s *Store) GetInverterDailyEnergy(inverterID string, date time.Time) (float64, error) {
	start := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	end := start.Add(24 * time.Hour)
	var total float64
	err := s.db.QueryRow(
		`SELECT COALESCE(SUM(energy_kwh), 0) FROM generation_records WHERE inverter_id = ? AND timestamp >= ? AND timestamp < ?`,
		inverterID, start, end).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("get inverter daily energy: %w", err)
	}
	return total, nil
}

func (s *Store) GetInverterRecordsCount(inverterID string, start, end time.Time) (int, error) {
	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM generation_records WHERE inverter_id = ? AND timestamp >= ? AND timestamp < ?`,
		inverterID, start, end).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get records count: %w", err)
	}
	return count, nil
}

// DeleteInverterData 删除逆变器相关数据（测试用）
func (s *Store) DeleteInverterData(inverterID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	for _, table := range []string{"generation_records", "alerts", "maintenance_tasks", "cleaning_schedules"} {
		if _, err := tx.Exec(fmt.Sprintf("DELETE FROM %s WHERE inverter_id = ?", table), inverterID); err != nil {
			tx.Rollback()
			return fmt.Errorf("delete from %s: %w", table, err)
		}
	}
	return tx.Commit()
}
