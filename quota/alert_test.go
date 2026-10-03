package quota

import "testing"

func TestAlertThresholds(t *testing.T) {
	cases := []struct {
		committed int64
		want      AlertLevel
	}{
		{0, AlertOK}, {7999, AlertOK},
		{8000, AlertWarning}, {9499, AlertWarning},
		{9500, AlertCritical}, {9999, AlertCritical},
		{10000, AlertBlocked}, {12000, AlertBlocked},
	}
	for _, c := range cases {
		if got := alertOf(c.committed, 10000); got != c.want {
			t.Errorf("alertOf(%d, 10000) = %v, want %v", c.committed, got, c.want)
		}
	}
}

func TestAlertViaQuery(t *testing.T) {
	l := NewLedger(testConfig(10000))
	m := Month{Year: 2026, Month: 5}
	if !l.Approve(Request{ID: "w", UserID: "u1", Units: 1, MonthlyFee: 8500, StartMonth: m, DurationMonths: 1}) {
		t.Fatal("approve failed")
	}
	if got := l.Query("u1", m).UserMonth.Alert; got != AlertWarning {
		t.Fatalf("alert = %v, want WARNING", got)
	}
	// Actual cost may exceed the budget and must still be booked.
	l.Revise(Revision{RequestID: "w", Revision: 1, Actuals: map[Month]int64{m: 11000}})
	q := l.Query("u1", m).UserMonth
	if q.Alert != AlertBlocked || q.Committed != 11000 || q.Actual != 11000 || q.Open != 0 {
		t.Fatalf("after over-budget actual: %+v", q)
	}
}
