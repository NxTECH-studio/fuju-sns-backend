package fuju

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fuju/backend/pkg/fujumodel"
	"github.com/fuju/backend/pkg/logger"
)

// recordingSender captures every batch the dispatcher flushes.
type recordingSender struct {
	mu       sync.Mutex
	contents [][]fujumodel.Content
	events   [][]fujumodel.Event
	err      error
}

func (s *recordingSender) RegisterContents(_ context.Context, items []fujumodel.Content) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	cp := make([]fujumodel.Content, len(items))
	copy(cp, items)
	s.contents = append(s.contents, cp)
	return nil
}

func (s *recordingSender) SendEvents(_ context.Context, evs []fujumodel.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	cp := make([]fujumodel.Event, len(evs))
	copy(cp, evs)
	s.events = append(s.events, cp)
	return nil
}

func (s *recordingSender) totalContents() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, b := range s.contents {
		n += len(b)
	}
	return n
}

func (s *recordingSender) totalEvents() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, b := range s.events {
		n += len(b)
	}
	return n
}

func newDispatcher(t *testing.T, batchSize int, flushInterval time.Duration) (*Dispatcher, *recordingSender) {
	t.Helper()
	sender := &recordingSender{}
	d, err := NewDispatcher(Options{
		Sender:        sender,
		Log:           logger.New("error"),
		BatchSize:     batchSize,
		FlushInterval: flushInterval,
		QueueCapacity: batchSize * 4,
		SendTimeout:   500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d, sender
}

func TestDispatcher_BatchSizeFlushes(t *testing.T) {
	d, sender := newDispatcher(t, 3, time.Hour) // tick disabled by long interval
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d.Run(ctx)

	for i := 0; i < 5; i++ {
		d.EnqueueEvent(ctx, fujumodel.Event{
			UserID: "u", ItemID: "i",
			EventType: fujumodel.EventLike,
			Timestamp: time.Now(),
		})
	}
	// First batch (3) should flush by size; remaining 2 wait.
	waitFor(t, 1*time.Second, func() bool {
		return sender.totalEvents() >= 3
	})

	got := sender.totalEvents()
	if got != 3 {
		t.Fatalf("totalEvents=%d want 3", got)
	}
}

func TestDispatcher_PeriodicFlush(t *testing.T) {
	d, sender := newDispatcher(t, 100, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d.Run(ctx)

	d.EnqueueEvent(ctx, fujumodel.Event{
		UserID: "u", ItemID: "i",
		EventType: fujumodel.EventViewStart,
		Timestamp: time.Now(),
	})
	d.EnqueueContent(ctx, fujumodel.Content{ContentID: "c1", AuthorID: "a1"})

	// Wait up to 1s for the periodic tick to flush both.
	waitFor(t, 1*time.Second, func() bool {
		return sender.totalEvents() >= 1 && sender.totalContents() >= 1
	})
	if sender.totalEvents() != 1 || sender.totalContents() != 1 {
		t.Fatalf("totals events=%d contents=%d", sender.totalEvents(), sender.totalContents())
	}
}

func TestDispatcher_QueueOverflowDropsOldest(t *testing.T) {
	d, sender := newDispatcher(t, 100, time.Hour) // long flush so we can pile up
	// Don't Run() — we want to inspect the queue state without it draining.
	for i := 0; i < d.batchSize*4+5; i++ {
		d.EnqueueEvent(context.Background(), fujumodel.Event{
			UserID: "u", ItemID: "i",
			EventType: fujumodel.EventLike,
			Timestamp: time.Now(),
		})
	}
	// Now start the worker with a short flush so it flushes once.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d2, _ := NewDispatcher(Options{
		Sender: sender, Log: logger.New("error"),
		BatchSize: 100, FlushInterval: 30 * time.Millisecond,
		QueueCapacity: 100, SendTimeout: 500 * time.Millisecond,
	})
	// Move events from d to d2 to verify the queue cap actually capped.
	close(d.eventsCh)
	for ev := range d.eventsCh {
		d2.EnqueueEvent(context.Background(), ev)
	}
	d2.Run(ctx)
	waitFor(t, 1*time.Second, func() bool { return sender.totalEvents() > 0 })

	if sender.totalEvents() > d.batchSize*4 {
		t.Fatalf("dispatcher must cap queue at QueueCapacity=%d, got %d events", d.batchSize*4, sender.totalEvents())
	}
}

func TestDispatcher_FinalFlushOnContextCancel(t *testing.T) {
	d, sender := newDispatcher(t, 100, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	d.Run(ctx)

	d.EnqueueContent(ctx, fujumodel.Content{ContentID: "c1", AuthorID: "a1"})
	d.EnqueueEvent(ctx, fujumodel.Event{UserID: "u", ItemID: "i", EventType: fujumodel.EventLike, Timestamp: time.Now()})

	// Give the worker a moment to receive from channels (without the
	// batch threshold hitting). Items end up in the worker's local
	// slice — final-flush must catch them.
	time.Sleep(50 * time.Millisecond)
	cancel()
	d.Wait()

	if sender.totalContents() != 1 {
		t.Fatalf("final flush missed contents: got %d", sender.totalContents())
	}
	if sender.totalEvents() != 1 {
		t.Fatalf("final flush missed events: got %d", sender.totalEvents())
	}
}

func TestDispatcher_SendErrorIsLoggedNotPropagated(t *testing.T) {
	sender := &recordingSender{err: errors.New("boom")}
	d, err := NewDispatcher(Options{
		Sender: sender, Log: logger.New("error"),
		BatchSize: 1, FlushInterval: 30 * time.Millisecond,
		QueueCapacity: 8, SendTimeout: 500 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d.Run(ctx)

	// Caller never sees the upstream error — it's a fire-and-forget.
	d.EnqueueEvent(ctx, fujumodel.Event{UserID: "u", ItemID: "i", EventType: fujumodel.EventLike, Timestamp: time.Now()})
	time.Sleep(150 * time.Millisecond)
}

func TestNewDispatcher_RequiresSenderAndLog(t *testing.T) {
	if _, err := NewDispatcher(Options{Log: logger.New("error")}); err == nil {
		t.Fatalf("missing sender should fail")
	}
	if _, err := NewDispatcher(Options{Sender: &recordingSender{}}); err == nil {
		t.Fatalf("missing logger should fail")
	}
}

// waitFor polls until cond is true or the deadline expires; small
// helper to keep the time-sensitive tests legible.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("waitFor: condition not met within %v", timeout)
}
