// Package fuju is the SNS-backend's outbound bridge to fuju-emotion-model.
//
// Two streams flow outward:
//
//   - Contents — when a Post is published, its metadata is forwarded to
//     fuju's POST /v1/{tenant}/contents. SNS does NOT pre-extract
//     hashtags / entities; fuju does that side itself (RFC-LT-003
//     Option D).
//   - Events — server-side user actions (like, follow, comment) are
//     buffered and posted in batches to fuju's POST /v1/{tenant}/events.
//     Append-only, fire-and-forget from the SNS-side request path.
//     Client-originated view_*/scroll_stop/rewind signals are NOT routed
//     here — clients ingest them directly into fuju.
//
// The dispatcher absorbs both streams via internal channels and flushes
// them through a background worker. Hot-path callers (post / like /
// follow commit hooks) only enqueue; they never block on the network
// call to fuju.
//
// Loss policy: in-memory queues are bounded (BatchSize × 8 by default).
// When full, oldest entries are dropped with a warning log — this is
// an analytics pipeline, not a financial one. A future revision can
// add disk persistence if loss tolerance becomes too lax.
package fuju

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fuju/backend/pkg/fujumodel"
	"github.com/fuju/backend/pkg/logger"
)

// Sender is the subset of fujumodel.Client this package needs. Lets
// tests inject a recording stub without standing up an HTTP server.
type Sender interface {
	RegisterContents(ctx context.Context, items []fujumodel.Content) error
	SendEvents(ctx context.Context, events []fujumodel.Event) error
}

// Options configures a Dispatcher.
type Options struct {
	Sender        Sender
	Log           *logger.Logger
	BatchSize     int           // flush batch threshold (default 50)
	FlushInterval time.Duration // periodic flush cadence (default 5s)
	QueueCapacity int           // in-memory queue cap (default BatchSize*8)
	SendTimeout   time.Duration // per-flush context timeout (default 5s)
}

// Dispatcher is the outbound queue + flusher.
type Dispatcher struct {
	sender        Sender
	log           *logger.Logger
	batchSize     int
	flushInterval time.Duration
	sendTimeout   time.Duration

	contentsCh chan fujumodel.Content
	eventsCh   chan fujumodel.Event

	startOnce sync.Once
	stopOnce  sync.Once
	done      chan struct{}
}

// NewDispatcher constructs a Dispatcher. Call Run(ctx) to start the
// flusher; the channels are closed via Stop().
func NewDispatcher(opts Options) (*Dispatcher, error) {
	if opts.Sender == nil {
		return nil, errors.New("fuju: dispatcher needs a Sender")
	}
	if opts.Log == nil {
		return nil, errors.New("fuju: dispatcher needs a Logger")
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 50
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 5 * time.Second
	}
	if opts.SendTimeout <= 0 {
		opts.SendTimeout = 5 * time.Second
	}
	if opts.QueueCapacity <= 0 {
		opts.QueueCapacity = opts.BatchSize * 8
	}
	return &Dispatcher{
		sender:        opts.Sender,
		log:           opts.Log,
		batchSize:     opts.BatchSize,
		flushInterval: opts.FlushInterval,
		sendTimeout:   opts.SendTimeout,
		contentsCh:    make(chan fujumodel.Content, opts.QueueCapacity),
		eventsCh:      make(chan fujumodel.Event, opts.QueueCapacity),
		done:          make(chan struct{}),
	}, nil
}

// EnqueueContent buffers a content registration. Non-blocking: drops
// the oldest entry when the queue is full.
func (d *Dispatcher) EnqueueContent(ctx context.Context, c fujumodel.Content) {
	select {
	case d.contentsCh <- c:
		return
	default:
	}
	// Slow path: queue full. Drop the oldest, then enqueue. Logging is
	// deliberately noisy so operators notice if drops keep happening.
	select {
	case <-d.contentsCh:
		d.log.Warn(ctx, "fuju.dispatcher: contents queue full; dropped oldest entry")
	default:
	}
	select {
	case d.contentsCh <- c:
	default:
		d.log.Warn(ctx, "fuju.dispatcher: contents queue still full; dropped current entry",
			"content_id", c.ContentID)
	}
}

// EnqueueEvent buffers a user event. Same loss policy as EnqueueContent.
func (d *Dispatcher) EnqueueEvent(ctx context.Context, e fujumodel.Event) {
	select {
	case d.eventsCh <- e:
		return
	default:
	}
	select {
	case <-d.eventsCh:
		d.log.Warn(ctx, "fuju.dispatcher: events queue full; dropped oldest entry")
	default:
	}
	select {
	case d.eventsCh <- e:
	default:
		d.log.Warn(ctx, "fuju.dispatcher: events queue still full; dropped current entry",
			"event_type", string(e.EventType), "user_id", e.UserID)
	}
}

// Run drives the flusher loop. Blocks until ctx is cancelled, at which
// point it makes one final best-effort flush and returns.
func (d *Dispatcher) Run(ctx context.Context) {
	d.startOnce.Do(func() {
		go d.loop(ctx)
	})
}

func (d *Dispatcher) loop(ctx context.Context) {
	defer close(d.done)

	timer := time.NewTicker(d.flushInterval)
	defer timer.Stop()

	contents := make([]fujumodel.Content, 0, d.batchSize)
	events := make([]fujumodel.Event, 0, d.batchSize)

	for {
		select {
		case <-ctx.Done():
			d.flushFinal(contents, events)
			return
		case c := <-d.contentsCh:
			contents = append(contents, c)
			if len(contents) >= d.batchSize {
				d.flushContents(ctx, contents)
				contents = contents[:0]
			}
		case e := <-d.eventsCh:
			events = append(events, e)
			if len(events) >= d.batchSize {
				d.flushEvents(ctx, events)
				events = events[:0]
			}
		case <-timer.C:
			if len(contents) > 0 {
				d.flushContents(ctx, contents)
				contents = contents[:0]
			}
			if len(events) > 0 {
				d.flushEvents(ctx, events)
				events = events[:0]
			}
		}
	}
}

func (d *Dispatcher) flushContents(parent context.Context, batch []fujumodel.Content) {
	if len(batch) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), d.sendTimeout)
	defer cancel()
	if err := d.sender.RegisterContents(ctx, batch); err != nil {
		d.log.Error(parent, "fuju.dispatcher: RegisterContents failed", err,
			"batch_size", len(batch))
	}
}

func (d *Dispatcher) flushEvents(parent context.Context, batch []fujumodel.Event) {
	if len(batch) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), d.sendTimeout)
	defer cancel()
	if err := d.sender.SendEvents(ctx, batch); err != nil {
		d.log.Error(parent, "fuju.dispatcher: SendEvents failed", err,
			"batch_size", len(batch))
	}
}

// flushFinal drains any pending in-channel items + provided slices and
// flushes once with a Background context. Best effort — if shutdown is
// fast, late items may be lost.
func (d *Dispatcher) flushFinal(contents []fujumodel.Content, events []fujumodel.Event) {
	for {
		select {
		case c := <-d.contentsCh:
			contents = append(contents, c)
		default:
			goto eventsDrain
		}
	}
eventsDrain:
	for {
		select {
		case e := <-d.eventsCh:
			events = append(events, e)
		default:
			goto flush
		}
	}
flush:
	bg := context.Background()
	if len(contents) > 0 {
		ctx, cancel := context.WithTimeout(bg, d.sendTimeout)
		_ = d.sender.RegisterContents(ctx, contents)
		cancel()
	}
	if len(events) > 0 {
		ctx, cancel := context.WithTimeout(bg, d.sendTimeout)
		_ = d.sender.SendEvents(ctx, events)
		cancel()
	}
}

// Wait blocks until the loop exits (after ctx cancellation in Run).
// The shutdown sequence in main.go uses this to bound how long teardown
// waits for the final flush.
func (d *Dispatcher) Wait() {
	d.stopOnce.Do(func() { /* close once-only marker; loop closes done */ })
	<-d.done
}
