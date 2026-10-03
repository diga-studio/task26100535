// Package quota implements an in-memory multi-level (tenant / department /
// user) quota ledger for cloud resource approvals and billing revisions.
package quota

import "fmt"

// Month identifies a natural calendar month.
type Month struct {
	Year  int
	Month int // 1-12
}

// Add returns the month n months after m.
func (m Month) Add(n int) Month {
	total := m.Year*12 + (m.Month - 1) + n
	return Month{Year: total / 12, Month: total%12 + 1}
}

func (m Month) String() string { return fmt.Sprintf("%04d-%02d", m.Year, m.Month) }

// Limits caps one level of the hierarchy. All money values are integer cents.
type Limits struct {
	MaxUnits        int64 // max resource units in use
	MaxParallel     int64 // max concurrently open requests
	MaxStorageBytes int64 // max storage bytes in use
	MonthlyBudget   int64 // max budget occupancy per calendar month, in cents
}

// Config is the fixed hierarchy and limit setup of a Ledger.
type Config struct {
	UserDept   map[string]string // user ID -> department ID
	DeptTenant map[string]string // department ID -> tenant ID
	UserLimits map[string]Limits
	DeptLimits map[string]Limits
	TenantLim  map[string]Limits
}

// Request describes a resource approval request.
type Request struct {
	ID             string
	UserID         string
	Units          int64
	StoragePerUnit int64
	MonthlyFee     int64 // estimated fee per unit... per month, in cents
	StartMonth     Month
	DurationMonths int // 1..36
}

// Revision is a billing correction snapshot for an approved request.
// Actuals maps a covered month to its cumulative actual cost in cents;
// covered months missing from the map count as zero.
type Revision struct {
	RequestID string
	Revision  uint64
	Actuals   map[Month]int64
}

// AlertLevel classifies budget occupancy against the monthly budget limit.
type AlertLevel int

const (
	AlertOK AlertLevel = iota
	AlertWarning
	AlertCritical
	AlertBlocked
)

func (a AlertLevel) String() string {
	switch a {
	case AlertWarning:
		return "WARNING"
	case AlertCritical:
		return "CRITICAL"
	case AlertBlocked:
		return "BLOCKED"
	default:
		return "OK"
	}
}

// Usage is the instantaneous (non-budget) occupancy of one level.
type Usage struct {
	Units       int64
	Parallel    int64
	StorageByte int64
}

// MonthReport is the budget view of one level for one month.
type MonthReport struct {
	Committed int64 // budget occupancy in cents
	Actual    int64 // cumulative actual cost in cents
	Open      int64 // outstanding commitment = Committed - Actual
	Alert     AlertLevel
}

// Report is the consistent snapshot returned by Query for a user, its
// department and its tenant.
type Report struct {
	User      Usage
	Dept      Usage
	Tenant    Usage
	UserMonth MonthReport
	DeptMonth MonthReport
	TenantMon MonthReport
}
