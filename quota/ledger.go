package quota

import "sync"

// requestState is the mutable ledger record of one request ID.
type requestState struct {
	req      Request
	closed   bool
	revision uint64
	actuals  map[Month]int64
}

// levelAgg holds the running aggregates of one entity (user, dept or tenant).
type levelAgg struct {
	units     int64
	parallel  int64
	storage   int64
	committed map[Month]int64
	actual    map[Month]int64
}

// Ledger is the in-memory multi-level quota ledger. All operations are
// serialized by a single mutex so every operation takes effect atomically
// and every Query observes one consistent instant.
type Ledger struct {
	mu       sync.Mutex
	cfg      Config
	requests map[string]*requestState
	users    map[string]*levelAgg
	depts    map[string]*levelAgg
	tenants  map[string]*levelAgg
}

// NewLedger builds an empty ledger from the fixed configuration.
func NewLedger(cfg Config) *Ledger {
	return &Ledger{
		cfg:      cfg,
		requests: make(map[string]*requestState),
		users:    make(map[string]*levelAgg),
		depts:    make(map[string]*levelAgg),
		tenants:  make(map[string]*levelAgg),
	}
}

// Approve attempts to record req and charge its occupancy to all three
// levels. It reports whether the request is (now) approved.
func (l *Ledger) Approve(req Request) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.approve(req)
}

// Revise applies a billing revision snapshot to an approved request.
func (l *Ledger) Revise(rev Revision) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.revise(rev)
}

// Close releases the instantaneous occupancy of a request while keeping
// its actual cost in the budget books. It is idempotent.
func (l *Ledger) Close(requestID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.close(requestID)
}

// Query returns a consistent snapshot of the user's, department's and
// tenant's usage plus their budget view for the given month.
func (l *Ledger) Query(userID string, month Month) Report {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.query(userID, month)
}
