package service

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"solar-ops/internal/model"
	"solar-ops/internal/store"
)

func newTestService4(t *testing.T) *MonitoringService {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewMonitoringService(s)
}

func TestBug004_CompleteMaintenanceTask_ErrorWrapping(t *testing.T) {
	svc := newTestService4(t)

	now := time.Now()
	site := &model.Site{
		ID:             "site-err",
		Name:           "ErrSite",
		Location:       "TestLoc",
		Latitude:       30.0,
		Longitude:      120.0,
		CapacityKW:     100.0,
		Timezone:       "Asia/Shanghai",
		CommissionDate: now,
		Active:         true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := svc.store.CreateSite(site); err != nil {
		t.Fatalf("create site: %v", err)
	}

	inv := &model.Inverter{
		ID:             "inv-err-001",
		Name:           "ErrorTest",
		SiteID:         "site-err",
		Model:          "SUN-10K",
		Manufacturer:   "TestMfr",
		RatedPowerKW:   10.0,
		Status:         model.StatusOnline,
		TemperatureC:   35.0,
		CommissionDate: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := svc.store.CreateInverter(inv); err != nil {
		t.Fatalf("create inverter: %v", err)
	}

	// Complete a nonexistent maintenance task -> should return ErrMaintenanceNotFound
	// bug: CompleteMaintenanceTask wraps with %v -> errors.Is(err, ErrMaintenanceNotFound) returns false
	err := svc.CompleteMaintenanceTask("nonexistent-task-id", "test notes")

	if err == nil {
		t.Fatal("expected error for nonexistent maintenance task")
	}

	// After fix, errors.Is should match ErrMaintenanceNotFound
	if !errors.Is(err, ErrMaintenanceNotFound) {
		t.Fatalf("expected errors.Is(err, ErrMaintenanceNotFound) to be true, got: %v", err)
	}
}