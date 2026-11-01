package payment

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
)

// unknownAgeBucketsMillis are cumulative bounds for how long a payment stayed
// unknown before a lookup resolved it.
var unknownAgeBucketsMillis = []int64{100, 500, 1000, 5000, 30000, 60000, 300000, 1800000, 3600000, 86400000}

// RecoveryMetrics is an in-process counter set for the lookup worker and the
// startup scan (2.4.9). It holds outcome names only, never payment content.
type RecoveryMetrics struct {
	lookups           atomic.Int64
	lookupUnavailable atomic.Int64
	replays           atomic.Int64
	stillUnknown      atomic.Int64
	staleRecovered    atomic.Int64

	mu          sync.Mutex
	resolutions map[string]int64
	buckets     []int64
	count       int64
	sumMS       float64
}

// NewRecoveryMetrics creates an empty metric set.
func NewRecoveryMetrics() *RecoveryMetrics {
	return &RecoveryMetrics{resolutions: map[string]int64{}, buckets: make([]int64, len(unknownAgeBucketsMillis)+1)}
}

// observeResolution counts a lookup result; ageMS < 0 skips the age histogram.
func (m *RecoveryMetrics) observeResolution(result string, ageMS int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resolutions[result]++
	if ageMS < 0 {
		return
	}
	index := len(unknownAgeBucketsMillis)
	for i, bound := range unknownAgeBucketsMillis {
		if ageMS <= bound {
			index = i
			break
		}
	}
	m.buckets[index]++
	m.count++
	m.sumMS += float64(ageMS)
}

// RecoveryMetricsSnapshot is a point-in-time copy safe to log or serialize.
type RecoveryMetricsSnapshot struct {
	// Lookups counts Lookup calls at the original provider.
	Lookups int64 `json:"lookups_total"`
	// LookupUnavailable counts lookups the provider could not answer.
	LookupUnavailable int64 `json:"lookup_unavailable_total"`
	// Replays counts same-provider, same-key resubmissions (2.4.4).
	Replays int64 `json:"same_key_replays_total"`
	// StillUnknown counts lookup rounds that left the payment unknown.
	StillUnknown int64 `json:"still_unknown_total"`
	// StaleRecovered counts submitted attempts the startup scan moved to unknown.
	StaleRecovered int64 `json:"stale_submitted_recovered_total"`
	// Resolutions counts lookup results: succeeded, declined, late_outcome, already_resolved.
	Resolutions           map[string]int64 `json:"resolutions"`
	UnknownAgeCount       int64            `json:"unknown_age_count"`
	UnknownAgeSumMS       float64          `json:"unknown_age_sum_ms"`
	UnknownAgeBucketsMS   map[string]int64 `json:"unknown_age_buckets_ms"`
	Unresolved            int64            `json:"unresolved_payments"`
	OldestUnresolvedID    *string          `json:"oldest_unresolved_payment_id"`
	OldestUnresolvedAgeMS *int64           `json:"oldest_unresolved_age_ms"`
}

// Snapshot returns the counters with cumulative age buckets ("+Inf" last).
func (m *RecoveryMetrics) Snapshot() RecoveryMetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	resolutions := make(map[string]int64, len(m.resolutions))
	for k, v := range m.resolutions {
		resolutions[k] = v
	}
	buckets := make(map[string]int64, len(m.buckets))
	var cumulative int64
	for i, n := range m.buckets {
		cumulative += n
		label := "+Inf"
		if i < len(unknownAgeBucketsMillis) {
			label = strconv.FormatInt(unknownAgeBucketsMillis[i], 10)
		}
		buckets[label] = cumulative
	}
	return RecoveryMetricsSnapshot{
		Lookups: m.lookups.Load(), LookupUnavailable: m.lookupUnavailable.Load(), Replays: m.replays.Load(),
		StillUnknown: m.stillUnknown.Load(), StaleRecovered: m.staleRecovered.Load(), Resolutions: resolutions,
		UnknownAgeCount: m.count, UnknownAgeSumMS: m.sumMS, UnknownAgeBucketsMS: buckets,
	}
}

// Snapshot adds the database-derived gauges to the counters: the number of
// payments currently unknown and the oldest of them (by when it last became
// unknown), across all workspaces.
func (r *Recovery) Snapshot(ctx context.Context) (RecoveryMetricsSnapshot, error) {
	snapshot := r.opts.Metrics.Snapshot()
	db := r.db.WithContext(ctx)
	if err := db.Raw("SELECT count(*) FROM payments WHERE status = ?", string(StatusUnknown)).Scan(&snapshot.Unresolved).Error; err != nil {
		return snapshot, err
	}
	var rows []struct {
		ID    string
		Since int64
	}
	err := db.Raw(`SELECT p.id AS id, max(t.created_at) AS since FROM payments p
JOIN payment_state_transitions t ON t.payment_id = p.id AND t.workspace_id = p.workspace_id AND t.to_status = ?
WHERE p.status = ? GROUP BY p.id ORDER BY since, p.id LIMIT 1`, string(StatusUnknown), string(StatusUnknown)).Scan(&rows).Error
	if err != nil {
		return snapshot, err
	}
	if len(rows) > 0 {
		id, age := rows[0].ID, max(r.opts.Now().UnixMilli()-rows[0].Since, 0)
		snapshot.OldestUnresolvedID, snapshot.OldestUnresolvedAgeMS = &id, &age
	}
	return snapshot, nil
}
