package quotaledger

// Close 关闭一个请求。可重复调用；关闭后资源占用归零，
// 各月预算占用只保留最新实账，实账不退。
func (l *Ledger) Close(requestID string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	req, ok := l.requests[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	if req.closed {
		return nil
	}

	un, dn, tn, err := l.chain(req.req.User)
	if err != nil {
		return err
	}

	l.applyResourceDelta(un, dn, tn, -req.req.Units, -1, -req.req.Units*req.req.StoragePerUnit)
	affected := make(map[Month]struct{})
	for i := 0; i < req.req.DurationMonths; i++ {
		affected[req.req.StartMonth+Month(i)] = struct{}{}
	}
	for m := range req.actuals {
		affected[m] = struct{}{}
	}
	for m := range affected {
		oldBudget := req.budgetAt(m)
		newBudget := req.actuals[m]
		l.applyMonthDelta(un, dn, tn, m, newBudget-oldBudget, 0)
	}
	req.closed = true
	return nil
}
