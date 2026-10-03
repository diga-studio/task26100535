package quota

// months returns the calendar months covered by r.
func months(r Request) []Month {
	ms := make([]Month, r.DurationMonths)
	for i := range ms {
		ms[i] = r.StartMonth.Add(i)
	}
	return ms
}

// committedFor returns the request's budget occupancy contribution for m.
func (s *requestState) committedFor(m Month) int64 {
	actual := s.actuals[m]
	if s.closed {
		return actual
	}
	if est := s.req.MonthlyFee * s.req.Units; est > actual {
		return est
	}
	return actual
}

func newAgg() *levelAgg {
	return &levelAgg{committed: make(map[Month]int64), actual: make(map[Month]int64)}
}

// aggs returns the aggregate buckets of the three levels owning userID,
// creating them lazily. The tenant bucket is nil if the user is unmapped.
func (l *Ledger) aggs(userID string) (user, dept, tenant *levelAgg) {
	user = l.users[userID]
	if user == nil {
		user = newAgg()
		l.users[userID] = user
	}
	deptID := l.cfg.UserDept[userID]
	dept = l.depts[deptID]
	if dept == nil {
		dept = newAgg()
		l.depts[deptID] = dept
	}
	tenantID := l.cfg.DeptTenant[deptID]
	tenant = l.tenants[tenantID]
	if tenant == nil {
		tenant = newAgg()
		l.tenants[tenantID] = tenant
	}
	return user, dept, tenant
}

// limitsOf returns the limits configured for the three levels of userID.
func (l *Ledger) limitsOf(userID string) (user, dept, tenant Limits) {
	deptID := l.cfg.UserDept[userID]
	tenantID := l.cfg.DeptTenant[deptID]
	return l.cfg.UserLimits[userID], l.cfg.DeptLimits[deptID], l.cfg.TenantLim[tenantID]
}

// addInstant charges (delta=+1) or releases (delta=-1) the instantaneous
// occupancy of s on all three levels.
func (l *Ledger) addInstant(s *requestState, delta int64) {
	user, dept, tenant := l.aggs(s.req.UserID)
	for _, a := range []*levelAgg{user, dept, tenant} {
		a.units += delta * s.req.Units
		a.parallel += delta
		a.storage += delta * s.req.StoragePerUnit * s.req.Units
	}
}

// addMonth shifts the committed and actual budget of all three levels for
// month m by the given deltas.
func (l *Ledger) addMonth(userID string, m Month, committedDelta, actualDelta int64) {
	user, dept, tenant := l.aggs(userID)
	for _, a := range []*levelAgg{user, dept, tenant} {
		a.committed[m] += committedDelta
		a.actual[m] += actualDelta
	}
}

// approve implements Approve with the mutex already held.
func (l *Ledger) approve(req Request) bool {
	if req.DurationMonths < 1 || req.DurationMonths > 36 {
		return false
	}
	if existing, ok := l.requests[req.ID]; ok {
		// Repeated approval of the same live request is a no-op; a closed
		// ID must not be recreated.
		return !existing.closed
	}

	userLim, deptLim, tenantLim := l.limitsOf(req.UserID)
	user, dept, tenant := l.aggs(req.UserID)

	est := req.MonthlyFee * req.Units
	storage := req.StoragePerUnit * req.Units
	for i, a := range []*levelAgg{user, dept, tenant} {
		lim := []Limits{userLim, deptLim, tenantLim}[i]
		if a.units+req.Units > lim.MaxUnits ||
			a.parallel+1 > lim.MaxParallel ||
			a.storage+storage > lim.MaxStorageBytes {
			return false
		}
		for _, m := range months(req) {
			if a.committed[m]+est > lim.MonthlyBudget {
				return false
			}
		}
	}

	s := &requestState{req: req, actuals: make(map[Month]int64)}
	l.requests[req.ID] = s
	l.addInstant(s, 1)
	for _, m := range months(req) {
		l.addMonth(req.UserID, m, est, 0)
	}
	return true
}
