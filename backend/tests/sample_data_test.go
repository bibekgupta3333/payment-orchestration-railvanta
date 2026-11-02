package tests

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/railvanta/railvanta/backend/internal/auth"
	"github.com/railvanta/railvanta/backend/internal/simulation"
	"github.com/railvanta/railvanta/backend/internal/storage"
	httpapi "github.com/railvanta/railvanta/backend/internal/transport/http"
)

type sampleList struct {
	Items []httpapi.SampleRunView `json:"items"`
}

func (d *dispatchEnv) samples(key string) sampleList {
	d.t.Helper()
	response := d.do("GET", "/v1/sandbox/samples", key, "", "")
	if response.Code != 200 {
		d.t.Fatalf("samples: %d %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		d.t.Fatalf("Cache-Control %q", got)
	}
	var list sampleList
	if err := json.Unmarshal(response.Body.Bytes(), &list); err != nil {
		d.t.Fatal(err)
	}
	if len(list.Items) != len(simulation.SampleScenarios) {
		d.t.Fatalf("%d items, want %d", len(list.Items), len(simulation.SampleScenarios))
	}
	for i, item := range list.Items {
		if item.Scenario != simulation.SampleScenarios[i] {
			d.t.Fatalf("item %d is %s, want %s", i, item.Scenario, simulation.SampleScenarios[i])
		}
	}
	return list
}

func (d *dispatchEnv) sampleRunner() *simulation.Runner {
	return simulation.NewRunner(d.store.DB, d.service, simulation.Options{Notify: d.dispatcher.Notify, Poll: 5 * time.Millisecond})
}

func sampleRunCount(t *testing.T, d *dispatchEnv, workspaceID string) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	for _, scenario := range simulation.SampleScenarios {
		var n int64
		if err := d.store.DB.Model(&storage.SimulationRun{}).Where("workspace_id = ? AND seed = ?", workspaceID, simulation.SampleSeed(scenario)).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		counts[scenario] = n
	}
	return counts
}

func TestSampleDataSeedsEachScenarioOnceAndIsListed(t *testing.T) {
	d := newSimEnv(t)

	// Before seeding every scenario is missing.
	for _, item := range d.samples(credentials.ReaderKey).Items {
		if item.Status != httpapi.SampleStatusMissing || item.RunID != nil || item.PaymentID != nil || item.InvariantsPassed != 0 || item.InvariantsTotal != 0 {
			t.Fatalf("unseeded item %+v", item)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := d.sampleRunner().SeedSamples(ctx, auth.DemoWorkspaceID, nil); err != nil {
		t.Fatal(err)
	}
	for scenario, n := range sampleRunCount(t, d, auth.DemoWorkspaceID) {
		if n != 1 {
			t.Fatalf("%s has %d sample runs after the first seed, want 1", scenario, n)
		}
	}

	// The endpoint lists each run in order; a read key is enough.
	list := d.samples(credentials.ReaderKey)
	for _, item := range list.Items {
		run, err := simulation.LatestSampleRun(d.store.DB, auth.DemoWorkspaceID, item.Scenario)
		if err != nil {
			t.Fatal(err)
		}
		if item.RunID == nil || *item.RunID != run.ID || item.Status != run.Status || item.PaymentID == nil {
			t.Fatalf("item %+v does not match run %s (%s)", item, run.ID, run.Status)
		}
		if item.Status != storage.SimulationCompleted {
			t.Fatalf("%s sample ended %s", item.Scenario, item.Status)
		}
		if item.InvariantsTotal < len(simulation.Titles) || item.InvariantsPassed != item.InvariantsTotal {
			t.Fatalf("%s invariants %d/%d", item.Scenario, item.InvariantsPassed, item.InvariantsTotal)
		}
	}

	// A second seed (restart over a retained volume) creates nothing.
	if err := d.sampleRunner().SeedSamples(ctx, auth.DemoWorkspaceID, nil); err != nil {
		t.Fatal(err)
	}
	for scenario, n := range sampleRunCount(t, d, auth.DemoWorkspaceID) {
		if n != 1 {
			t.Fatalf("%s has %d sample runs after the second seed, want 1", scenario, n)
		}
	}

	// Workspace isolation: the other workspace sees none of them.
	for _, item := range d.samples(credentials.OtherWorkspaceKey).Items {
		if item.Status != httpapi.SampleStatusMissing || item.RunID != nil {
			t.Fatalf("other workspace sees %+v", item)
		}
	}
}

func TestSampleDataEndpointRequiresCredentials(t *testing.T) {
	d := newSimEnv(t)
	if response := d.do("GET", "/v1/sandbox/samples", "", "", ""); response.Code != 401 {
		t.Fatalf("unauthenticated samples: %d", response.Code)
	}
}

func TestSampleDataStopsWhenContextEnds(t *testing.T) {
	d := newSimEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.sampleRunner().SeedSamples(ctx, "00000000-0000-7000-8000-00000000ffff", nil); err == nil {
		t.Fatal("expected the cancelled seeder to stop")
	}
}
