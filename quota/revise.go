package quota

// revise implements Revise with the mutex already held.
func (l *Ledger) revise(rev Revision) {
	s, ok := l.requests[rev.RequestID]
	if !ok {
		return
	}
	if rev.Revision <= s.revision {
		return
	}
	s.revision = rev.Revision

	// Recompute the request's contribution for every covered month: the
	// snapshot replaces the old one wholesale, and covered months absent
	// from it count as zero.
	for _, m := range months(s.req) {
		oldCommitted := s.committedFor(m)
		oldActual := s.actuals[m]
		var newActual int64
		if v, ok := rev.Actuals[m]; ok && v > 0 {
			newActual = v
		}
		if newActual == 0 {
			delete(s.actuals, m)
		} else {
			s.actuals[m] = newActual
		}
		newCommitted := s.committedFor(m)
		if dc, da := newCommitted-oldCommitted, newActual-oldActual; dc != 0 || da != 0 {
			l.addMonth(s.req.UserID, m, dc, da)
		}
	}
}
