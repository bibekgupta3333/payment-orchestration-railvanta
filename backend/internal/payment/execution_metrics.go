package payment

import (
	"strconv"
	"sync"
	"sync/atomic"
)

// attemptLatencyBucketsMillis are cumulative bounds for completed_at - submitted_at.
var attemptLatencyBucketsMillis = []int64{10, 50, 100, 250, 500, 1000, 2500, 5000, 10000, 30000}

// ExecutionMetrics is an in-process counter set for dispatch execution. It
// holds provider IDs and outcome names only, never payment content.
type ExecutionMetrics struct {
	attemptsStarted atomic.Int64
	unknownOutcomes atomic.Int64
	staleReclaims   atomic.Int64
	noRoute         atomic.Int64
	failoverBlocked atomic.Int64
	lateOutcomes    atomic.Int64

	mu       sync.Mutex
	outcomes map[string]map[string]int64 // provider ID -> outcome -> count
	buckets  []int64
	count    int64
	sumMS    float64
}

// NewExecutionMetrics creates an empty metric set.
func NewExecutionMetrics() *ExecutionMetrics {
	return &ExecutionMetrics{outcomes: map[string]map[string]int64{}, buckets: make([]int64, len(attemptLatencyBucketsMillis)+1)}
}

// ExecutionMetricsSnapshot is a point-in-time copy safe to log or serialize.
type ExecutionMetricsSnapshot struct {
	AttemptsStarted int64 `json:"attempts_started_total"`
	// UnknownOutcomes counts attempts moved to unknown (ambiguous or stale).
	UnknownOutcomes       int64                       `json:"unknown_outcomes_total"`
	StaleReclaims         int64                       `json:"stale_submitted_reclaims_total"`
	NoRoute               int64                       `json:"no_eligible_provider_total"`
	FailoverBlocked       int64                       `json:"failover_blocked_total"`
	LateOutcomes          int64                       `json:"late_outcomes_total"`
	OutcomesByProvider    map[string]map[string]int64 `json:"outcomes_by_provider"`
	AttemptLatencyCount   int64                       `json:"attempt_latency_count"`
	AttemptLatencySumMS   float64                     `json:"attempt_latency_sum_ms"`
	AttemptLatencyBuckets map[string]int64            `json:"attempt_latency_buckets_ms"`
}

func (m *ExecutionMetrics) observeOutcome(providerID, outcome string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.outcomes[providerID] == nil {
		m.outcomes[providerID] = map[string]int64{}
	}
	m.outcomes[providerID][outcome]++
}

// observeAttemptLatency records completed_at - submitted_at in milliseconds.
func (m *ExecutionMetrics) observeAttemptLatency(millis int64) {
	millis = max(millis, 0)
	index := len(attemptLatencyBucketsMillis)
	for i, bound := range attemptLatencyBucketsMillis {
		if millis <= bound {
			index = i
			break
		}
	}
	m.mu.Lock()
	m.buckets[index]++
	m.count++
	m.sumMS += float64(millis)
	m.mu.Unlock()
}

// Snapshot returns cumulative latency buckets keyed by upper bound ("+Inf" last).
func (m *ExecutionMetrics) Snapshot() ExecutionMetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	outcomes := make(map[string]map[string]int64, len(m.outcomes))
	for providerID, byOutcome := range m.outcomes {
		outcomes[providerID] = make(map[string]int64, len(byOutcome))
		for outcome, n := range byOutcome {
			outcomes[providerID][outcome] = n
		}
	}
	buckets := make(map[string]int64, len(m.buckets))
	var cumulative int64
	for i, n := range m.buckets {
		cumulative += n
		label := "+Inf"
		if i < len(attemptLatencyBucketsMillis) {
			label = strconv.FormatInt(attemptLatencyBucketsMillis[i], 10)
		}
		buckets[label] = cumulative
	}
	return ExecutionMetricsSnapshot{
		AttemptsStarted: m.attemptsStarted.Load(), UnknownOutcomes: m.unknownOutcomes.Load(), StaleReclaims: m.staleReclaims.Load(),
		NoRoute: m.noRoute.Load(), FailoverBlocked: m.failoverBlocked.Load(), LateOutcomes: m.lateOutcomes.Load(),
		OutcomesByProvider: outcomes, AttemptLatencyCount: m.count, AttemptLatencySumMS: m.sumMS, AttemptLatencyBuckets: buckets,
	}
}
