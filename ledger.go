package quotaledger

import "sync"

// node 是账本中的一个层级节点（用户/部门/租户）。
type node struct {
	units      int64
	parallel   int64
	storage    int64
	budgetUsed map[Month]int64
	actual     map[Month]int64
}

func newNode() *node {
	return &node{
		budgetUsed: make(map[Month]int64),
		actual:     make(map[Month]int64),
	}
}

// request 记录一条已批准请求的内部状态。
type request struct {
	req         ApproveRequest
	closed      bool
	maxRevision uint64
	hasRevision bool
	actuals     map[Month]int64 // 最新修订快照
}

// months 返回请求覆盖的所有月份。
func (r *request) months() []Month {
	ms := make([]Month, r.req.DurationMonths)
	for i := range ms {
		ms[i] = r.req.StartMonth + Month(i)
	}
	return ms
}

// estimated 返回请求每月的预计费用。
func (r *request) estimated() int64 {
	return r.req.Units * r.req.MonthlyFeePerUnit
}

// budgetAt 返回请求在指定月份当前的预算占用。
func (r *request) budgetAt(m Month) int64 {
	a := r.actuals[m]
	if r.closed || m < r.req.StartMonth || m >= r.req.StartMonth+Month(r.req.DurationMonths) {
		return a
	}
	if e := r.estimated(); e > a {
		return e
	}
	return a
}

// Ledger 是统一额度账本，所有操作通过一把互斥锁保证整体生效。
type Ledger struct {
	mu       sync.Mutex
	users    map[string]*node
	depts    map[string]*node
	tenants  map[string]*node
	userDept map[string]string
	deptTen  map[string]string
	userLim  map[string]Limits
	deptLim  map[string]Limits
	tenLim   map[string]Limits
	requests map[string]*request
}

// New 依据配置创建账本。
func New(cfg Config) *Ledger {
	l := &Ledger{
		users:    make(map[string]*node),
		depts:    make(map[string]*node),
		tenants:  make(map[string]*node),
		userDept: cfg.UserDept,
		deptTen:  cfg.DeptTenant,
		userLim:  cfg.UserLimits,
		deptLim:  cfg.DeptLimits,
		tenLim:   cfg.TenantLimits,
		requests: make(map[string]*request),
	}
	for u := range cfg.UserLimits {
		l.users[u] = newNode()
	}
	for d := range cfg.DeptLimits {
		l.depts[d] = newNode()
	}
	for t := range cfg.TenantLimits {
		l.tenants[t] = newNode()
	}
	return l
}

// chain 返回用户对应的用户、部门、租户三级节点。
func (l *Ledger) chain(user string) (un, dn, tn *node, err error) {
	un, ok := l.users[user]
	if !ok {
		return nil, nil, nil, errUnknownUser
	}
	dept, ok := l.userDept[user]
	if !ok {
		return nil, nil, nil, errUnknownUser
	}
	dn, ok = l.depts[dept]
	if !ok {
		return nil, nil, nil, errUnknownDept
	}
	tenant, ok := l.deptTen[dept]
	if !ok {
		return nil, nil, nil, errUnknownDept
	}
	tn, ok = l.tenants[tenant]
	if !ok {
		return nil, nil, nil, errUnknownTenant
	}
	return un, dn, tn, nil
}

// applyDelta 将一条请求在某月的预算/实账变化量应用到三级节点。
func (l *Ledger) applyMonthDelta(un, dn, tn *node, m Month, budgetDelta, actualDelta int64) {
	for _, n := range []*node{un, dn, tn} {
		n.budgetUsed[m] += budgetDelta
		n.actual[m] += actualDelta
	}
}

// applyResourceDelta 将资源占用变化量应用到三级节点。
func (l *Ledger) applyResourceDelta(un, dn, tn *node, units, parallel, storage int64) {
	for _, n := range []*node{un, dn, tn} {
		n.units += units
		n.parallel += parallel
		n.storage += storage
	}
}
