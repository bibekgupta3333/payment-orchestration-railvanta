package provider

import (
	"sort"
	"time"
)

// Observation is one provider attempt reduced to what health needs. It carries
// no payment-domain types so this package stays independent of them.
type Observation struct {
	// SubmittedAt is when the attempt was sent to the provider (Unix ms).
	SubmittedAt int64
	// CompletedAt is when a definitive result was recorded (Unix ms), or nil.
	CompletedAt *int64
	// Status is the attempt status: succeeded, failed or unknown count; created
	// and submitted attempts are still in flight and are not observations.
	Status    string
	ErrorCode string
}

// Health status values. NoObservations is reported instead of any rate.
const (
	HealthNoObservations = "no_observations"
	HealthInsufficient   = "insufficient_data"
	HealthHealthy        = "healthy"
	HealthDegraded       = "degraded"
	HealthUnhealthy      = "unhealthy"
)

// MinObservations is the sample size below which no verdict is given.
const MinObservations = 5

// WindowHealth summarizes observations inside one window.
type WindowHealth struct {
	Window       string
	Observations int64
	Succeeded    int64
	Failed       int64
	Unknown      int64
	// SuccessRatePercent is nil when there are no observations.
	SuccessRatePercent *float64
	// P95Millis is nil when no observation has both timestamps.
	P95Millis    *int64
	LatencyCount int64
	// ErrorCounts counts failed and unknown attempts by error code.
	ErrorCounts map[string]int64
}

// Summarize computes one window ending at now. Only attempts with a definitive
// or unknown status submitted inside the window are observations.
func Summarize(window string, length time.Duration, now time.Time, observations []Observation) WindowHealth {
	start := now.Add(-length).UnixMilli()
	w := WindowHealth{Window: window, ErrorCounts: map[string]int64{}}
	var latencies []int64
	for _, o := range observations {
		if o.SubmittedAt < start || o.SubmittedAt > now.UnixMilli() {
			continue
		}
		switch o.Status {
		case "succeeded":
			w.Succeeded++
		case "failed":
			w.Failed++
		case "unknown":
			w.Unknown++
		default:
			continue
		}
		w.Observations++
		if o.Status != "succeeded" {
			code := o.ErrorCode
			if code == "" {
				code = "unspecified"
			}
			w.ErrorCounts[code]++
		}
		if o.CompletedAt != nil && *o.CompletedAt >= o.SubmittedAt {
			latencies = append(latencies, *o.CompletedAt-o.SubmittedAt)
		}
	}
	if w.Observations > 0 {
		rate := float64(w.Succeeded) * 100 / float64(w.Observations)
		w.SuccessRatePercent = &rate
	}
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		index := (95*len(latencies) + 99) / 100
		if index < 1 {
			index = 1
		}
		value := latencies[index-1]
		w.P95Millis = &value
		w.LatencyCount = int64(len(latencies))
	}
	return w
}

// Verdict turns a window into a status. Without enough evidence it never
// reports healthy.
func Verdict(w WindowHealth) string {
	switch {
	case w.Observations == 0:
		return HealthNoObservations
	case w.Observations < MinObservations || w.SuccessRatePercent == nil:
		return HealthInsufficient
	case *w.SuccessRatePercent >= 95:
		return HealthHealthy
	case *w.SuccessRatePercent >= 80:
		return HealthDegraded
	default:
		return HealthUnhealthy
	}
}
