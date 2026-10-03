package quota

// close implements Close with the mutex already held.
func (l *Ledger) close(requestID string) {
	s, ok := l.requests[requestID]
	if !ok || s.closed {
		return
	}
	// Release instantaneous occupancy; budget occupancy drops from
	// max(estimate, actual) to the actual cost alone.
	l.addInstant(s, -1)
	s.closed = true
	for _, m := range months(s.req) {
		est := s.req.MonthlyFee * s.req.Units
		if actual := s.actuals[m]; est > actual {
			l.addMonth(s.req.UserID, m, actual-est, 0)
		}
	}
}
