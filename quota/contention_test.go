package quota

import (
	"fmt"
	"sync"
	"testing"
)

// Different departments race for the tenant's last parallel slots: the
// winners must not overcommit and the losers must leave no residue.
func TestConcurrentTenantContention(t *testing.T) {
	const (
		depts        = 4
		usersPerDept = 8 // 32 concurrent callers
		slots        = 3 // tenant-wide remaining parallel slots
	)
	cfg := Config{UserDept: map[string]string{}, DeptTenant: map[string]string{},
		UserLimits: map[string]Limits{}, DeptLimits: map[string]Limits{},
		TenantLim: map[string]Limits{"t": {MaxUnits: 1 << 40, MaxParallel: slots, MaxStorageBytes: 1 << 40, MonthlyBudget: 1 << 40}}}
	roomy := Limits{MaxUnits: 1 << 40, MaxParallel: 1 << 40, MaxStorageBytes: 1 << 40, MonthlyBudget: 1 << 40}
	for d := 0; d < depts; d++ {
		dept := fmt.Sprintf("d%d", d)
		cfg.DeptTenant[dept] = "t"
		cfg.DeptLimits[dept] = roomy
		for u := 0; u < usersPerDept; u++ {
			user := fmt.Sprintf("%s-u%d", dept, u)
			cfg.UserDept[user] = dept
			cfg.UserLimits[user] = roomy
		}
	}
	l := NewLedger(cfg)
	m := Month{Year: 2026, Month: 1}

	results := make([]bool, depts*usersPerDept)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			user := fmt.Sprintf("d%d-u%d", i/usersPerDept, i%usersPerDept)
			results[i] = l.Approve(Request{
				ID: fmt.Sprintf("req-%d", i), UserID: user, Units: 1,
				MonthlyFee: 100, StartMonth: m, DurationMonths: 12,
			})
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for _, ok := range results {
		if ok {
			succeeded++
		}
	}
	if succeeded != slots {
		t.Fatalf("approved %d requests, want exactly %d", succeeded, slots)
	}

	// The tenant's usage must equal the winners' occupancy, nothing more.
	q := l.Query("d0-u0", m)
	if q.Tenant.Parallel != slots || q.Tenant.Units != slots {
		t.Fatalf("tenant usage = %+v, want %d units / %d parallel", q.Tenant, slots, slots)
	}
	if got := q.TenantMon.Committed; got != int64(slots)*100 {
		t.Fatalf("tenant committed = %d, want %d", got, slots*100)
	}

	// Losers left no residue: per-user and per-dept usage matches the set
	// of winning request IDs exactly.
	want := map[string]int64{}
	for i, ok := range results {
		if ok {
			user := fmt.Sprintf("d%d-u%d", i/usersPerDept, i%usersPerDept)
			want[user]++
		}
	}
	for d := 0; d < depts; d++ {
		var deptTotal int64
		for u := 0; u < usersPerDept; u++ {
			user := fmt.Sprintf("d%d-u%d", d, u)
			if got := l.Query(user, m).User.Parallel; got != want[user] {
				t.Fatalf("user %s parallel = %d, want %d", user, got, want[user])
			}
			deptTotal += want[user]
		}
		if got := l.Query(fmt.Sprintf("d%d-u0", d), m).Dept.Parallel; got != deptTotal {
			t.Fatalf("dept d%d parallel = %d, want %d", d, got, deptTotal)
		}
	}
}
