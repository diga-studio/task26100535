package quotaledger

import "errors"

var (
	ErrInvalidRequest = errors.New("quotaledger: invalid request")
	ErrRequestClosed  = errors.New("quotaledger: request id already closed")
	ErrQuotaExceeded  = errors.New("quotaledger: quota exceeded")
)

// Approve 批准一个请求。重复批准同一编号不重复扣额；
// 已关闭编号不能重新创建；任何一处超额则整个请求不占额。
func (l *Ledger) Approve(r ApproveRequest) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if r.ID == "" || r.Units <= 0 || r.StoragePerUnit < 0 ||
		r.MonthlyFeePerUnit < 0 || r.DurationMonths < 1 || r.DurationMonths > 36 {
		return ErrInvalidRequest
	}

	if existing, ok := l.requests[r.ID]; ok {
		if existing.closed {
			return ErrRequestClosed
		}
		return nil // 重复批准不重复扣额
	}

	un, dn, tn, err := l.chain(r.User)
	if err != nil {
		return err
	}

	storage := r.Units * r.StoragePerUnit
	est := r.Units * r.MonthlyFeePerUnit

	levels := []struct {
		n   *node
		lim Limits
	}{
		{un, l.userLim[r.User]},
		{dn, l.deptLim[l.userDept[r.User]]},
		{tn, l.tenLim[l.deptTen[l.userDept[r.User]]]},
	}

	for _, lv := range levels {
		if lv.n.units+r.Units > lv.lim.MaxUnits ||
			lv.n.parallel+1 > lv.lim.MaxParallel ||
			lv.n.storage+storage > lv.lim.MaxStorageBytes {
			return ErrQuotaExceeded
		}
		for i := 0; i < r.DurationMonths; i++ {
			m := r.StartMonth + Month(i)
			if lv.n.budgetUsed[m]+est > lv.lim.MaxMonthlyBudget {
				return ErrQuotaExceeded
			}
		}
	}

	req := &request{req: r, actuals: make(map[Month]int64)}
	l.requests[r.ID] = req
	l.applyResourceDelta(un, dn, tn, r.Units, 1, storage)
	for i := 0; i < r.DurationMonths; i++ {
		m := r.StartMonth + Month(i)
		l.applyMonthDelta(un, dn, tn, m, est, 0)
	}
	return nil
}
