package provider

import (
	"testing"
	"time"
)

func TestSummarizeReportsNoObservationsInsteadOfZeros(t *testing.T) {
	now := time.UnixMilli(10_000_000)
	w := Summarize("24h", 24*time.Hour, now, nil)
	if w.Observations != 0 || w.SuccessRatePercent != nil || w.P95Millis != nil || Verdict(w) != HealthNoObservations {
		t.Fatalf("%+v", w)
	}
	// In-flight attempts and attempts outside the window are not observations.
	w = Summarize("1h", time.Hour, now, []Observation{
		{SubmittedAt: now.UnixMilli() - 1000, Status: "submitted"},
		{SubmittedAt: now.UnixMilli() - 2*3600*1000, Status: "succeeded"},
	})
	if w.Observations != 0 {
		t.Fatalf("%+v", w)
	}
}

func TestSummarizeRatesLatencyAndErrors(t *testing.T) {
	now := time.UnixMilli(10_000_000)
	at := func(ms int64) *int64 { return &ms }
	var obs []Observation
	for i := int64(1); i <= 19; i++ {
		obs = append(obs, Observation{SubmittedAt: now.UnixMilli() - 5000, CompletedAt: at(now.UnixMilli() - 5000 + i*10), Status: "succeeded"})
	}
	obs = append(obs, Observation{SubmittedAt: now.UnixMilli() - 4000, Status: "unknown", ErrorCode: "timeout"})
	w := Summarize("24h", 24*time.Hour, now, obs)
	if w.Observations != 20 || w.Succeeded != 19 || w.Unknown != 1 || w.ErrorCounts["timeout"] != 1 {
		t.Fatalf("%+v", w)
	}
	if w.SuccessRatePercent == nil || *w.SuccessRatePercent != 95 || Verdict(w) != HealthHealthy {
		t.Fatalf("%v %s", w.SuccessRatePercent, Verdict(w))
	}
	if w.P95Millis == nil || *w.P95Millis != 190 || w.LatencyCount != 19 {
		t.Fatalf("p95 %v", w.P95Millis)
	}
	if got := Verdict(Summarize("24h", 24*time.Hour, now, obs[:3])); got != HealthInsufficient {
		t.Fatalf("small sample verdict %s", got)
	}
}
