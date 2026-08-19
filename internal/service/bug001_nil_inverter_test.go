package service

import (
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"solar-ops/internal/model"
	"solar-ops/internal/store"
)

// newTestService 创建测试用 service（SQLite 文件模式，t.TempDir 隔离）
func newTestService(t *testing.T) *MonitoringService {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewMonitoringService(s)
}

// seedInverter 写入测试逆变器
func seedInverter(t *testing.T, svc *MonitoringService, id, siteID string) *model.Inverter {
	t.Helper()
	now := time.Now()
	inv := &model.Inverter{
		ID:           id,
		Name:         "Test-" + id,
		SiteID:       siteID,
		Model:        "SUN-10K",
		Manufacturer: "TestMfr",
		RatedPowerKW: 10.0,
		Status:       model.StatusOnline,
		TemperatureC: 35.0,
		FirmwareVer:  "1.0.0",
		CommissionDate: now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := svc.store.CreateInverter(inv); err != nil {
		t.Fatalf("seed inverter: %v", err)
	}
	return inv
}

func TestBug001_GetEfficiencyReport_NilInverter(t *testing.T) {
	svc := newTestService(t)

	// 查询不存在的逆变器 ID
	// bug: GetInverterByID 返回 nil,nil → GetEfficiencyReport 不检查 nil → panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()

	report, err := svc.GetEfficiencyReport("nonexistent-inv", time.Now())
	// 修复后应该返回错误而非 panic
	if err == nil && report != nil {
		t.Fatal("expected error for nonexistent inverter, got nil error and non-nil report")
	}
}