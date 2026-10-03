package quota

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
)

// finalState is the test's own record of what the ledger should contain,
// rebuilt from scratch using only the final per-request state.
type finalState struct {
	closed   bool
	revision uint64
	actuals  map[Month]int64
	req      Request
	approved bool
}

// TestMixedOperationsConsistency hammers the ledger with interleaved
// approves, revisions, closes and queries, then verifies every level's
// usage against an independent recomputation from the final request states.
func TestMixedOperationsConsistency(t *testing.T) {
	const (
		tenants     = 2
		depts       = 4
		users       = 8
		requests    = 200
		ops         = 4000
		concurrency = 16
	)
	cfg := Config{UserDept: map[string]string{}, DeptTenant: map[string]string{},
		UserLimits: map[string]Limits{}, DeptLimits: map[string]Limits{}, TenantLim: map[string]Limits{}}
	roomy := Limits{MaxUnits: 1 << 40, MaxParallel: 1 << 40, MaxStorageBytes: 1 << 40, MonthlyBudget: 1 << 50}
	for t := 0; t < tenants; t++ {
		tid := fmt.Sprintf("t%d", t)
		cfg.TenantLim[tid] = roomy
		for d := 0; d < depts; d++ {
			did := fmt.Sprintf("%s-d%d", tid, d)
			cfg.DeptTenant[did] = tid
			cfg.DeptLimits[did] = roomy
			for u := 0; u < users; u++ {
				uid := fmt.Sprintf("%s-u%d", did, u)
				cfg.UserDept[uid] = did
				cfg.UserLimits[uid] = roomy
			}
		}
	}
	userIDs := make([]string, 0, len(cfg.UserDept))
	for u := range cfg.UserDept {
		userIDs = append(userIDs, u)
	}

	l := NewLedger(cfg)
	base := Month{Year: 2025, Month: 6}

	// reqMeta[i] is the request the i-th ID attempts to create.
	reqMeta := make([]Request, requests)
	for i := range reqMeta {
		rng := rand.New(rand.NewSource(int64(i)))
		reqMeta[i] = Request{
			ID:             fmt.Sprintf("r%d", i),
			UserID:         userIDs[rng.Intn(len(userIDs))],
			Units:          1 + rng.Int63n(4),
			StoragePerUnit: 1 + rng.Int63n(16),
			MonthlyFee:     1 + rng.Int63n(5000),
			StartMonth:     base.Add(rng.Intn(6)),
			DurationMonths: 1 + rng.Intn(12),
		}
	}

	// state[i] is the goroutine-safely maintained model of request i.
	state := make([]finalState, requests)
	var mu sync.Mutex

	worker := func(seed int64, n int, wg *sync.WaitGroup) {
		defer wg.Done()
		rng := rand.New(rand.NewSource(seed))
		for op := 0; op < n; op++ {
			i := rng.Intn(requests)
			switch rng.Intn(5) {
			case 0, 1: // approve
				if l.Approve(reqMeta[i]) {
					mu.Lock()
					if !state[i].approved {
						state[i].approved = true
						state[i].req = reqMeta[i]
						state[i].actuals = map[Month]int64{}
					}
					mu.Unlock()
				}
			case 2, 3: // revise
				mu.Lock()
				approved := state[i].approved
				rev := state[i].revision
				mu.Unlock()
				if !approved {
					continue
				}
				rev += 1 + uint64(rng.Intn(3))
				actuals := map[Month]int64{}
				for m := 0; m < reqMeta[i].DurationMonths; m++ {
					if rng.Intn(3) > 0 { // some months absent => zero
						actuals[reqMeta[i].StartMonth.Add(m)] = rng.Int63n(20000)
					}
				}
				l.Revise(Revision{RequestID: reqMeta[i].ID, Revision: rev, Actuals: actuals})
				mu.Lock()
				if state[i].approved && rev > state[i].revision {
					state[i].revision = rev
					state[i].actuals = actuals
				}
				mu.Unlock()
			default: // close or query
				if rng.Intn(2) == 0 {
					l.Close(reqMeta[i].ID)
					mu.Lock()
					if state[i].approved {
						state[i].closed = true
					}
					mu.Unlock()
				} else {
					_ = l.Query(userIDs[rng.Intn(len(userIDs))], base.Add(rng.Intn(18)))
				}
			}
		}
	}

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go worker(int64(w*7919+1), ops/concurrency, &wg)
	}
	wg.Wait()

	// Independent recomputation from the final per-request states.
	type agg struct {
		units, parallel, storage int64
		committed, actual        map[Month]int64
	}
	levels := map[string]*agg{}
	get := func(k string) *agg {
		a := levels[k]
		if a == nil {
			a = &agg{committed: map[Month]int64{}, actual: map[Month]int64{}}
			levels[k] = a
		}
		return a
	}
	mu.Lock()
	for i := range state {
		s := state[i]
		if !s.approved {
			continue
		}
		uid := s.req.UserID
		did := cfg.UserDept[uid]
		tid := cfg.DeptTenant[did]
		for _, key := range []string{"u:" + uid, "d:" + did, "t:" + tid} {
			a := get(key)
			if !s.closed {
				a.units += s.req.Units
				a.parallel++
				a.storage += s.req.StoragePerUnit * s.req.Units
			}
			for m := 0; m < s.req.DurationMonths; m++ {
				mo := s.req.StartMonth.Add(m)
				actual := s.actuals[mo]
				committed := actual
				if !s.closed && s.req.MonthlyFee*s.req.Units > committed {
					committed = s.req.MonthlyFee * s.req.Units
				}
				a.committed[mo] += committed
				a.actual[mo] += actual
			}
		}
	}
	mu.Unlock()

	// Compare every user (hence every dept and tenant) on every month.
	for _, uid := range userIDs {
		for m := 0; m < 18; m++ {
			mo := base.Add(m)
			rep := l.Query(uid, mo)
			did := cfg.UserDept[uid]
			tid := cfg.DeptTenant[did]
			checks := []struct {
				name    string
				usage   Usage
				mr      MonthReport
				wantKey string
			}{
				{"user", rep.User, rep.UserMonth, "u:" + uid},
				{"dept", rep.Dept, rep.DeptMonth, "d:" + did},
				{"tenant", rep.Tenant, rep.TenantMon, "t:" + tid},
			}
			for _, c := range checks {
				want := get(c.wantKey)
				if c.usage.Units != want.units || c.usage.Parallel != want.parallel || c.usage.StorageByte != want.storage {
					t.Fatalf("%s %s usage = %+v, want units=%d parallel=%d storage=%d",
						c.name, c.wantKey, c.usage, want.units, want.parallel, want.storage)
				}
				if c.mr.Committed != want.committed[mo] || c.mr.Actual != want.actual[mo] {
					t.Fatalf("%s %s month %s = committed %d actual %d, want committed %d actual %d",
						c.name, c.wantKey, mo, c.mr.Committed, c.mr.Actual, want.committed[mo], want.actual[mo])
				}
				if c.mr.Open != c.mr.Committed-c.mr.Actual {
					t.Fatalf("%s %s month %s open = %d, want %d", c.name, c.wantKey, mo, c.mr.Open, c.mr.Committed-c.mr.Actual)
				}
			}
		}
	}
}
