package quotaledger

// Month 表示一个自然月，以绝对月份序号存储（year*12 + month-1）。
type Month int

// MonthOf 由年份与月份（1-12）构造 Month。
func MonthOf(year, month int) Month {
	return Month(year*12 + month - 1)
}

// YearMonth 返回 Month 对应的年份与月份（1-12）。
func (m Month) YearMonth() (int, int) {
	return int(m) / 12, int(m)%12 + 1
}

// Limits 是某一层级的额度上限配置。
type Limits struct {
	MaxUnits         int64 // 在用单位数上限
	MaxParallel      int64 // 并行请求数上限
	MaxStorageBytes  int64 // 存储字节数上限
	MaxMonthlyBudget int64 // 逐月预算上限（整数分）
}

// Config 为初始化配置，运行中不变。
type Config struct {
	UserDept     map[string]string // 用户 -> 部门
	DeptTenant   map[string]string // 部门 -> 租户
	UserLimits   map[string]Limits
	DeptLimits   map[string]Limits
	TenantLimits map[string]Limits
}

// ApproveRequest 描述一次批准请求。
type ApproveRequest struct {
	ID                string // 唯一编号
	User              string
	Units             int64 // 资源数量
	StoragePerUnit    int64 // 每单位存储字节数
	MonthlyFeePerUnit int64 // 每单位月费（整数分）
	StartMonth        Month // 起始自然月
	DurationMonths    int   // 持续期 1-36 个月
}

// Revision 是一次计费修订，携带覆盖承诺月份的累计实账快照。
type Revision struct {
	RequestID string
	Number    uint64          // 修订号，较大者整体替换旧快照
	Actuals   map[Month]int64 // 各月累计实账（整数分），未出现的月份视为零
}

// Status 是预算告警状态。
type Status int

const (
	StatusOK Status = iota
	StatusWarning
	StatusCritical
	StatusBlocked
)

func (s Status) String() string {
	switch s {
	case StatusWarning:
		return "WARNING"
	case StatusCritical:
		return "CRITICAL"
	case StatusBlocked:
		return "BLOCKED"
	default:
		return "OK"
	}
}

// LevelUsage 是某一层级在指定月份的用量视图。
type LevelUsage struct {
	Units          int64
	Parallel       int64
	StorageBytes   int64
	BudgetUsed     int64 // 预算占用
	Actual         int64 // 实账
	OpenCommitment int64 // 未结承诺 = 预算占用 - 实账
	Status         Status
}

// QueryResult 是一次查询的结果，三个层级来自同一一致时刻。
type QueryResult struct {
	User   LevelUsage
	Dept   LevelUsage
	Tenant LevelUsage
}
