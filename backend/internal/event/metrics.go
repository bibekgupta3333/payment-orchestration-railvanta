package event

import (
	"strconv"
	"sync"
	"sync/atomic"
)

// lagBucketsMillis are cumulative upper bounds for dispatcher lag.
var lagBucketsMillis = []int64{10, 50, 100, 250, 500, 1000, 5000, 30000, 60000}

// Metrics is an in-process counter set for the outbox dispatcher. Lag is the
// time from a row becoming due (available_at) to its claim.
type Metrics struct {
	claims        atomic.Int64
	reclaims      atomic.Int64
	handled       atomic.Int64
	handlerErrors atomic.Int64
	leaseLost     atomic.Int64
	parked        atomic.Int64

	mu      sync.Mutex
	buckets []int64
	count   int64
	sumMS   float64
	maxMS   int64
}

// NewMetrics creates an empty metric set.
func NewMetrics() *Metrics { return &Metrics{buckets: make([]int64, len(lagBucketsMillis)+1)} }

// MetricsSnapshot is a point-in-time copy safe to log or serialize.
type MetricsSnapshot struct {
	Claims        int64            `json:"outbox_claims_total"`
	Reclaims      int64            `json:"outbox_reclaims_total"`
	Handled       int64            `json:"outbox_handled_total"`
	HandlerErrors int64            `json:"outbox_handler_errors_total"`
	LeaseLost     int64            `json:"outbox_lease_lost_total"`
	Parked        int64            `json:"outbox_parked_total"`
	LagCount      int64            `json:"dispatcher_lag_count"`
	LagSumMS      float64          `json:"dispatcher_lag_sum_ms"`
	LagMaxMS      int64            `json:"dispatcher_lag_max_ms"`
	LagBuckets    map[string]int64 `json:"dispatcher_lag_buckets_ms"`
}

func (m *Metrics) observeClaim(claim Claim) {
	m.claims.Add(1)
	if claim.Reclaimed {
		m.reclaims.Add(1)
	}
	lag := max(claim.ClaimedAt-claim.AvailableAt, 0)
	index := len(lagBucketsMillis)
	for i, bound := range lagBucketsMillis {
		if lag <= bound {
			index = i
			break
		}
	}
	m.mu.Lock()
	m.buckets[index]++
	m.count++
	m.sumMS += float64(lag)
	m.maxMS = max(m.maxMS, lag)
	m.mu.Unlock()
}

// Snapshot returns cumulative lag buckets keyed by upper bound ("+Inf" last).
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	buckets := make(map[string]int64, len(m.buckets))
	var cumulative int64
	for i, n := range m.buckets {
		cumulative += n
		label := "+Inf"
		if i < len(lagBucketsMillis) {
			label = strconv.FormatInt(lagBucketsMillis[i], 10)
		}
		buckets[label] = cumulative
	}
	return MetricsSnapshot{
		Claims: m.claims.Load(), Reclaims: m.reclaims.Load(), Handled: m.handled.Load(), HandlerErrors: m.handlerErrors.Load(),
		LeaseLost: m.leaseLost.Load(), Parked: m.parked.Load(),
		LagCount: m.count, LagSumMS: m.sumMS, LagMaxMS: m.maxMS, LagBuckets: buckets,
	}
}
