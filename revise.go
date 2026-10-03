package quotaledger

import "errors"

var (
	ErrRequestNotFound = errors.New("quotaledger: request not found")
	ErrInvalidRevision = errors.New("quotaledger: invalid revision")
)

// Revise 接收一次计费修订。较大修订号整体替换旧快照，
// 较小或相同修订号不产生影响。实账即使超额也必须入账。
func (l *Ledger) Revise(rev Revision) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	req, ok := l.requests[rev.RequestID]
	if !ok {
		return ErrRequestNotFound
	}
	if req.hasRevision && rev.Number <= req.maxRevision {
		return nil // 旧修订重放，不产生影响
	}
	for _, v := range rev.Actuals {
		if v < 0 {
			return ErrInvalidRevision
		}
	}

	un, dn, tn, err := l.chain(req.req.User)
	if err != nil {
		return err
	}

	// 收集受影响的月份：旧快照、新快照与承诺月份的并集。
	affected := make(map[Month]struct{})
	for i := 0; i < req.req.DurationMonths; i++ {
		affected[req.req.StartMonth+Month(i)] = struct{}{}
	}
	for m := range req.actuals {
		affected[m] = struct{}{}
	}
	for m := range rev.Actuals {
		affected[m] = struct{}{}
	}

	for m := range affected {
		oldBudget := req.budgetAt(m)
		oldActual := req.actuals[m]
		newActual := rev.Actuals[m]
		inCommitment := m >= req.req.StartMonth && m < req.req.StartMonth+Month(req.req.DurationMonths)
		var newBudget int64
		if req.closed || !inCommitment {
			newBudget = newActual
		} else if e := req.estimated(); e > newActual {
			newBudget = e
		} else {
			newBudget = newActual
		}
		l.applyMonthDelta(un, dn, tn, m, newBudget-oldBudget, newActual-oldActual)
	}

	req.actuals = make(map[Month]int64, len(rev.Actuals))
	for m, v := range rev.Actuals {
		if v > 0 {
			req.actuals[m] = v
		}
	}
	req.maxRevision = rev.Number
	req.hasRevision = true
	return nil
}
