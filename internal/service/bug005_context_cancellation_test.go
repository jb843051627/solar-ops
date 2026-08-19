package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"solar-ops/internal/model"
	"solar-ops/internal/store"
)

func newTestService5(t *testing.T) *MonitoringService {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewMonitoringService(s)
}

func TestBug005_CheckInverterTimeouts_ContextCancellation(t *testing.T) {
	svc := newTestService5(t)

	now := time.Now()
	site := &model.Site{
		ID:             "site-ctx",
		Name:           "ContextSite",
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

	// Create 3 online inverters with stale generation records (>5 min ago)
	staleTime := now.Add(-10 * time.Minute)
	for i := 0; i < 3; i++ {
		inv := &model.Inverter{
			ID:             string(rune('A'+i)) + "inv-ctx-00" + string(rune('1'+i)),
			Name:           "ContextTest" + string(rune('A'+i)),
			SiteID:         "site-ctx",
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
			t.Fatalf("create inverter %d: %v", i, err)
		}
		rec := &model.GenerationRecord{
			ID:           "gen-ctx-" + string(rune('A'+i)),
			InverterID:   inv.ID,
			SiteID:       inv.SiteID,
			Timestamp:    staleTime,
			PowerKW:      5.0,
			EnergyKWh:    2.5,
			Voltage:      600,
			Current:      8.3,
			TemperatureC: 40,
			Irradiance:   800,
			CreatedAt:    staleTime,
		}
		if err := svc.store.InsertGenerationRecord(rec); err != nil {
			t.Fatalf("insert gen record %d: %v", i, err)
		}
	}

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Call checkInverterTimeouts with cancelled ctx
	// bug: checkInverterTimeouts ignores ctx.Done() -> marks all inverters offline
	// fix: checkInverterTimeouts checks ctx.Done() -> returns early, no inverters marked offline
	svc.checkInverterTimeouts(ctx)

	// Verify: with fix, no inverter should be marked offline (ctx was cancelled)
	inverters, err := svc.store.ListInverters("")
	if err != nil {
		t.Fatalf("list inverters: %v", err)
	}

	for _, inv := range inverters {
		if inv.Status == model.StatusOffline {
			t.Fatalf("inverter %s was marked offline despite cancelled context (ctx propagation broken)", inv.ID)
		}
	}
}