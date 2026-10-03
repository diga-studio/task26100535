package quota

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// Acceptance scale: 100k approved requests each covering the same 12
// months, 32 concurrent callers running 5000 revisions mixed with 5000
// queries; the mixed phase must finish within 5 seconds.
func TestPerfMixedRevisionsAndQueries(t *testing.T) {
	const (
		numRequests = 100_000
		numUsers    = 1000
		months      = 12
		callers     = 32
		revisions   = 5000
		queries     = 5000
	)
	cfg := Config{UserDept: map[string]string{}, DeptTenant: map[string]string{},
		UserLimits: map[string]Limits{}, DeptLimits: map[string]Limits{}, TenantLim: map[string]Limits{}}
	roomy := Limits{MaxUnits: 1 << 50, MaxParallel: 1 << 50, MaxStorageBytes: 1 << 50, MonthlyBudget: 1 << 60}
	cfg.TenantLim["t"] = roomy
	for d := 0; d < 10; d++ {
		did := fmt.Sprintf("d%d", d)
		cfg.DeptTenant[did] = "t"
		cfg.DeptLimits[did] = roomy
	}
	for u := 0; u < numUsers; u++ {
		uid := fmt.Sprintf("u%d", u)
		cfg.UserDept[uid] = fmt.Sprintf("d%d", u%10)
		cfg.UserLimits[uid] = roomy
	}

	l := NewLedger(cfg)
	base := Month{Year: 2026, Month: 1}
	for i := 0; i < numRequests; i++ {
		if !l.Approve(Request{
			ID: fmt.Sprintf("r%d", i), UserID: fmt.Sprintf("u%d", i%numUsers),
			Units: 1, StoragePerUnit: 1, MonthlyFee: 100,
			StartMonth: base, DurationMonths: months,
		}) {
			t.Fatalf("setup approve %d failed", i)
		}
	}

	start := time.Now()
	var wg sync.WaitGroup
	for c := 0; c < callers; c++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for i := 0; i < (revisions+queries)/callers; i++ {
				if i%2 == 0 {
					actuals := make(map[Month]int64, months)
					for m := 0; m < months; m++ {
						actuals[base.Add(m)] = rng.Int63n(200)
					}
					l.Revise(Revision{
						RequestID: fmt.Sprintf("r%d", rng.Intn(numRequests)),
						Revision:  uint64(1 + rng.Intn(1000)),
						Actuals:   actuals,
					})
				} else {
					_ = l.Query(fmt.Sprintf("u%d", rng.Intn(numUsers)), base.Add(rng.Intn(months)))
				}
			}
		}(int64(c + 1))
	}
	wg.Wait()
	elapsed := time.Since(start)
	t.Logf("mixed phase: %v", elapsed)
	if elapsed > 5*time.Second {
		t.Fatalf("mixed phase took %v, want < 5s", elapsed)
	}
}
