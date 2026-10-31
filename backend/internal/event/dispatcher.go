package event

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"sort"
	"sync"
	"time"

	"gorm.io/gorm"
)

// Handler processes one claimed outbox row. It runs with no transaction open
// and must not hold one across provider I/O. A handler that commits effects
// should call Complete(tx, claim, now) inside the same transaction; when it
// returns nil without doing so, the dispatcher completes the row itself.
// Returning an error releases the row for retry with backoff (see RetryAfter).
type Handler func(ctx context.Context, claim Claim) error

// RetryError asks the dispatcher to release the row and retry at a set delay.
type RetryError struct {
	Delay time.Duration
	Err   error
}

func (e *RetryError) Error() string { return e.Err.Error() }
func (e *RetryError) Unwrap() error { return e.Err }

// RetryAfter wraps err so the row becomes due again after delay.
func RetryAfter(delay time.Duration, err error) error { return &RetryError{Delay: delay, Err: err} }

// Options configures a Dispatcher. Zero values select defaults.
type Options struct {
	// PollInterval is the idle wait between claim rounds (default 500ms).
	PollInterval time.Duration
	// LeaseDuration bounds lease_until (default 30s). Leases are renewed every
	// LeaseDuration/3 while a handler runs.
	LeaseDuration time.Duration
	// MaxInFlight bounds concurrently running handlers (default 4).
	MaxInFlight int
	// MaxAttempts parks a row as failed after this many handler errors (default 10).
	MaxAttempts int64
	// BaseBackoff and MaxBackoff bound full-jitter retry backoff (100ms, 30s).
	BaseBackoff, MaxBackoff time.Duration
	// DrainTimeout bounds how long Stop waits for in-flight handlers before
	// cancelling their contexts (default 5s).
	DrainTimeout time.Duration
	Now          func() time.Time
	Logger       *slog.Logger
	Metrics      *Metrics
}

// Dispatcher claims due outbox rows and runs the handler registered for each
// event type. It only claims types it has a handler for, so commands owned by a
// later component (for example payment.lookup) wait durably until registered.
type Dispatcher struct {
	db       *gorm.DB
	opts     Options
	handlers map[string]Handler

	mu       sync.Mutex
	started  bool
	stopping chan struct{}
	loopDone chan struct{}
	cancel   context.CancelFunc // cancels in-flight handler contexts
	inFlight sync.WaitGroup
	slots    chan struct{}
	wake     chan struct{}
}

// NewDispatcher creates a stopped dispatcher over the store's root handle.
func NewDispatcher(db *gorm.DB, opts Options) *Dispatcher {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 500 * time.Millisecond
	}
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = 30 * time.Second
	}
	if opts.MaxInFlight <= 0 {
		opts.MaxInFlight = 4
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 10
	}
	if opts.BaseBackoff <= 0 {
		opts.BaseBackoff = 100 * time.Millisecond
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 30 * time.Second
	}
	if opts.DrainTimeout <= 0 {
		opts.DrainTimeout = 5 * time.Second
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	if opts.Metrics == nil {
		opts.Metrics = NewMetrics()
	}
	return &Dispatcher{db: db, opts: opts, handlers: map[string]Handler{}, slots: make(chan struct{}, opts.MaxInFlight), wake: make(chan struct{}, 1)}
}

// Register binds a handler to an event type. Call before Start.
func (d *Dispatcher) Register(eventType string, handler Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[eventType] = handler
}

// Metrics returns the dispatcher's metrics.
func (d *Dispatcher) Metrics() *Metrics { return d.opts.Metrics }

// Options returns the effective options.
func (d *Dispatcher) Options() Options { return d.opts }

func (d *Dispatcher) eventTypes() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	types := make([]string, 0, len(d.handlers))
	for t := range d.handlers {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

func (d *Dispatcher) handler(eventType string) Handler {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.handlers[eventType]
}

// Start launches the claim loop. Start after migrations; call Stop before
// closing the database.
func (d *Dispatcher) Start() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started {
		return
	}
	d.started = true
	d.stopping, d.loopDone = make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	go d.loop(ctx)
}

// Notify wakes the claim loop early, e.g. right after a command is committed.
func (d *Dispatcher) Notify() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Stop stops claiming new rows, waits up to DrainTimeout (or ctx) for in-flight
// handlers, then cancels their contexts and waits for them to return. Handlers
// persist their outcome on a context detached from cancellation, so a
// cancelled provider call is recorded (as ambiguous) rather than lost.
func (d *Dispatcher) Stop(ctx context.Context) error {
	d.mu.Lock()
	if !d.started {
		d.mu.Unlock()
		return nil
	}
	d.started = false
	close(d.stopping)
	d.mu.Unlock()
	<-d.loopDone
	drained := make(chan struct{})
	go func() { d.inFlight.Wait(); close(drained) }()
	timer := time.NewTimer(d.opts.DrainTimeout)
	defer timer.Stop()
	var err error
	select {
	case <-drained:
	case <-timer.C:
		err = errors.New("dispatcher drain timed out; cancelled in-flight handlers")
	case <-ctx.Done():
		err = ctx.Err()
	}
	d.cancel()
	<-drained
	return err
}

func (d *Dispatcher) loop(ctx context.Context) {
	defer close(d.loopDone)
	for {
		claimed, err := d.claimAndRun(ctx, false)
		if err != nil {
			d.opts.Logger.Error("outbox claim failed", "error", err.Error())
		}
		if claimed > 0 && err == nil {
			select {
			case <-d.stopping:
				return
			default:
				continue
			}
		}
		timer := time.NewTimer(d.opts.PollInterval)
		select {
		case <-d.stopping:
			timer.Stop()
			return
		case <-d.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// RunOnce claims one batch of due rows and runs their handlers to completion
// before returning. Tests use it to drive the dispatcher deterministically.
func (d *Dispatcher) RunOnce(ctx context.Context) (int, error) {
	return d.claimAndRun(ctx, true)
}

func (d *Dispatcher) claimAndRun(ctx context.Context, wait bool) (int, error) {
	free := d.opts.MaxInFlight - len(d.slots)
	if free <= 0 {
		return 0, nil
	}
	claims, err := ClaimDue(ctx, d.db, d.eventTypes(), d.opts.Now(), d.opts.LeaseDuration, free)
	for _, claim := range claims {
		d.opts.Metrics.observeClaim(claim)
		d.slots <- struct{}{}
		d.inFlight.Add(1)
		go func(claim Claim) {
			defer func() { <-d.slots; d.inFlight.Done() }()
			d.run(ctx, claim)
		}(claim)
	}
	if wait {
		d.inFlight.Wait()
	}
	return len(claims), err
}

// run executes one claim with a lease heartbeat, then settles the row.
func (d *Dispatcher) run(ctx context.Context, claim Claim) {
	handler := d.handler(claim.EventType)
	heartbeatDone := make(chan struct{})
	heartbeatStopped := make(chan struct{})
	go func() {
		defer close(heartbeatStopped)
		ticker := time.NewTicker(max(d.opts.LeaseDuration/3, time.Millisecond))
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatDone:
				return
			case <-ticker.C:
				renewCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), d.opts.LeaseDuration)
				err := Renew(renewCtx, d.db, &claim, d.opts.Now(), d.opts.LeaseDuration)
				cancel()
				if errors.Is(err, ErrLeaseLost) {
					d.opts.Metrics.leaseLost.Add(1)
					d.opts.Logger.Warn("outbox lease lost during handler", "event_id", claim.EventID)
					return
				}
				if err != nil {
					d.opts.Logger.Warn("outbox lease renewal failed", "event_id", claim.EventID, "error", err.Error())
				}
			}
		}
	}()
	err := handler(ctx, claim)
	close(heartbeatDone)
	<-heartbeatStopped
	// Settling must survive shutdown cancellation.
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	switch {
	case err == nil:
		d.opts.Metrics.handled.Add(1)
		completeErr := Complete(d.db.WithContext(settleCtx), claim, d.opts.Now().UnixMilli())
		if completeErr != nil && !errors.Is(completeErr, ErrLeaseLost) {
			d.opts.Logger.Error("outbox completion failed", "event_id", claim.EventID, "error", completeErr.Error())
		}
	case errors.Is(err, ErrLeaseLost):
		d.opts.Metrics.leaseLost.Add(1)
		d.opts.Logger.Warn("outbox handler lost its lease", "event_id", claim.EventID)
	default:
		d.opts.Metrics.handlerErrors.Add(1)
		d.opts.Logger.Warn("outbox handler failed", "event_id", claim.EventID, "event_type", claim.EventType, "attempts", claim.Attempts, "error", err.Error())
		var settleErr error
		if claim.Attempts >= d.opts.MaxAttempts {
			d.opts.Metrics.parked.Add(1)
			settleErr = Fail(settleCtx, d.db, claim, err.Error())
		} else {
			settleErr = Release(settleCtx, d.db, claim, d.opts.Now().Add(d.backoff(claim.Attempts, err)), err.Error())
		}
		if settleErr != nil && !errors.Is(settleErr, ErrLeaseLost) {
			d.opts.Logger.Error("outbox release failed", "event_id", claim.EventID, "error", settleErr.Error())
		}
	}
}

func (d *Dispatcher) backoff(attempt int64, err error) time.Duration {
	var retry *RetryError
	if errors.As(err, &retry) && retry.Delay > 0 {
		return retry.Delay
	}
	ceiling := d.opts.BaseBackoff << min(attempt, 16)
	if ceiling > d.opts.MaxBackoff || ceiling <= 0 {
		ceiling = d.opts.MaxBackoff
	}
	return time.Duration(rand.Int64N(int64(ceiling)) + 1)
}
