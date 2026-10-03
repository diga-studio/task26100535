package quota

// alertOf classifies committed against the monthly budget limit.
func alertOf(committed, limit int64) AlertLevel {
	switch {
	case committed*100 >= limit*100:
		return AlertBlocked
	case committed*100 >= limit*95:
		return AlertCritical
	case committed*100 >= limit*80:
		return AlertWarning
	default:
		return AlertOK
	}
}

func monthReport(a *levelAgg, m Month, lim Limits) MonthReport {
	committed := a.committed[m]
	actual := a.actual[m]
	return MonthReport{
		Committed: committed,
		Actual:    actual,
		Open:      committed - actual,
		Alert:     alertOf(committed, lim.MonthlyBudget),
	}
}

func usageOf(a *levelAgg) Usage {
	return Usage{Units: a.units, Parallel: a.parallel, StorageByte: a.storage}
}

// query implements Query with the mutex already held.
func (l *Ledger) query(userID string, month Month) Report {
	user, dept, tenant := l.aggs(userID)
	userLim, deptLim, tenantLim := l.limitsOf(userID)
	return Report{
		User:      usageOf(user),
		Dept:      usageOf(dept),
		Tenant:    usageOf(tenant),
		UserMonth: monthReport(user, month, userLim),
		DeptMonth: monthReport(dept, month, deptLim),
		TenantMon: monthReport(tenant, month, tenantLim),
	}
}
