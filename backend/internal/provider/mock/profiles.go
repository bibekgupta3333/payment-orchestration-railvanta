package mock

import (
	"time"

	"gorm.io/gorm"
)

// Profile is one clearly-labelled SIMULATED provider backed by the same
// deterministic mock adapter: it differs only in code, rails and a fixed
// latency, so routing has a real story without any real provider.
type Profile struct {
	ID    string
	Code  string
	Label string
	// Rails are schema rails (card, ach) the profile declares support for.
	Rails   []string
	Latency time.Duration
}

// Simulated demo provider profiles. IDs are the stable demo-workspace provider
// IDs seeded by auth.SeedDemo.
var (
	ProfileFast    = Profile{ID: "00000000-0000-7000-8000-000000000211", Code: "mock_fast", Label: "Simulated Fast", Rails: []string{"card", "ach"}, Latency: 50 * time.Millisecond}
	ProfileEconomy = Profile{ID: "00000000-0000-7000-8000-000000000212", Code: "mock_economy", Label: "Simulated Economy", Rails: []string{"card"}, Latency: 400 * time.Millisecond}
	ProfileACH     = Profile{ID: "00000000-0000-7000-8000-000000000213", Code: "mock_ach", Label: "Simulated ACH", Rails: []string{"ach"}, Latency: 250 * time.Millisecond}
)

// Profiles lists every simulated profile in display order.
var Profiles = []Profile{ProfileFast, ProfileEconomy, ProfileACH}

// NewProfile builds an adapter for a profile. opts.Code, Rails and Latency are
// overridden by the profile; set opts.Sleep to a no-op in tests to skip latency.
func NewProfile(db *gorm.DB, p Profile, opts Options) *Adapter {
	opts.Code, opts.Rails, opts.Latency = p.Code, p.Rails, p.Latency
	return New(db, opts)
}

// ProfileByCode returns the profile for a provider code.
func ProfileByCode(code string) (Profile, bool) {
	for _, p := range Profiles {
		if p.Code == code {
			return p, true
		}
	}
	return Profile{}, false
}
