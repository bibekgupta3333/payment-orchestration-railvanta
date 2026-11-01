package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/reconciliation"
	"github.com/railvanta/railvanta/backend/internal/storage"
)

// Reconciliation routes (WBS 4.2). Reads are open to any authenticated
// principal; POST routes are operator-only (ServeHTTP rejects non-operators and
// bad CSRF for every write before routing, and the service checks the role
// again). Resolution records evidence only.

// ReconciliationCounts are the run totals.
type ReconciliationCounts struct {
	Compared  int64 `json:"compared"`
	Matched   int64 `json:"matched"`
	Unmatched int64 `json:"unmatched"`
	Mismatch  int64 `json:"mismatch"`
}

// ReconciliationRunView is one run.
type ReconciliationRunView struct {
	ID                    string               `json:"id"`
	RunKey                string               `json:"run_key"`
	Status                string               `json:"status"`
	WindowStart           string               `json:"window_start"`
	WindowEnd             string               `json:"window_end"`
	CreatedAt             string               `json:"created_at"`
	CompletedAt           *string              `json:"completed_at"`
	RequestedBy           *string              `json:"requested_by"`
	Counts                ReconciliationCounts `json:"counts"`
	EvidenceConsumedCount int64                `json:"evidence_consumed_count"`
	Simulated             bool                 `json:"simulated"`
}

// ResolutionEventView is one append-only history row.
type ResolutionEventView struct {
	ID         string `json:"id"`
	FromStatus string `json:"from_status"`
	ToStatus   string `json:"to_status"`
	Note       string `json:"note"`
	Actor      string `json:"actor"`
	CreatedAt  string `json:"created_at"`
}

// DiscrepancyView is one discrepancy with evidence and history.
type DiscrepancyView struct {
	ID               string                `json:"id"`
	RunID            string                `json:"run_id"`
	PaymentID        *string               `json:"payment_id"`
	Kind             string                `json:"kind"`
	Status           string                `json:"status"`
	Details          json.RawMessage       `json:"details"`
	ResolutionNote   *string               `json:"resolution_note"`
	ResolvedBy       *string               `json:"resolved_by"`
	CreatedAt        string                `json:"created_at"`
	ResolvedAt       *string               `json:"resolved_at"`
	RelatedJournalID *string               `json:"related_journal_id"`
	History          []ResolutionEventView `json:"history"`
	Simulated        bool                  `json:"simulated"`
}

func newRunView(run storage.ReconciliationRun) ReconciliationRunView {
	view := ReconciliationRunView{
		ID: run.ID, RunKey: run.RunKey, Status: run.Status,
		WindowStart: payment.FormatTimestamp(run.WindowStart), WindowEnd: payment.FormatTimestamp(run.WindowEnd),
		CreatedAt: payment.FormatTimestamp(run.CreatedAt), RequestedBy: run.RequestedBy,
		Counts: ReconciliationCounts{
			Compared: run.ComparedCount, Matched: run.MatchedCount, Unmatched: run.UnmatchedCount, Mismatch: run.MismatchCount,
		},
		EvidenceConsumedCount: run.EvidenceConsumedCount, Simulated: true,
	}
	if run.CompletedAt != nil {
		at := payment.FormatTimestamp(*run.CompletedAt)
		view.CompletedAt = &at
	}
	return view
}

func newDiscrepancyView(d reconciliation.DiscrepancyView) DiscrepancyView {
	view := DiscrepancyView{
		ID: d.ID, RunID: d.RunID, PaymentID: d.PaymentID, Kind: d.Kind, Status: d.Status,
		Details: json.RawMessage(d.Details), ResolutionNote: d.ResolutionNote, ResolvedBy: d.ResolvedBy,
		CreatedAt: payment.FormatTimestamp(d.CreatedAt), RelatedJournalID: d.RelatedJournalID,
		History: make([]ResolutionEventView, 0, len(d.History)), Simulated: true,
	}
	if d.ResolvedAt != nil {
		at := payment.FormatTimestamp(*d.ResolvedAt)
		view.ResolvedAt = &at
	}
	for _, e := range d.History {
		view.History = append(view.History, ResolutionEventView{
			ID: e.ID, FromStatus: e.FromStatus, ToStatus: e.ToStatus, Note: e.Note, Actor: e.ActorReference,
			CreatedAt: payment.FormatTimestamp(e.CreatedAt),
		})
	}
	return view
}

func (h *handler) serveReconciliation(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) bool {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/reconciliation")
	if rest == r.URL.Path {
		return false
	}
	switch {
	case rest == "/runs" && r.Method == http.MethodGet:
		h.listReconciliationRuns(w, r, principal, id)
	case rest == "/runs" && r.Method == http.MethodPost:
		h.startReconciliationRun(w, r, principal, id)
	case rest == "/discrepancies" && r.Method == http.MethodGet:
		h.listDiscrepancies(w, r, principal, id)
	case strings.HasPrefix(rest, "/discrepancies/") && r.Method == http.MethodGet:
		discrepancyID := strings.TrimPrefix(rest, "/discrepancies/")
		if discrepancyID == "" || strings.Contains(discrepancyID, "/") {
			return false
		}
		h.getDiscrepancy(w, r, principal, id, discrepancyID)
	case strings.HasPrefix(rest, "/discrepancies/") && strings.HasSuffix(rest, "/resolve") && r.Method == http.MethodPost:
		discrepancyID := strings.TrimSuffix(strings.TrimPrefix(rest, "/discrepancies/"), "/resolve")
		if discrepancyID == "" || strings.Contains(discrepancyID, "/") {
			return false
		}
		h.resolveDiscrepancy(w, r, principal, id, discrepancyID)
	default:
		return false
	}
	return true
}

func parsePage(r *http.Request, fields *[]FieldError) (int, *reconciliation.Cursor) {
	query := r.URL.Query()
	limit := defaultPageSize
	if value := query.Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > maxPageSize {
			*fields = append(*fields, FieldError{Field: "limit", Message: "Use an integer from 1 to 100"})
		} else {
			limit = n
		}
	}
	var after *reconciliation.Cursor
	if value := query.Get("cursor"); value != "" {
		cursor, ok := decodeCursor(value, false)
		if !ok {
			*fields = append(*fields, FieldError{Field: "cursor", Message: "Use next_cursor from a previous page"})
		} else {
			after = &reconciliation.Cursor{CreatedAt: cursor.CreatedAt, ID: cursor.ID}
		}
	}
	return limit, after
}

func (h *handler) listReconciliationRuns(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	var fields []FieldError
	limit, after := parsePage(r, &fields)
	if len(fields) > 0 {
		fail(w, 400, "validation_error", "Invalid list parameters", id, fields...)
		return
	}
	runs, err := reconciliation.ListRuns(h.db.WithContext(r.Context()), principal.WorkspaceID, reconciliation.RunFilter{After: after, Limit: limit + 1})
	if err != nil {
		fail(w, 503, "unavailable", "Reconciliation unavailable", id)
		return
	}
	var next *string
	if len(runs) > limit {
		runs = runs[:limit]
		cursor := encodeCursor(storage.PaymentCursor{CreatedAt: runs[len(runs)-1].CreatedAt, ID: runs[len(runs)-1].ID}, false)
		next = &cursor
	}
	items := make([]ReconciliationRunView, 0, len(runs))
	for _, run := range runs {
		items = append(items, newRunView(run))
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
}

var (
	discrepancyKinds    = map[string]bool{"missing_posting": true, "unknown_outcome": true, "amount_mismatch": true, "status_mismatch": true, "missing_at_provider": true}
	discrepancyStatuses = map[string]bool{"open": true, "investigating": true, "resolved": true}
)

func (h *handler) listDiscrepancies(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	var fields []FieldError
	limit, after := parsePage(r, &fields)
	query := r.URL.Query()
	filter := reconciliation.DiscrepancyFilter{RunID: query.Get("run_id"), Kind: query.Get("kind"), Status: query.Get("status"), After: after}
	if filter.RunID != "" && !safeID.MatchString(filter.RunID) {
		fields = append(fields, FieldError{Field: "run_id", Message: "Use a run ID"})
	}
	if filter.Kind != "" && !discrepancyKinds[filter.Kind] {
		fields = append(fields, FieldError{Field: "kind", Message: "Use a known discrepancy kind"})
	}
	if filter.Status != "" && !discrepancyStatuses[filter.Status] {
		fields = append(fields, FieldError{Field: "status", Message: "Use open, investigating or resolved"})
	}
	if len(fields) > 0 {
		fail(w, 400, "validation_error", "Invalid list parameters", id, fields...)
		return
	}
	filter.Limit = limit + 1
	rows, err := reconciliation.ListDiscrepancies(h.db.WithContext(r.Context()), principal.WorkspaceID, filter)
	if err != nil {
		fail(w, 503, "unavailable", "Reconciliation unavailable", id)
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		cursor := encodeCursor(storage.PaymentCursor{CreatedAt: rows[len(rows)-1].CreatedAt, ID: rows[len(rows)-1].ID}, false)
		next = &cursor
	}
	items := make([]DiscrepancyView, 0, len(rows))
	for _, row := range rows {
		items = append(items, newDiscrepancyView(row))
	}
	writeJSON(w, 200, map[string]any{"items": items, "next_cursor": next})
}

func (h *handler) getDiscrepancy(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, discrepancyID string) {
	row, err := reconciliation.GetDiscrepancy(h.db.WithContext(r.Context()), principal.WorkspaceID, discrepancyID)
	if errors.Is(err, reconciliation.ErrNotFound) || len(discrepancyID) > 64 {
		fail(w, 404, "not_found", "Discrepancy not found", id)
		return
	}
	if err != nil {
		fail(w, 503, "unavailable", "Reconciliation unavailable", id)
		return
	}
	writeJSON(w, 200, newDiscrepancyView(row))
}

type runRequest struct {
	WindowStart string `json:"window_start"`
	WindowEnd   string `json:"window_end"`
}

// startReconciliationRun serves POST /v1/reconciliation/runs. The
// Idempotency-Key header is the run key: repeating it with the same window
// returns the stored run (200) and re-checks the window, adding only
// discrepancies not already recorded; a different window is 409. The run
// executes synchronously and only reads payments, journals and charges.
func (h *handler) startReconciliationRun(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	if principal.Role != "operator" {
		fail(w, 403, "forbidden", "Operator permission is required", id)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	var fields []FieldError
	if len(key) < 1 || len(key) > 255 {
		fields = append(fields, FieldError{Field: "Idempotency-Key", Message: "Send an Idempotency-Key of 1 to 255 characters"})
	}
	var body runRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		fail(w, 400, "validation_error", "Invalid request body", id, FieldError{Field: "body", Message: "Send JSON with window_start and window_end"})
		return
	}
	start, startErr := time.Parse(time.RFC3339, body.WindowStart)
	end, endErr := time.Parse(time.RFC3339, body.WindowEnd)
	if startErr != nil {
		fields = append(fields, FieldError{Field: "window_start", Message: "Use an RFC 3339 timestamp"})
	}
	if endErr != nil {
		fields = append(fields, FieldError{Field: "window_end", Message: "Use an RFC 3339 timestamp"})
	}
	if startErr == nil && endErr == nil && !end.After(start) {
		fields = append(fields, FieldError{Field: "window_end", Message: "window_end must be after window_start"})
	}
	if len(fields) > 0 {
		fail(w, 400, "validation_error", "Invalid reconciliation run", id, fields...)
		return
	}
	result, err := reconciliation.NewService(h.db, nil).Run(r.Context(), reconciliation.RunInput{
		WorkspaceID: principal.WorkspaceID, RunKey: key, WindowStart: start.UnixMilli(), WindowEnd: end.UnixMilli(),
		RequestedBy: actorReference(principal),
	})
	switch {
	case errors.Is(err, reconciliation.ErrInvalidWindow):
		fail(w, 400, "validation_error", "Invalid reconciliation run", id, FieldError{Field: "window_end", Message: "The window must be positive and at most 366 days"})
		return
	case errors.Is(err, reconciliation.ErrRunKeyConflict):
		fail(w, 409, "idempotency_conflict", "This Idempotency-Key was used with a different window", id)
		return
	case err != nil:
		fail(w, 503, "unavailable", "Reconciliation unavailable", id)
		return
	}
	status := 200
	if result.Created {
		status = 201
	}
	writeJSON(w, status, newRunView(result.Run))
}

type resolveRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

// resolveDiscrepancy serves POST /v1/reconciliation/discrepancies/{id}/resolve.
// It changes the discrepancy's status and appends history; it never changes a
// payment or a journal.
func (h *handler) resolveDiscrepancy(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, discrepancyID string) {
	var body resolveRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		fail(w, 400, "validation_error", "Invalid request body", id, FieldError{Field: "body", Message: "Send JSON with status and note"})
		return
	}
	row, err := reconciliation.NewService(h.db, nil).Resolve(r.Context(), reconciliation.Resolution{
		WorkspaceID: principal.WorkspaceID, Role: principal.Role, ID: discrepancyID,
		ToStatus: body.Status, Note: body.Note, Actor: actorReference(principal),
	})
	switch {
	case errors.Is(err, reconciliation.ErrNotOperator):
		fail(w, 403, "forbidden", "Operator permission is required", id)
	case errors.Is(err, reconciliation.ErrNotFound):
		fail(w, 404, "not_found", "Discrepancy not found", id)
	case errors.Is(err, reconciliation.ErrInvalidResolution):
		fail(w, 400, "validation_error", "Invalid resolution", id,
			FieldError{Field: "status", Message: "Use open, investigating or resolved"},
			FieldError{Field: "note", Message: "A note of 1 to 2000 characters is required"})
	case errors.Is(err, reconciliation.ErrInvalidTransition):
		fail(w, 409, "invalid_transition", "This status change is not allowed", id)
	case err != nil:
		fail(w, 503, "unavailable", "Reconciliation unavailable", id)
	default:
		view, err := reconciliation.GetDiscrepancy(h.db.WithContext(r.Context()), principal.WorkspaceID, row.ID)
		if err != nil {
			fail(w, 503, "unavailable", "Reconciliation unavailable", id)
			return
		}
		writeJSON(w, 200, newDiscrepancyView(view))
	}
}
