package quotaledger

// statusOf 按预算占用占上限的比例计算告警状态。
func statusOf(used, limit int64) Status {
	if limit <= 0 {
		if used > 0 {
			return StatusBlocked
		}
		return StatusOK
	}
	pct100 := used * 100
	switch {
	case pct100 >= limit*100:
		return StatusBlocked
	case pct100 >= limit*95:
		return StatusCritical
	case pct100 >= limit*80:
		return StatusWarning
	default:
		return StatusOK
	}
}

func usageOf(n *node, m Month, lim Limits) LevelUsage {
	used := n.budgetUsed[m]
	act := n.actual[m]
	return LevelUsage{
		Units:          n.units,
		Parallel:       n.parallel,
		StorageBytes:   n.storage,
		BudgetUsed:     used,
		Actual:         act,
		OpenCommitment: used - act,
		Status:         statusOf(used, lim.MaxMonthlyBudget),
	}
}

// Query 返回指定用户及其部门、租户在指定月份的用量视图。
// 三个层级的数据来自同一个一致时刻。
func (l *Ledger) Query(user string, month Month) (QueryResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	un, dn, tn, err := l.chain(user)
	if err != nil {
		return QueryResult{}, err
	}
	dept := l.userDept[user]
	tenant := l.deptTen[dept]
	return QueryResult{
		User:   usageOf(un, month, l.userLim[user]),
		Dept:   usageOf(dn, month, l.deptLim[dept]),
		Tenant: usageOf(tn, month, l.tenLim[tenant]),
	}, nil
}
