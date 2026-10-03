package quotaledger

import (
	"sync"
	"testing"
)

// 不同部门的请求并发争抢租户最后一份并行额度：
// 成功请求不能超占，失败请求不能残留占用。
func TestConcurrentContentionForLastSlot(t *testing.T) {
	cfg := Config{
		UserDept:   map[string]string{"u1": "d1", "u2": "d2"},
		DeptTenant: map[string]string{"d1": "t1", "d2": "t1"},
		UserLimits: map[string]Limits{
			"u1": {MaxUnits: 10, MaxParallel: 5, MaxStorageBytes: 1 << 40, MaxMonthlyBudget: 1 << 40},
			"u2": {MaxUnits: 10, MaxParallel: 5, MaxStorageBytes: 1 << 40, MaxMonthlyBudget: 1 << 40},
		},
		DeptLimits: map[string]Limits{
			"d1": {MaxUnits: 10, MaxParallel: 5, MaxStorageBytes: 1 << 40, MaxMonthlyBudget: 1 << 40},
			"d2": {MaxUnits: 10, MaxParallel: 5, MaxStorageBytes: 1 << 40, MaxMonthlyBudget: 1 << 40},
		},
		TenantLimits: map[string]Limits{
			"t1": {MaxUnits: 20, MaxParallel: 1, MaxStorageBytes: 1 << 40, MaxMonthlyBudget: 1 << 40},
		},
	}
	l := New(cfg)
	m := MonthOf(2026, 1)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []string{"r1", "r2"} {
		wg.Add(1)
		go func(i int, id, user string) {
			defer wg.Done()
			errs[i] = l.Approve(ApproveRequest{
				ID: id, User: user, Units: 1, StoragePerUnit: 100,
				MonthlyFeePerUnit: 500, StartMonth: m, DurationMonths: 3,
			})
		}(i, id, map[int]string{0: "u1", 1: "u2"}[i])
	}
	wg.Wait()

	var okCount int
	for _, err := range errs {
		if err == nil {
			okCount++
		} else if err != ErrQuotaExceeded {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if okCount != 1 {
		t.Fatalf("expected exactly 1 approval, got %d", okCount)
	}

	for _, user := range []string{"u1", "u2"} {
		res, err := l.Query(user, m)
		if err != nil {
			t.Fatal(err)
		}
		if res.Tenant.Parallel != 1 || res.Tenant.Units != 1 {
			t.Fatalf("tenant over-occupied: %+v", res.Tenant)
		}
		// 失败方不能残留任何占用。
		if res.User.Parallel == 0 {
			if res.User.Units != 0 || res.User.StorageBytes != 0 || res.User.BudgetUsed != 0 {
				t.Fatalf("failed request left residue for %s: %+v", user, res.User)
			}
		}
	}
}

// 每月预算 10000 分、连续两月预估各 8000 分的请求：
// 实账、关闭、修订替换与旧修订重放的完整场景。
func TestRevisionCloseAndReplay(t *testing.T) {
	lim := Limits{MaxUnits: 10, MaxParallel: 10, MaxStorageBytes: 1 << 40, MaxMonthlyBudget: 10000}
	cfg := Config{
		UserDept:     map[string]string{"u1": "d1"},
		DeptTenant:   map[string]string{"d1": "t1"},
		UserLimits:   map[string]Limits{"u1": lim},
		DeptLimits:   map[string]Limits{"d1": lim},
		TenantLimits: map[string]Limits{"t1": lim},
	}
	l := New(cfg)
	m1 := MonthOf(2026, 1)
	m2 := MonthOf(2026, 2)

	if err := l.Approve(ApproveRequest{
		ID: "r1", User: "u1", Units: 1, StoragePerUnit: 10,
		MonthlyFeePerUnit: 8000, StartMonth: m1, DurationMonths: 2,
	}); err != nil {
		t.Fatal(err)
	}

	// 首份实账仅第一月 12000 分（超额也必须入账）。
	if err := l.Revise(Revision{RequestID: "r1", Number: 1, Actuals: map[Month]int64{m1: 12000}}); err != nil {
		t.Fatal(err)
	}
	res, _ := l.Query("u1", m1)
	if res.User.BudgetUsed != 12000 || res.User.Actual != 12000 {
		t.Fatalf("m1 after revise: %+v", res.User)
	}

	// 关闭后两月占用应为 12000 和 0。
	if err := l.Close("r1"); err != nil {
		t.Fatal(err)
	}
	res, _ = l.Query("u1", m1)
	if res.User.BudgetUsed != 12000 || res.User.Units != 0 || res.User.Parallel != 0 {
		t.Fatalf("m1 after close: %+v", res.User)
	}
	res, _ = l.Query("u1", m2)
	if res.User.BudgetUsed != 0 || res.User.Actual != 0 {
		t.Fatalf("m2 after close: %+v", res.User)
	}

	// 更新修订仅第二月 4000 分后，应变为 0 和 4000。
	if err := l.Revise(Revision{RequestID: "r1", Number: 2, Actuals: map[Month]int64{m2: 4000}}); err != nil {
		t.Fatal(err)
	}
	res, _ = l.Query("u1", m1)
	if res.User.BudgetUsed != 0 || res.User.Actual != 0 {
		t.Fatalf("m1 after revision 2: %+v", res.User)
	}
	res, _ = l.Query("u1", m2)
	if res.User.BudgetUsed != 4000 || res.User.Actual != 4000 {
		t.Fatalf("m2 after revision 2: %+v", res.User)
	}

	// 重放旧修订不得改变结果。
	if err := l.Revise(Revision{RequestID: "r1", Number: 1, Actuals: map[Month]int64{m1: 12000}}); err != nil {
		t.Fatal(err)
	}
	res, _ = l.Query("u1", m1)
	if res.User.BudgetUsed != 0 || res.User.Actual != 0 {
		t.Fatalf("m1 after replay: %+v", res.User)
	}
	res, _ = l.Query("u1", m2)
	if res.User.BudgetUsed != 4000 || res.User.Actual != 4000 {
		t.Fatalf("m2 after replay: %+v", res.User)
	}
}

// 重复批准不重复扣额；已关闭编号不能重建；被拒绝请求可按余额重试。
func TestApproveIdempotencyAndRetry(t *testing.T) {
	lim := Limits{MaxUnits: 2, MaxParallel: 2, MaxStorageBytes: 1 << 40, MaxMonthlyBudget: 10000}
	cfg := Config{
		UserDept:     map[string]string{"u1": "d1"},
		DeptTenant:   map[string]string{"d1": "t1"},
		UserLimits:   map[string]Limits{"u1": lim},
		DeptLimits:   map[string]Limits{"d1": lim},
		TenantLimits: map[string]Limits{"t1": lim},
	}
	l := New(cfg)
	m := MonthOf(2026, 1)
	req := func(id string, units, fee int64) ApproveRequest {
		return ApproveRequest{ID: id, User: "u1", Units: units, StoragePerUnit: 1,
			MonthlyFeePerUnit: fee, StartMonth: m, DurationMonths: 1}
	}

	if err := l.Approve(req("a", 1, 8000)); err != nil {
		t.Fatal(err)
	}
	// 重复批准不重复扣额。
	if err := l.Approve(req("a", 1, 8000)); err != nil {
		t.Fatal(err)
	}
	res, _ := l.Query("u1", m)
	if res.User.Parallel != 1 || res.User.BudgetUsed != 8000 {
		t.Fatalf("duplicate approve double-counted: %+v", res.User)
	}

	// 预算不足被拒绝；释放后可按余额重试成功。
	if err := l.Approve(req("b", 1, 5000)); err != ErrQuotaExceeded {
		t.Fatalf("expected quota exceeded, got %v", err)
	}
	if err := l.Close("a"); err != nil {
		t.Fatal(err)
	}
	if err := l.Approve(req("b", 1, 5000)); err != nil {
		t.Fatalf("retry after close should succeed: %v", err)
	}

	// 已关闭编号不能重新创建。
	if err := l.Approve(req("a", 1, 100)); err != ErrRequestClosed {
		t.Fatalf("expected ErrRequestClosed, got %v", err)
	}
}
