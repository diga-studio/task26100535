package quotaledger

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
)

// 混合操作并发执行后，各级最终用量必须与按请求最终状态
// （是否关闭、最高修订号快照）从头独立汇总的结果完全一致。
func TestMixedOperationsConsistency(t *testing.T) {
	const (
		numUsers    = 8
		numRequests = 200
		duration    = 3
	)
	base := MonthOf(2026, 1)
	big := int64(1) << 50

	cfg := Config{
		UserDept:     map[string]string{},
		DeptTenant:   map[string]string{"d0": "t0", "d1": "t0"},
		UserLimits:   map[string]Limits{},
		DeptLimits:   map[string]Limits{},
		TenantLimits: map[string]Limits{},
	}
	lim := Limits{MaxUnits: big, MaxParallel: big, MaxStorageBytes: big, MaxMonthlyBudget: big}
	cfg.TenantLimits["t0"] = lim
	cfg.DeptLimits["d0"] = lim
	cfg.DeptLimits["d1"] = lim
	users := make([]string, numUsers)
	for i := range users {
		users[i] = fmt.Sprintf("u%d", i)
		cfg.UserDept[users[i]] = fmt.Sprintf("d%d", i%2)
		cfg.UserLimits[users[i]] = lim
	}
	l := New(cfg)

	type approvedReq struct {
		req    ApproveRequest
		closed bool
		hasRev bool
		revNum uint64
		snap   map[Month]int64
	}
	final := make(map[string]*approvedReq)
	var mu sync.Mutex

	// 请求参数按编号确定生成，保证同一编号的重复批准内容一致。
	reqFor := func(id string) ApproveRequest {
		var n int
		fmt.Sscanf(id, "r%d", &n)
		rng := rand.New(rand.NewSource(int64(n)*7919 + 13))
		return ApproveRequest{
			ID: id, User: users[n%numUsers],
			Units: int64(1 + rng.Intn(3)), StoragePerUnit: int64(rng.Intn(100)),
			MonthlyFeePerUnit: int64(rng.Intn(1000)),
			StartMonth:        base + Month(rng.Intn(3)), DurationMonths: duration,
		}
	}

	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for i := 0; i < 150; i++ {
				id := fmt.Sprintf("r%d", rng.Intn(numRequests))
				switch rng.Intn(4) {
				case 0:
					mu.Lock()
					if _, ok := final[id]; !ok {
						req := reqFor(id)
						if err := l.Approve(req); err != nil {
							mu.Unlock()
							t.Errorf("approve %s: %v", id, err)
							continue
						}
						final[id] = &approvedReq{req: req, snap: map[Month]int64{}}
					}
					mu.Unlock()
				case 1:
					rev := Revision{RequestID: id, Number: uint64(rng.Intn(10)), Actuals: map[Month]int64{}}
					for j := 0; j < duration; j++ {
						if rng.Intn(2) == 0 {
							rev.Actuals[base+Month(j)] = int64(rng.Intn(2000))
						}
					}
					mu.Lock()
					ar, approved := final[id]
					if approved {
						if err := l.Revise(rev); err != nil {
							mu.Unlock()
							t.Errorf("revise %s: %v", id, err)
							continue
						}
						if !ar.hasRev || rev.Number > ar.revNum {
							ar.hasRev = true
							ar.revNum = rev.Number
							ar.snap = rev.Actuals
						}
					}
					mu.Unlock()
				case 2:
					mu.Lock()
					if ar, ok := final[id]; ok {
						if err := l.Close(id); err != nil {
							mu.Unlock()
							t.Errorf("close %s: %v", id, err)
							continue
						}
						ar.closed = true
					}
					mu.Unlock()
				default:
					_, _ = l.Query(users[rng.Intn(numUsers)], base+Month(rng.Intn(5)))
				}
			}
		}(int64(w) + 1)
	}
	wg.Wait()

	// 独立汇总：按请求最终状态从头计算每个用户的预期用量。
	type agg struct {
		units, parallel, storage int64
		budget, actual           map[Month]int64
	}
	expected := map[string]*agg{}
	get := func(u string) *agg {
		a, ok := expected[u]
		if !ok {
			a = &agg{budget: map[Month]int64{}, actual: map[Month]int64{}}
			expected[u] = a
		}
		return a
	}
	for _, ar := range final {
		a := get(ar.req.User)
		if !ar.closed {
			a.units += ar.req.Units
			a.parallel++
			a.storage += ar.req.Units * ar.req.StoragePerUnit
		}
		months := map[Month]struct{}{}
		for i := 0; i < ar.req.DurationMonths; i++ {
			months[ar.req.StartMonth+Month(i)] = struct{}{}
		}
		for m := range ar.snap {
			months[m] = struct{}{}
		}
		est := ar.req.Units * ar.req.MonthlyFeePerUnit
		for m := range months {
			act := ar.snap[m]
			a.actual[m] += act
			inCommitment := m >= ar.req.StartMonth && m < ar.req.StartMonth+Month(ar.req.DurationMonths)
			switch {
			case ar.closed || !inCommitment:
				a.budget[m] += act
			case est > act:
				a.budget[m] += est
			default:
				a.budget[m] += act
			}
		}
	}

	for _, u := range users {
		for mi := 0; mi < 6; mi++ {
			m := base + Month(mi)
			res, err := l.Query(u, m)
			if err != nil {
				t.Fatal(err)
			}
			a := get(u)
			if res.User.Units != a.units || res.User.Parallel != a.parallel ||
				res.User.StorageBytes != a.storage ||
				res.User.BudgetUsed != a.budget[m] || res.User.Actual != a.actual[m] {
				t.Fatalf("user %s month %d mismatch: got %+v want units=%d parallel=%d storage=%d budget=%d actual=%d",
					u, mi, res.User, a.units, a.parallel, a.storage, a.budget[m], a.actual[m])
			}
			if res.User.OpenCommitment != res.User.BudgetUsed-res.User.Actual {
				t.Fatalf("open commitment wrong: %+v", res.User)
			}
		}
	}
}
