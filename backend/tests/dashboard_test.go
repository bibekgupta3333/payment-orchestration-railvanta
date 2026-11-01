package tests

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/payment"
)

type summaryBody struct {
	DataSource   string           `json:"data_source"`
	Window       string           `json:"window"`
	PaymentCount int64            `json:"payment_count"`
	StatusCounts map[string]int64 `json:"status_counts"`
	Volumes      []struct {
		Currency     string `json:"currency"`
		AmountMinor  string `json:"amount_minor"`
		PaymentCount int64  `json:"payment_count"`
	} `json:"volumes"`
	SucceededVolumes []json.RawMessage `json:"succeeded_volumes"`
	SuccessRate      *string           `json:"success_rate_percent"`
	P95              *int64            `json:"completion_latency_p95_ms"`
	Samples          int64             `json:"completion_latency_samples"`
}

func (e *env) summary(key, query string) (int, summaryBody) {
	e.t.Helper()
	response := e.do("GET", "/v1/dashboard/summary"+query, key, "", "")
	var body summaryBody
	if response.Code == 200 {
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			e.t.Fatal(err)
		}
	}
	return response.Code, body
}

func TestDashboardSummaryWithoutObservationsIsNull(t *testing.T) {
	e := newEnv(t, payment.Options{})
	code, body := e.summary(credentials.OperatorKey, "")
	if code != 200 || body.DataSource != "observed" || body.Window != "24h" || body.PaymentCount != 0 {
		t.Fatalf("unexpected summary %d %+v", code, body)
	}
	if body.SuccessRate != nil || body.P95 != nil || body.Samples != 0 || len(body.Volumes) != 0 {
		t.Fatalf("empty workspace must report no observations: %+v", body)
	}
	if code, _ := e.summary(credentials.OperatorKey, "?window=1y"); code != 400 {
		t.Fatalf("invalid window accepted: %d", code)
	}
	if code, _ := e.summary("", ""); code != 401 {
		t.Fatalf("unauthenticated summary returned %d", code)
	}
}

func TestDashboardGroupsVolumesPerCurrencyExactly(t *testing.T) {
	e := newEnv(t, payment.Options{})
	maxInt64 := "9223372036854775807"
	for i, body := range []string{intent(maxInt64, "USD", auth.DemoMethodID), intent(maxInt64, "USD", auth.DemoMethodID), intent("1234", "JPY", auth.DemoMethodID)} {
		if response := e.create("dash-"+string(rune('a'+i)), body); response.Code != 202 {
			t.Fatalf("create %d: %s", response.Code, response.Body.String())
		}
	}
	// Mark one USD payment succeeded 40ms after creation to observe a latency.
	if err := e.store.DB.Exec("UPDATE payments SET status = 'succeeded', updated_at = created_at + 40 WHERE id = (SELECT id FROM payments WHERE currency = 'USD' ORDER BY id LIMIT 1)").Error; err != nil {
		t.Fatal(err)
	}
	code, body := e.summary(credentials.OperatorKey, "?window=7d")
	if code != 200 || body.PaymentCount != 3 || len(body.Volumes) != 2 {
		t.Fatalf("unexpected summary %d %+v", code, body)
	}
	if body.Volumes[0].Currency != "JPY" || body.Volumes[0].AmountMinor != "1234" {
		t.Fatalf("JPY volume %+v", body.Volumes[0])
	}
	// The sum exceeds int64, so it must be exact big-integer arithmetic.
	if body.Volumes[1].Currency != "USD" || body.Volumes[1].AmountMinor != "18446744073709551614" || body.Volumes[1].PaymentCount != 2 {
		t.Fatalf("USD volume %+v", body.Volumes[1])
	}
	if body.SuccessRate == nil || *body.SuccessRate != "100.0" || body.P95 == nil || *body.P95 != 40 || body.Samples != 1 {
		t.Fatalf("success rate/p95 %+v", body)
	}
	if body.StatusCounts["succeeded"] != 1 || body.StatusCounts["created"] != 2 || len(body.SucceededVolumes) != 1 {
		t.Fatalf("status counts %+v", body)
	}
	// Another workspace sees none of it.
	code, other := e.summary(credentials.OtherWorkspaceKey, "")
	if code != 200 || other.PaymentCount != 0 || len(other.Volumes) != 0 {
		t.Fatalf("workspace leak %d %+v", code, other)
	}

	response := e.do("GET", "/v1/dashboard/payment-volume?window=24h", credentials.ReaderKey, "", "")
	var volume struct {
		DataSource string `json:"data_source"`
		Bucket     string `json:"bucket"`
		Items      []struct {
			Timestamp   int64  `json:"timestamp"`
			Currency    string `json:"currency"`
			AmountMinor string `json:"amount_minor"`
		} `json:"items"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &volume) != nil {
		t.Fatalf("volume %d %s", response.Code, response.Body.String())
	}
	if volume.DataSource != "observed" || volume.Bucket != "hour" || len(volume.Items) < 2 {
		t.Fatalf("volume body %+v", volume)
	}
	for _, item := range volume.Items {
		if item.Timestamp%3_600_000 != 0 || (item.Currency != "USD" && item.Currency != "JPY") {
			t.Fatalf("bucket %+v", item)
		}
	}
}

func TestPaymentHistoryMasksKeyAndIsWorkspaceScoped(t *testing.T) {
	e := newEnv(t, payment.Options{})
	response := e.create("history-key-0123456789", intent("2500", "USD", auth.DemoMethodID))
	if response.Code != 202 {
		t.Fatalf("create %d", response.Code)
	}
	id := decodePayment(t, response.Body.Bytes()).ID
	history := e.do("GET", "/v1/payments/"+id+"/history", credentials.ReaderKey, "", "")
	if history.Code != 200 {
		t.Fatalf("history %d %s", history.Code, history.Body.String())
	}
	text := history.Body.String()
	if strings.Contains(text, "history-key-0123456789") || !strings.Contains(text, `"idempotency_key_masked":"****6789"`) {
		t.Fatalf("idempotency key not masked: %s", text)
	}
	if !strings.Contains(text, `"to_status":"created"`) || !strings.Contains(text, `"event_type":"payment.dispatch"`) {
		t.Fatalf("history content: %s", text)
	}
	if other := e.do("GET", "/v1/payments/"+id+"/history", credentials.OtherWorkspaceKey, "", ""); other.Code != 404 {
		t.Fatalf("cross-workspace history returned %d", other.Code)
	}
}

func TestPaymentListFiltersAndDirectionalCursor(t *testing.T) {
	e := newEnv(t, payment.Options{})
	ids := []string{}
	for i, amount := range []string{"100", "200", "300"} {
		body := `{"amount_minor":"` + amount + `","currency":"USD","payment_method_id":"` + auth.DemoMethodID + `","merchant_reference":"order-` + amount + `"}`
		response := e.create("filter-"+string(rune('a'+i)), body)
		if response.Code != 202 {
			t.Fatalf("create %d", response.Code)
		}
		ids = append(ids, decodePayment(t, response.Body.Bytes()).ID)
	}
	list := func(query string) (int, []string, *string) {
		response := e.do("GET", "/v1/payments?"+query, credentials.ReaderKey, "", "")
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
			NextCursor *string `json:"next_cursor"`
		}
		_ = json.Unmarshal(response.Body.Bytes(), &page)
		found := []string{}
		for _, item := range page.Items {
			found = append(found, item.ID)
		}
		return response.Code, found, page.NextCursor
	}
	if code, found, _ := list("q=order-200"); code != 200 || len(found) != 1 || found[0] != ids[1] {
		t.Fatalf("reference search %d %v", code, found)
	}
	if code, found, _ := list("q=" + ids[2]); code != 200 || len(found) != 1 || found[0] != ids[2] {
		t.Fatalf("ID search %d %v", code, found)
	}
	if code, found, _ := list("amount_min=150&amount_max=300&currency=USD&rail=mock"); code != 200 || len(found) != 2 {
		t.Fatalf("amount filter %d %v", code, found)
	}
	if code, found, _ := list("rail=card"); code != 200 || len(found) != 0 {
		t.Fatalf("rail filter %d %v", code, found)
	}
	if code, found, _ := list("created_from=2000-01-01&created_to=2999-12-31"); code != 200 || len(found) != 3 {
		t.Fatalf("date filter %d %v", code, found)
	}
	code, first, next := list("sort=created_asc&limit=2")
	if code != 200 || len(first) != 2 || next == nil {
		t.Fatalf("ascending page %d %v", code, first)
	}
	if code, _, _ := list("sort=created_desc&cursor=" + *next); code != 400 {
		t.Fatalf("cursor reused with another sort returned %d", code)
	}
	code, second, last := list("sort=created_asc&limit=2&cursor=" + *next)
	if code != 200 || len(second) != 1 || last != nil {
		t.Fatalf("ascending second page %d %v", code, second)
	}
	seen := map[string]bool{first[0]: true, first[1]: true, second[0]: true}
	if len(seen) != 3 {
		t.Fatalf("pages overlap: %v %v", first, second)
	}
	for _, query := range []string{"sort=amount", "amount_min=1.5", "created_from=yesterday", "rail=wire", "currency=usd"} {
		if code, _, _ := list(query); code != 400 {
			t.Fatalf("%s returned %d", query, code)
		}
	}
}
