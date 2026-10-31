package payment

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// latencyBucketsMillis are cumulative upper bounds for acceptance latency.
var latencyBucketsMillis = []int64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}

// Metrics is an in-process counter set for the acceptance path. It holds no
// request content. A future exporter can publish Snapshot without changes here.
type Metrics struct {
	accepted        atomic.Int64
	replayed        atomic.Int64
	conflicts       atomic.Int64
	busyErrors      atomic.Int64
	busyExhausted   atomic.Int64
	reservationRace atomic.Int64

	mu      sync.Mutex
	buckets []int64 // one per latencyBucketsMillis plus +Inf
	count   int64
	sumMS   float64
}

// NewMetrics creates an empty metric set.
func NewMetrics() *Metrics {
	return &Metrics{buckets: make([]int64, len(latencyBucketsMillis)+1)}
}

// MetricsSnapshot is a point-in-time copy safe to log or serialize.
type MetricsSnapshot struct {
	Accepted                 int64            `json:"accepted_total"`
	Replayed                 int64            `json:"replayed_total"`
	Conflicts                int64            `json:"idempotency_conflicts_total"`
	BusyErrors               int64            `json:"sqlite_busy_errors_total"`
	BusyRetriesExhausted     int64            `json:"sqlite_busy_retries_exhausted_total"`
	ReservationRaces         int64            `json:"reservation_races_total"`
	AcceptanceLatencyCount   int64            `json:"acceptance_latency_count"`
	AcceptanceLatencySumMS   float64          `json:"acceptance_latency_sum_ms"`
	AcceptanceLatencyBuckets map[string]int64 `json:"acceptance_latency_buckets_ms"`
}

func (m *Metrics) observeLatency(duration time.Duration) {
	millis := float64(duration) / float64(time.Millisecond)
	index := len(latencyBucketsMillis)
	for i, bound := range latencyBucketsMillis {
		if millis <= float64(bound) {
			index = i
			break
		}
	}
	m.mu.Lock()
	m.buckets[index]++
	m.count++
	m.sumMS += millis
	m.mu.Unlock()
}

// Snapshot returns cumulative histogram buckets keyed by upper bound ("+Inf" last).
func (m *Metrics) Snapshot() MetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	buckets := make(map[string]int64, len(m.buckets))
	var cumulative int64
	for i, n := range m.buckets {
		cumulative += n
		label := "+Inf"
		if i < len(latencyBucketsMillis) {
			label = strconv.FormatInt(latencyBucketsMillis[i], 10)
		}
		buckets[label] = cumulative
	}
	return MetricsSnapshot{
		Accepted: m.accepted.Load(), Replayed: m.replayed.Load(), Conflicts: m.conflicts.Load(),
		BusyErrors: m.busyErrors.Load(), BusyRetriesExhausted: m.busyExhausted.Load(), ReservationRaces: m.reservationRace.Load(),
		AcceptanceLatencyCount: m.count, AcceptanceLatencySumMS: m.sumMS, AcceptanceLatencyBuckets: buckets,
	}
}
