package service

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"solar-ops/internal/model"
	"solar-ops/internal/store"
)

func newTestService3(t *testing.T) *MonitoringService {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return NewMonitoringService(s)
}

func TestBug003_ConcurrentIngest_DataRace(t *testing.T) {
	svc := newTestService3(t)

	now := time.Now()
	site := &model.Site{
		ID:             "site-race",
		Name:           "RaceSite",
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
		ID:             "inv-race-001",
		Name:           "RaceTest",
		SiteID:         "site-race",
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

	// Seed one record so GetLatestReading hits cache path
	seedRec := &model.GenerationRecord{
		InverterID:   inv.ID,
		SiteID:       inv.SiteID,
		Timestamp:    now,
		PowerKW:      1.0,
		EnergyKWh:    0.5,
		Voltage:      600,
		Current:      10,
		TemperatureC: 40,
		Irradiance:   800,
		CreatedAt:    now,
	}
	if err := svc.IngestGenerationRecord(seedRec); err != nil {
		t.Fatalf("seed ingest: %v", err)
	}

	// Writers: 50 goroutines each calling IngestGenerationRecord (RLock + map write)
	// Readers: 50 goroutines each calling GetLatestReading in a tight loop (RLock + map read)
	// Under bug_base_3: both use RLock -> concurrent map read+write -> race detector fires
	// Under gold_model_fix_3: writer uses Lock -> no race
	const writers = 50
	const readers = 50
	const iterations = 50

	var startBarrier sync.WaitGroup
	startBarrier.Add(1)

	var wg sync.WaitGroup

	// writers
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startBarrier.Wait()
			for j := 0; j < iterations; j++ {
				rec := &model.GenerationRecord{
					InverterID:   inv.ID,
					SiteID:       inv.SiteID,
					Timestamp:    now.Add(time.Duration(idx*iterations+j) * time.Millisecond),
					PowerKW:      float64(idx*j + j),
					EnergyKWh:    float64(idx*j+j) * 0.5,
					Voltage:      600,
					Current:      10,
					TemperatureC: 40,
					Irradiance:   800,
					CreatedAt:    now,
				}
				_ = svc.IngestGenerationRecord(rec)
			}
		}(i)
	}

	// readers
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startBarrier.Wait()
			for j := 0; j < iterations; j++ {
				_, _ = svc.GetLatestReading(inv.ID)
			}
		}(i)
	}

	startBarrier.Done()
	wg.Wait()
}