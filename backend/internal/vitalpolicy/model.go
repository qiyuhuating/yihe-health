package vitalpolicy

// Alert is the vital-alert snapshot consumed by presentation policy.
// Persistence adapters return this contract without making policy depend on SQL.
type Alert struct {
	ID            int64
	DeviceID      *int64
	OldID         *int64
	Hr            *int64
	Bp            *int64
	RateState     *string
	PreState      *string
	RateDone      bool
	PreDone       bool
	CreateTime    *string
	OldPersonName *string
	HrHigh, HrLow *int
	BpHigh, BpLow *int
}
