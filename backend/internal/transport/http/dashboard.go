package httpapi

import (
	"errors"
	"math/big"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// Dashboard read models (WBS 3.1). Every query is scoped to the principal's
// workspace, money is aggregated with big.Int per currency (never summed
// across currencies) and returned as decimal strings, and statistics without
// observations are null rather than invented.

type metricWindow struct {
	name     string
	duration time.Duration
	bucket   time.Duration
}

var metricWindows = map[string]metricWindow{
	"24h": {"24h", 24 * time.Hour, time.Hour},
	"7d":  {"7d", 7 * 24 * time.Hour, 24 * time.Hour},
	"30d": {"30d", 30 * 24 * time.Hour, 24 * time.Hour},
}

type currencyVolume struct {
	Currency     string `json:"currency"`
	AmountMinor  string `json:"amount_minor"`
	PaymentCount int64  `json:"payment_count"`
}

type volumePoint struct {
	Timestamp    int64  `json:"timestamp"`
	Currency     string `json:"currency"`
	AmountMinor  string `json:"amount_minor"`
	PaymentCount int64  `json:"payment_count"`
}

type paymentRow struct {
	Currency    string
	AmountMinor int64
	Status      string
	CreatedAt   int64
	UpdatedAt   int64
}

// serveReadModels handles dashboard metrics and payment history. It reports
// false for paths it does not own so the caller continues routing.
func (h *handler) serveReadModels(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch {
	case r.URL.Path == "/v1/dashboard/summary":
		h.dashboardSummary(w, r, principal, id)
	case r.URL.Path == "/v1/dashboard/payment-volume":
		h.paymentVolume(w, r, principal, id)
	case strings.HasPrefix(r.URL.Path, "/v1/payments/") && strings.HasSuffix(r.URL.Path, "/history"):
		paymentID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/payments/"), "/history")
		if paymentID == "" || strings.Contains(paymentID, "/") {
			return false
		}
		h.paymentHistory(w, r, principal, id, paymentID)
	default:
		return false
	}
	return true
}

func parseWindow(w http.ResponseWriter, r *http.Request, id string) (metricWindow, time.Time, bool) {
	name := r.URL.Query().Get("window")
	if name == "" {
		name = "24h"
	}
	window, ok := metricWindows[name]
	if !ok {
		fail(w, 400, "validation_error", "Invalid metric window", id, FieldError{Field: "window", Message: "Use 24h, 7d or 30d"})
		return metricWindow{}, time.Time{}, false
	}
	return window, time.Now().UTC(), true
}

// windowPayments streams the workspace's payments created in [start, end].
func (h *handler) windowPayments(r *http.Request, workspaceID string, start, end time.Time, visit func(paymentRow)) error {
	rows, err := h.db.WithContext(r.Context()).Model(&storage.Payment{}).
		Select("currency, amount_minor, status, created_at, updated_at").
		Where("workspace_id = ? AND created_at >= ? AND created_at <= ?", workspaceID, start.UnixMilli(), end.UnixMilli()).
		Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row paymentRow
		if err := rows.Scan(&row.Currency, &row.AmountMinor, &row.Status, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return err
		}
		visit(row)
	}
	return rows.Err()
}

type volumeAccumulator map[string]*struct {
	amount big.Int
	count  int64
}

func (v volumeAccumulator) add(key string, amount int64) {
	entry, ok := v[key]
	if !ok {
		entry = &struct {
			amount big.Int
			count  int64
		}{}
		v[key] = entry
	}
	entry.amount.Add(&entry.amount, big.NewInt(amount))
	entry.count++
}

func (v volumeAccumulator) list() []currencyVolume {
	items := make([]currencyVolume, 0, len(v))
	for currency, entry := range v {
		items = append(items, currencyVolume{Currency: currency, AmountMinor: entry.amount.String(), PaymentCount: entry.count})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Currency < items[j].Currency })
	return items
}

// percentile returns the nearest-rank percentile of sorted values.
func percentile(sorted []int64, p int) int64 {
	rank := (p*len(sorted) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return sorted[rank-1]
}

// percentOneDecimal renders numerator/denominator*100 rounded half-up to one
// decimal place with integer arithmetic.
func percentOneDecimal(numerator, denominator int64) string {
	tenths := (numerator*1000*2 + denominator) / (denominator * 2)
	return strconv.FormatInt(tenths/10, 10) + "." + strconv.FormatInt(tenths%10, 10)
}

func (h *handler) dashboardSummary(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	window, now, ok := parseWindow(w, r, id)
	if !ok {
		return
	}
	start := now.Add(-window.duration)
	accepted, succeeded := volumeAccumulator{}, volumeAccumulator{}
	statusCounts := map[string]int64{}
	for _, status := range []payment.Status{payment.StatusCreated, payment.StatusProcessing, payment.StatusSucceeded, payment.StatusFailed, payment.StatusCanceled, payment.StatusUnknown} {
		statusCounts[string(status)] = 0
	}
	var count int64
	latencies := []int64{}
	err := h.windowPayments(r, principal.WorkspaceID, start, now, func(row paymentRow) {
		count++
		statusCounts[row.Status]++
		accepted.add(row.Currency, row.AmountMinor)
		if row.Status == string(payment.StatusSucceeded) {
			succeeded.add(row.Currency, row.AmountMinor)
		}
		if row.Status == string(payment.StatusSucceeded) || row.Status == string(payment.StatusFailed) {
			latencies = append(latencies, row.UpdatedAt-row.CreatedAt)
		}
	})
	if err != nil {
		fail(w, 503, "unavailable", "Dashboard metrics unavailable", id)
		return
	}
	terminal := statusCounts[string(payment.StatusSucceeded)] + statusCounts[string(payment.StatusFailed)]
	var successRate *string
	var p95 *int64
	if terminal > 0 {
		rate := percentOneDecimal(statusCounts[string(payment.StatusSucceeded)], terminal)
		successRate = &rate
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		value := percentile(latencies, 95)
		p95 = &value
	}
	writeJSON(w, 200, map[string]any{
		"data_source":                "observed",
		"window":                     window.name,
		"window_start":               payment.FormatTimestamp(start.UnixMilli()),
		"window_end":                 payment.FormatTimestamp(now.UnixMilli()),
		"generated_at":               payment.FormatTimestamp(now.UnixMilli()),
		"payment_count":              count,
		"status_counts":              statusCounts,
		"volumes":                    accepted.list(),
		"succeeded_volumes":          succeeded.list(),
		"terminal_count":             terminal,
		"success_rate_percent":       successRate,
		"completion_latency_p95_ms":  p95,
		"completion_latency_samples": int64(len(latencies)),
	})
}

func (h *handler) paymentVolume(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	window, now, ok := parseWindow(w, r, id)
	if !ok {
		return
	}
	start := now.Add(-window.duration).Truncate(window.bucket)
	buckets := volumeAccumulator{}
	err := h.windowPayments(r, principal.WorkspaceID, start, now, func(row paymentRow) {
		bucket := time.UnixMilli(row.CreatedAt).UTC().Truncate(window.bucket).UnixMilli()
		buckets.add(strconv.FormatInt(bucket, 10)+"|"+row.Currency, row.AmountMinor)
	})
	if err != nil {
		fail(w, 503, "unavailable", "Payment volume unavailable", id)
		return
	}
	items := make([]volumePoint, 0, len(buckets))
	for key, entry := range buckets {
		at, currency, _ := strings.Cut(key, "|")
		timestamp, _ := strconv.ParseInt(at, 10, 64)
		items = append(items, volumePoint{Timestamp: timestamp, Currency: currency, AmountMinor: entry.amount.String(), PaymentCount: entry.count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Timestamp != items[j].Timestamp {
			return items[i].Timestamp < items[j].Timestamp
		}
		return items[i].Currency < items[j].Currency
	})
	bucket := "hour"
	if window.bucket == 24*time.Hour {
		bucket = "day"
	}
	writeJSON(w, 200, map[string]any{
		"data_source":  "observed",
		"window":       window.name,
		"bucket":       bucket,
		"window_start": payment.FormatTimestamp(start.UnixMilli()),
		"window_end":   payment.FormatTimestamp(now.UnixMilli()),
		"generated_at": payment.FormatTimestamp(now.UnixMilli()),
		"items":        items,
		"next_cursor":  nil,
	})
}

// maskKey reveals at most the last four characters of an idempotency key.
func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

func (h *handler) paymentHistory(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, paymentID string) {
	db := h.db.WithContext(r.Context())
	if _, err := storage.GetPayment(db, principal.WorkspaceID, paymentID); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			fail(w, 404, "not_found", "Payment not found", id)
		} else {
			fail(w, 503, "unavailable", "Payment history unavailable", id)
		}
		return
	}
	transitions, err := storage.ListStateTransitions(db, principal.WorkspaceID, paymentID)
	if err != nil {
		fail(w, 503, "unavailable", "Payment history unavailable", id)
		return
	}
	type transitionView struct {
		FromStatus     *string `json:"from_status"`
		ToStatus       string  `json:"to_status"`
		Reason         string  `json:"reason"`
		ActorReference string  `json:"actor_reference"`
		CreatedAt      string  `json:"created_at"`
	}
	history := make([]transitionView, 0, len(transitions))
	for _, t := range transitions {
		history = append(history, transitionView{FromStatus: t.FromStatus, ToStatus: t.ToStatus, Reason: t.Reason, ActorReference: t.ActorReference, CreatedAt: payment.FormatTimestamp(t.CreatedAt)})
	}
	var masked *string
	var record storage.IdempotencyRecord
	if err := db.Where("workspace_id = ? AND payment_id = ?", principal.WorkspaceID, paymentID).Order("created_at").Take(&record).Error; err == nil {
		value := maskKey(record.Key)
		masked = &value
	}
	var outbox []storage.OutboxEvent
	if err := db.Where("workspace_id = ? AND payment_id = ?", principal.WorkspaceID, paymentID).Order("created_at, id").Find(&outbox).Error; err != nil {
		fail(w, 503, "unavailable", "Payment history unavailable", id)
		return
	}
	type eventView struct {
		ID          string  `json:"id"`
		EventType   string  `json:"event_type"`
		Status      string  `json:"status"`
		Attempts    int64   `json:"attempts"`
		CreatedAt   string  `json:"created_at"`
		DeliveredAt *string `json:"delivered_at"`
	}
	events := make([]eventView, 0, len(outbox))
	for _, e := range outbox {
		var delivered *string
		if e.DeliveredAt != nil {
			value := payment.FormatTimestamp(*e.DeliveredAt)
			delivered = &value
		}
		events = append(events, eventView{ID: e.ID, EventType: e.EventType, Status: e.Status, Attempts: e.Attempts, CreatedAt: payment.FormatTimestamp(e.CreatedAt), DeliveredAt: delivered})
	}
	writeJSON(w, 200, map[string]any{
		"payment_id":             paymentID,
		"idempotency_key_masked": masked,
		"transitions":            history,
		"events":                 events,
	})
}
