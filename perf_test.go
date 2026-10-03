package quotaledger

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// 10 万条请求、每条覆盖相同 12 个月、32 个并发调用方：
// 5000 次计费修订与 5000 次额度查询混合执行应在 5 秒内完成（不含建账）。
func TestMixedLoadPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}
	const (
		numUsers    = 1000
		numRequests = 100000
		duration    = 12
		revs        = 5000
		queries     = 5000
		callers     = 32
	)
	base := MonthOf(2026, 1)
	big := int64(1) << 55

	cfg := Config{
		UserDept:     map[string]string{},
		DeptTenant:   map[string]string{},
		UserLimits:   map[string]Limits{},
		DeptLimits:   map[string]Limits{},
		TenantLimits: map[string]Limits{},
	}
	lim := Limits{MaxUnits: big, MaxParallel: big, MaxStorageBytes: big, MaxMonthlyBudget: big}
	for d := 0; d < 20; d++ {
		dept := fmt.Sprintf("d%d", d)
		cfg.DeptTenant[dept] = "t0"
		cfg.DeptLimits[dept] = lim
	}
	cfg.TenantLimits["t0"] = lim
	users := make([]string, numUsers)
	for i := range users {
		users[i] = fmt.Sprintf("u%d", i)
		cfg.UserDept[users[i]] = fmt.Sprintf("d%d", i%20)
		cfg.UserLimits[users[i]] = lim
	}
	l := New(cfg)

	for i := 0; i < numRequests; i++ {
		if err := l.Approve(ApproveRequest{
			ID: fmt.Sprintf("r%d", i), User: users[i%numUsers],
			Units: 1, StoragePerUnit: 64, MonthlyFeePerUnit: 100,
			StartMonth: base, DurationMonths: duration,
		}); err != nil {
			t.Fatal(err)
		}
	}

	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < callers; w++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			total := revs + queries
			for i := 0; i < total/callers; i++ {
				if i%2 == 0 {
					act := map[Month]int64{}
					for j := 0; j < duration; j++ {
						act[base+Month(j)] = int64(rng.Intn(500))
					}
					_ = l.Revise(Revision{
						RequestID: fmt.Sprintf("r%d", rng.Intn(numRequests)),
						Number:    uint64(rng.Intn(1 << 40)),
						Actuals:   act,
					})
				} else {
					if _, err := l.Query(users[rng.Intn(numUsers)], base+Month(rng.Intn(duration))); err != nil {
						t.Error(err)
						return
					}
				}
			}
		}(int64(w) + 1)
	}
	wg.Wait()
	elapsed := time.Since(start)
	t.Logf("mixed load: %d revisions + %d queries across %d callers took %v", revs, queries, callers, elapsed)
	if elapsed > 5*time.Second {
		t.Fatalf("mixed load took %v, exceeds 5s", elapsed)
	}
}
