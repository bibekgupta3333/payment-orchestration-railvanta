package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
	"github.com/railvanta/railvanta/backend/internal/storage"
	"gorm.io/gorm"
)

// Events (WBS 4.3). GET /v1/events merges the webhook inbox and the outbox into
// one newest-first feed for the principal's workspace. Responses carry safe
// fields only: no raw payload, lease token, last error, idempotency key or
// secret. The payload is reduced to an allow-listed summary of short
// identifier-like values plus a count of everything redacted.

const (
	inboxPrefix  = "whk_"
	outboxPrefix = "obx_"
	eventsLimit  = 25
)

var eventToken = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

// summaryFields are the only payload keys whose values may be shown, and only
// when the value is a short identifier-like string.
var summaryFields = map[string]bool{
	"type": true, "event_id": true, "payment_id": true, "attempt_id": true, "provider_id": true,
	"outcome": true, "source": true, "code": true, "payment_status": true, "attempt_status": true,
}

type payloadField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type payloadSummary struct {
	Fields        []payloadField `json:"fields"`
	RedactedCount int            `json:"redacted_count"`
}

func summarizePayload(raw string) payloadSummary {
	summary := payloadSummary{Fields: []payloadField{}}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		summary.RedactedCount = 1
		return summary
	}
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var text string
		if summaryFields[name] && json.Unmarshal(object[name], &text) == nil && eventToken.MatchString(text) {
			summary.Fields = append(summary.Fields, payloadField{Name: name, Value: text})
			continue
		}
		summary.RedactedCount++
	}
	return summary
}

type eventView struct {
	ID               string  `json:"id"`
	Source           string  `json:"source"`
	EventType        string  `json:"event_type"`
	Status           string  `json:"status"`
	Outcome          *string `json:"outcome"`
	OutcomeReason    *string `json:"outcome_reason"`
	ProviderEventID  *string `json:"provider_event_id"`
	PaymentID        *string `json:"payment_id"`
	DuplicateCount   int64   `json:"duplicate_count"`
	NeedsInvestigate bool    `json:"needs_investigation"`
	SignatureStatus  string  `json:"signature_status"`
	DataSource       string  `json:"data_source"`
	CreatedAt        string  `json:"created_at"`
	LastReceivedAt   string  `json:"last_received_at"`
	sortTime         int64
	sortID           string
	ConflictCount    int64   `json:"-"`
	PayloadHash      string  `json:"-"`
	ConflictingHash  *string `json:"-"`
	ProviderID       *string `json:"-"`
	AttemptID        *string `json:"-"`
	ProcessedAt      *int64  `json:"-"`
	DeliveryAttempts int64   `json:"-"`
	rawPayload       string
}

type eventAttempt struct {
	ID            string `json:"id"`
	AttemptNumber int64  `json:"attempt_number"`
	ProviderID    string `json:"provider_id"`
	Status        string `json:"status"`
}

type eventDetail struct {
	eventView
	ProviderID             *string        `json:"provider_id"`
	PayloadHash            *string        `json:"payload_hash"`
	ConflictingPayloadHash *string        `json:"conflicting_payload_hash"`
	ConflictCount          int64          `json:"conflict_count"`
	DeliveryAttempts       int64          `json:"delivery_attempts"`
	ProcessedAt            *string        `json:"processed_at"`
	PaymentStatus          *string        `json:"payment_status"`
	Attempts               []eventAttempt `json:"attempts"`
	PayloadSummary         payloadSummary `json:"payload_summary"`
}

func inboxView(row storage.WebhookInbox) eventView {
	return eventView{
		ID: inboxPrefix + row.ID, Source: "inbox", EventType: row.EventType, Status: row.Status, Outcome: row.Outcome,
		OutcomeReason: row.OutcomeReason, ProviderEventID: &row.ProviderEventID, PaymentID: row.PaymentID,
		DuplicateCount: row.DuplicateCount, NeedsInvestigate: row.InvestigationFlag == 1, SignatureStatus: row.SignatureStatus,
		DataSource: "simulated", CreatedAt: payment.FormatTimestamp(row.ReceivedAt), LastReceivedAt: payment.FormatTimestamp(row.LastReceivedAt),
		sortTime: row.ReceivedAt, sortID: inboxPrefix + row.ID, ConflictCount: row.ConflictCount, PayloadHash: row.PayloadHash,
		ConflictingHash: row.ConflictingPayloadHash, ProviderID: &row.ProviderID, AttemptID: row.AttemptID, ProcessedAt: row.ProcessedAt,
		rawPayload: row.Payload,
	}
}

func outboxView(row storage.OutboxEvent) eventView {
	return eventView{
		ID: outboxPrefix + row.ID, Source: "outbox", EventType: row.EventType, Status: row.Status, PaymentID: row.PaymentID,
		SignatureStatus: payment.SignatureNotApplicable, DataSource: "simulated", CreatedAt: payment.FormatTimestamp(row.CreatedAt),
		LastReceivedAt: payment.FormatTimestamp(row.CreatedAt), sortTime: row.CreatedAt, sortID: outboxPrefix + row.ID,
		DeliveryAttempts: row.Attempts, ProcessedAt: row.DeliveredAt, rawPayload: row.Payload,
	}
}

type eventCursor struct {
	Time int64
	ID   string
}

func encodeEventCursor(c eventCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(c.Time, 10) + "." + c.ID))
}

func decodeEventCursor(value string) (*eventCursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, false
	}
	at, id, ok := strings.Cut(string(raw), ".")
	when, err := strconv.ParseInt(at, 10, 64)
	if !ok || err != nil || when < 0 || !(strings.HasPrefix(id, inboxPrefix) || strings.HasPrefix(id, outboxPrefix)) || len(id) > 80 {
		return nil, false
	}
	return &eventCursor{Time: when, ID: id}, true
}

// stateFilter maps the public state vocabulary to per-table conditions. A nil
// condition with skip=true means the table can never match.
func stateFilter(state string) (inbox, outbox string, skipInbox, skipOutbox bool, ok bool) {
	switch state {
	case "":
		return "", "", false, false, true
	case "pending", "processing", "failed":
		return "status = '" + state + "'", "status = '" + state + "'", false, false, true
	case "processed":
		return "status = 'processed'", "", false, true, true
	case "delivered":
		return "", "status = 'delivered'", true, false, true
	case "applied", "ignored":
		return "outcome = '" + state + "'", "", false, true, true
	case "investigation":
		return "investigation_flag = 1", "", false, true, true
	}
	return "", "", false, false, false
}

func (h *handler) serveEvents(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch {
	case r.URL.Path == "/v1/events":
		h.listEvents(w, r, principal, id)
	case strings.HasPrefix(r.URL.Path, "/v1/events/"):
		h.getEvent(w, r, principal, id, strings.TrimPrefix(r.URL.Path, "/v1/events/"))
	default:
		return false
	}
	return true
}

func (h *handler) listEvents(w http.ResponseWriter, r *http.Request, principal auth.Principal, id string) {
	query := r.URL.Query()
	var fields []FieldError
	limit := eventsLimit
	if value := query.Get("limit"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 100 {
			fields = append(fields, FieldError{Field: "limit", Message: "Use 1 to 100"})
		} else {
			limit = n
		}
	}
	search := query.Get("q")
	if search != "" && !eventToken.MatchString(search) {
		fields = append(fields, FieldError{Field: "q", Message: "Use a payment ID, event ID or provider event ID"})
	}
	source := query.Get("source")
	if source != "" && source != "inbox" && source != "outbox" {
		fields = append(fields, FieldError{Field: "source", Message: "Use inbox or outbox"})
	}
	inboxState, outboxState, skipInbox, skipOutbox, okState := stateFilter(query.Get("state"))
	if !okState {
		fields = append(fields, FieldError{Field: "state", Message: "Use pending, processing, processed, delivered, failed, applied, ignored or investigation"})
	}
	var from, to *int64
	for _, bound := range []struct {
		name   string
		end    bool
		target **int64
	}{{"created_from", false, &from}, {"created_to", true, &to}} {
		if value := query.Get(bound.name); value != "" {
			at, ok := parseListDate(value, bound.end)
			if !ok {
				fields = append(fields, FieldError{Field: bound.name, Message: "Use YYYY-MM-DD or an RFC 3339 timestamp"})
			}
			*bound.target = &at
		}
	}
	var after *eventCursor
	if value := query.Get("cursor"); value != "" {
		cursor, ok := decodeEventCursor(value)
		if !ok {
			fields = append(fields, FieldError{Field: "cursor", Message: "Use next_cursor from a previous page"})
		}
		after = cursor
	}
	if len(fields) > 0 {
		fail(w, 400, "validation_error", "Invalid event parameters", id, fields...)
		return
	}
	db := h.db.WithContext(r.Context())
	bounded := func(table, timeColumn, prefix, extra string) *gorm.DB {
		tx := db.Table(table).Where("workspace_id = ?", principal.WorkspaceID)
		if extra != "" {
			tx = tx.Where(extra)
		}
		if from != nil {
			tx = tx.Where(timeColumn+" >= ?", *from)
		}
		if to != nil {
			tx = tx.Where(timeColumn+" < ?", *to)
		}
		if after != nil {
			tx = tx.Where("("+timeColumn+" < ? OR ("+timeColumn+" = ? AND (? || id) < ?))", after.Time, after.Time, prefix, after.ID)
		}
		return tx.Order(timeColumn + " DESC, id DESC").Limit(limit + 1)
	}
	events := []eventView{}
	if source != "outbox" && !skipInbox {
		var rows []storage.WebhookInbox
		clause := ""
		args := []any{}
		if search != "" {
			clause, args = "(payment_id = ? OR provider_event_id = ? OR id = ? OR ? || id = ?)", []any{search, search, strings.TrimPrefix(search, inboxPrefix), inboxPrefix, search}
		}
		tx := bounded("webhook_inbox", "received_at", inboxPrefix, inboxState)
		if clause != "" {
			tx = tx.Where(clause, args...)
		}
		if err := tx.Find(&rows).Error; err != nil {
			fail(w, 503, "unavailable", "Events unavailable", id)
			return
		}
		for _, row := range rows {
			events = append(events, inboxView(row))
		}
	}
	if source != "inbox" && !skipOutbox {
		var rows []storage.OutboxEvent
		tx := bounded("outbox_events", "created_at", outboxPrefix, outboxState)
		if search != "" {
			tx = tx.Where("(payment_id = ? OR ? || id = ?)", search, outboxPrefix, search)
		}
		if err := tx.Find(&rows).Error; err != nil {
			fail(w, 503, "unavailable", "Events unavailable", id)
			return
		}
		for _, row := range rows {
			events = append(events, outboxView(row))
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].sortTime != events[j].sortTime {
			return events[i].sortTime > events[j].sortTime
		}
		return events[i].sortID > events[j].sortID
	})
	var next *string
	if len(events) > limit {
		events = events[:limit]
		last := events[len(events)-1]
		cursor := encodeEventCursor(eventCursor{Time: last.sortTime, ID: last.sortID})
		next = &cursor
	}
	writeJSON(w, 200, map[string]any{"items": events, "next_cursor": next})
}

func (h *handler) getEvent(w http.ResponseWriter, r *http.Request, principal auth.Principal, id, eventID string) {
	db := h.db.WithContext(r.Context())
	var view eventView
	switch {
	case strings.HasPrefix(eventID, inboxPrefix) && eventToken.MatchString(eventID):
		var row storage.WebhookInbox
		if err := db.Where("workspace_id = ? AND id = ?", principal.WorkspaceID, strings.TrimPrefix(eventID, inboxPrefix)).Take(&row).Error; err != nil {
			h.eventLookupFailed(w, id, err)
			return
		}
		view = inboxView(row)
	case strings.HasPrefix(eventID, outboxPrefix) && eventToken.MatchString(eventID):
		var row storage.OutboxEvent
		if err := db.Where("workspace_id = ? AND id = ?", principal.WorkspaceID, strings.TrimPrefix(eventID, outboxPrefix)).Take(&row).Error; err != nil {
			h.eventLookupFailed(w, id, err)
			return
		}
		view = outboxView(row)
	default:
		fail(w, 404, "not_found", "Event not found", id)
		return
	}
	detail := eventDetail{eventView: view, ProviderID: view.ProviderID, ConflictingPayloadHash: view.ConflictingHash, ConflictCount: view.ConflictCount,
		DeliveryAttempts: view.DeliveryAttempts, Attempts: []eventAttempt{}, PayloadSummary: summarizePayload(view.rawPayload)}
	if view.Source == "inbox" {
		hash := view.PayloadHash
		detail.PayloadHash = &hash
	}
	if view.ProcessedAt != nil {
		value := payment.FormatTimestamp(*view.ProcessedAt)
		detail.ProcessedAt = &value
	}
	if view.PaymentID != nil {
		if p, err := storage.GetPayment(db, principal.WorkspaceID, *view.PaymentID); err == nil {
			detail.PaymentStatus = &p.Status
			attempts, err := storage.ListPaymentAttempts(db, principal.WorkspaceID, p.ID)
			if err != nil {
				fail(w, 503, "unavailable", "Event unavailable", id)
				return
			}
			for _, a := range attempts {
				detail.Attempts = append(detail.Attempts, eventAttempt{ID: a.ID, AttemptNumber: a.AttemptNumber, ProviderID: a.ProviderID, Status: a.Status})
			}
		} else if !errors.Is(err, storage.ErrNotFound) {
			fail(w, 503, "unavailable", "Event unavailable", id)
			return
		}
	}
	writeJSON(w, 200, detail)
}

func (h *handler) eventLookupFailed(w http.ResponseWriter, id string, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		fail(w, 404, "not_found", "Event not found", id)
		return
	}
	fail(w, 503, "unavailable", "Event unavailable", id)
}
