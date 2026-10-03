package quota

import "testing"

// testConfig builds a one-tenant / one-dept / one-user hierarchy.
func testConfig(budget int64) Config {
	return Config{
		UserDept:   map[string]string{"u1": "d1"},
		DeptTenant: map[string]string{"d1": "t1"},
		UserLimits: map[string]Limits{"u1": {MaxUnits: 100, MaxParallel: 10, MaxStorageBytes: 1 << 20, MonthlyBudget: budget}},
		DeptLimits: map[string]Limits{"d1": {MaxUnits: 100, MaxParallel: 10, MaxStorageBytes: 1 << 20, MonthlyBudget: budget}},
		TenantLim:  map[string]Limits{"t1": {MaxUnits: 100, MaxParallel: 10, MaxStorageBytes: 1 << 20, MonthlyBudget: budget}},
	}
}

// Acceptance scenario: monthly budget 10000, a request estimated at 8000
// for each of two consecutive months.
func TestBillingRevisionLifecycle(t *testing.T) {
	l := NewLedger(testConfig(10000))
	m1 := Month{Year: 2026, Month: 1}
	m2 := Month{Year: 2026, Month: 2}

	ok := l.Approve(Request{
		ID: "r1", UserID: "u1", Units: 1, StoragePerUnit: 10,
		MonthlyFee: 8000, StartMonth: m1, DurationMonths: 2,
	})
	if !ok {
		t.Fatal("approve failed")
	}

	// Before any actuals: both months committed at the estimate.
	if got := l.Query("u1", m1).UserMonth.Committed; got != 8000 {
		t.Fatalf("m1 committed = %d, want 8000", got)
	}
	if got := l.Query("u1", m2).UserMonth.Committed; got != 8000 {
		t.Fatalf("m2 committed = %d, want 8000", got)
	}

	// First actual: month 1 alone at 12000 (over the estimate, even over
	// the budget — it must still be booked).
	l.Revise(Revision{RequestID: "r1", Revision: 1, Actuals: map[Month]int64{m1: 12000}})
	if got := l.Query("u1", m1).UserMonth.Committed; got != 12000 {
		t.Fatalf("m1 committed after rev1 = %d, want 12000", got)
	}
	if got := l.Query("u1", m2).UserMonth.Committed; got != 8000 {
		t.Fatalf("m2 committed after rev1 = %d, want 8000", got)
	}

	// Close: instantaneous occupancy zeroed, budget keeps only actuals.
	l.Close("r1")
	q := l.Query("u1", m1)
	if q.User != (Usage{}) {
		t.Fatalf("usage after close = %+v, want zero", q.User)
	}
	if got := q.UserMonth.Committed; got != 12000 {
		t.Fatalf("m1 committed after close = %d, want 12000", got)
	}
	if got := l.Query("u1", m2).UserMonth.Committed; got != 0 {
		t.Fatalf("m2 committed after close = %d, want 0", got)
	}

	// Second revision moves the cost to month 2 only.
	l.Revise(Revision{RequestID: "r1", Revision: 2, Actuals: map[Month]int64{m2: 4000}})
	if got := l.Query("u1", m1).UserMonth.Committed; got != 0 {
		t.Fatalf("m1 committed after rev2 = %d, want 0", got)
	}
	if got := l.Query("u1", m2).UserMonth.Committed; got != 4000 {
		t.Fatalf("m2 committed after rev2 = %d, want 4000", got)
	}

	// Replaying the stale revision must not change anything.
	l.Revise(Revision{RequestID: "r1", Revision: 1, Actuals: map[Month]int64{m1: 12000}})
	if got := l.Query("u1", m1).UserMonth.Committed; got != 0 {
		t.Fatalf("m1 committed after stale replay = %d, want 0", got)
	}
	if got := l.Query("u1", m2).UserMonth.Committed; got != 4000 {
		t.Fatalf("m2 committed after stale replay = %d, want 4000", got)
	}

	// Close is idempotent and never removes actuals.
	l.Close("r1")
	if got := l.Query("u1", m2).UserMonth.Committed; got != 4000 {
		t.Fatalf("m2 committed after second close = %d, want 4000", got)
	}
	if got := l.Query("u1", m2).UserMonth.Actual; got != 4000 {
		t.Fatalf("m2 actual = %d, want 4000", got)
	}
}

func TestApproveRules(t *testing.T) {
	l := NewLedger(testConfig(10000))
	m := Month{Year: 2026, Month: 3}
	req := Request{ID: "a", UserID: "u1", Units: 1, MonthlyFee: 6000, StartMonth: m, DurationMonths: 1}

	if !l.Approve(req) {
		t.Fatal("first approve failed")
	}
	if !l.Approve(req) {
		t.Fatal("repeated approve of live request must succeed as no-op")
	}
	if got := l.Query("u1", m).User.Parallel; got != 1 {
		t.Fatalf("parallel after duplicate approve = %d, want 1", got)
	}

	// A second request would push the month over 10000: rejected whole.
	if l.Approve(Request{ID: "b", UserID: "u1", Units: 1, MonthlyFee: 4001, StartMonth: m, DurationMonths: 1}) {
		t.Fatal("over-budget approve should fail")
	}
	if got := l.Query("u1", m).User.Parallel; got != 1 {
		t.Fatalf("parallel after rejected approve = %d, want 1", got)
	}
	if got := l.Query("u1", m).UserMonth.Committed; got != 6000 {
		t.Fatalf("committed after rejected approve = %d, want 6000", got)
	}

	// Exactly hitting the limit is allowed.
	if !l.Approve(Request{ID: "c", UserID: "u1", Units: 1, MonthlyFee: 4000, StartMonth: m, DurationMonths: 1}) {
		t.Fatal("approve exactly at the limit should succeed")
	}
	if got := l.Query("u1", m).UserMonth.Committed; got != 10000 {
		t.Fatalf("committed = %d, want 10000", got)
	}

	// A closed ID cannot be recreated.
	l.Close("a")
	if l.Approve(req) {
		t.Fatal("re-approving a closed ID must fail")
	}
}
