package daemon

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wahajolla/herdrmoro/internal/store"
	"github.com/Wahajolla/herdrmoro/internal/timer"
)

var (
	t0  = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	cfg = timer.Config{Focus: 25 * time.Minute, ShortBreak: 5 * time.Minute, LongBreak: 15 * time.Minute, SessionsBeforeLong: 4}
)

type sent struct{ title, body string }

// harness is a daemon over a temp state dir, a hand-driven clock and a fake
// notifier that answers with the queued reasons, then "shown".
type harness struct {
	t       *testing.T
	d       *Daemon
	now     time.Time
	replies []string
	sent    []sent
	log     bytes.Buffer
}

func newHarness(t *testing.T) *harness {
	h := &harness{t: t, now: t0}
	h.d = h.daemon(t.TempDir())
	return h
}

func (h *harness) daemon(dir string) *Daemon {
	return &Daemon{
		Store:  store.Store{Dir: dir},
		Config: func() timer.Config { return cfg },
		Now:    func() time.Time { return h.now },
		Notify: func(title, body string) (string, error) {
			h.sent = append(h.sent, sent{title, body})
			if len(h.replies) == 0 {
				return "shown", nil
			}
			r := h.replies[0]
			h.replies = h.replies[1:]
			return r, nil
		},
		Log: &h.log,
	}
}

func (h *harness) state() timer.State {
	s, err := h.d.Store.Load()
	if err != nil {
		h.t.Fatal(err)
	}
	return s
}

func (h *harness) act(fn func(timer.State) timer.State) {
	if _, err := h.d.Store.Update(fn); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) start() {
	h.act(func(s timer.State) timer.State { return timer.Toggle(s, cfg, h.now) })
}

// tickEverySecond advances the clock in 1 s ticks, like the real loop.
func (h *harness) tickEverySecond(d time.Duration) {
	for end := h.now.Add(d); h.now.Before(end); {
		h.now = h.now.Add(time.Second)
		if err := h.d.Tick(); err != nil {
			h.t.Fatal(err)
		}
	}
}

func (h *harness) render() string { return timer.Render(h.state(), cfg, h.now) }

func TestNotifiesOnceWhenFocusEnds(t *testing.T) {
	h := newHarness(t)
	if err := h.d.Start(false); err != nil {
		t.Fatal(err)
	}
	h.start()
	h.tickEverySecond(26 * time.Minute)
	want := []sent{{"🍅 Focus done", "Short break next (5 min). Start it when ready."}}
	if len(h.sent) != 1 || h.sent[0] != want[0] {
		t.Fatalf("sent = %+v, want %+v", h.sent, want)
	}
	if !h.state().Notified {
		t.Fatal("state not marked notified")
	}
}

func TestNotificationText(t *testing.T) {
	cases := []struct {
		name  string
		setup func(s timer.State, now time.Time) timer.State
		want  sent
	}{
		{"focus before a long break", func(s timer.State, now time.Time) timer.State {
			s.Sessions = 3
			return timer.Toggle(s, cfg, now)
		}, sent{"🍅 Focus done", "Long break next (15 min). Start it when ready."}},
		{"short break", func(s timer.State, now time.Time) timer.State {
			return timer.Toggle(timer.Skip(s, cfg, now), cfg, now)
		}, sent{"☕ Break over", "Focus next (25 min). Start it when ready."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.act(func(s timer.State) timer.State { return tc.setup(s, h.now) })
			h.tickEverySecond(26 * time.Minute)
			if len(h.sent) != 1 || h.sent[0] != tc.want {
				t.Fatalf("sent = %+v, want %+v", h.sent, tc.want)
			}
		})
	}
}

func TestRetriesBusyThenShows(t *testing.T) {
	h := newHarness(t)
	h.replies = []string{"busy", "rate_limited", "busy"}
	h.start()
	h.tickEverySecond(25*time.Minute + 10*time.Second)
	if len(h.sent) != 4 {
		t.Fatalf("attempts = %d, want 4 (three refusals, then shown)", len(h.sent))
	}
	if !h.state().Notified {
		t.Fatal("state not marked notified")
	}
	if h.log.Len() != 0 {
		t.Fatalf("log = %q, want nothing", h.log.String())
	}
}

func TestGivesUpAfterSixtySeconds(t *testing.T) {
	h := newHarness(t)
	h.replies = make([]string, 1000)
	for i := range h.replies {
		h.replies[i] = "busy"
	}
	h.start()
	h.tickEverySecond(30 * time.Minute)
	if n := len(h.sent); n < 55 || n > 62 {
		t.Fatalf("attempts = %d, want about 60 (one per tick for 60 s)", n)
	}
	if !h.state().Notified {
		t.Fatal("state not marked notified after giving up")
	}
	if !strings.Contains(h.log.String(), "gave up") {
		t.Fatalf("log = %q, want a give-up line", h.log.String())
	}
}

func TestStopsOnFinalRefusals(t *testing.T) {
	for _, reason := range []string{"disabled", "no_foreground_client"} {
		t.Run(reason, func(t *testing.T) {
			h := newHarness(t)
			h.replies = []string{reason}
			h.start()
			h.tickEverySecond(26 * time.Minute)
			if len(h.sent) != 1 || !h.state().Notified {
				t.Fatalf("attempts = %d notified = %v, want 1 attempt then notified", len(h.sent), h.state().Notified)
			}
		})
	}
}

func TestLogsDisabledOnlyOnce(t *testing.T) {
	h := newHarness(t)
	h.replies = []string{"disabled", "disabled"}
	h.start()
	h.tickEverySecond(26 * time.Minute)
	h.act(func(s timer.State) timer.State { return timer.Toggle(s, cfg, h.now) }) // break
	h.tickEverySecond(6 * time.Minute)
	if len(h.sent) != 2 {
		t.Fatalf("attempts = %d, want 2", len(h.sent))
	}
	if n := strings.Count(h.log.String(), "disabled"); n != 1 {
		t.Fatalf("log = %q, want exactly one disabled line", h.log.String())
	}
}

func TestHeartbeatWhileRunning(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.tickEverySecond(3 * time.Second)
	info, err := os.Stat(filepath.Join(h.d.Store.Dir, "heartbeat"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(h.now) {
		t.Fatalf("heartbeat mtime = %v, want %v", info.ModTime(), h.now)
	}
}

func TestColdStartFreezesAtLastHeartbeat(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.tickEverySecond(6*time.Minute + 20*time.Second) // herdr dies here
	h.now = h.now.Add(2 * time.Hour)                  // and comes back later
	cold := h.daemon(h.d.Store.Dir)
	if err := cold.Start(false); err != nil {
		t.Fatal(err)
	}
	if got := h.render(); got != "⏸ 🍅 18:40" {
		t.Fatalf("Render after cold start = %q, want %q", got, "⏸ 🍅 18:40")
	}
}

func TestColdStartAfterPhaseRanOutDoesNotNotify(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.tickEverySecond(24*time.Minute + 59*time.Second)
	h.now = h.now.Add(time.Hour)
	// Heartbeat said 00:01 left; freezing leaves a paused second, so make the
	// last heartbeat land after the end instead.
	h.act(func(s timer.State) timer.State { s.EndsAt = s.EndsAt.Add(-5 * time.Second); return s })
	cold := h.daemon(h.d.Store.Dir)
	if err := cold.Start(false); err != nil {
		t.Fatal(err)
	}
	h.d = cold
	h.tickEverySecond(5 * time.Second)
	if got := h.render(); got != "☕ ready ▶" {
		t.Fatalf("Render = %q, want %q", got, "☕ ready ▶")
	}
	if len(h.sent) != 0 {
		t.Fatalf("sent = %+v, want nothing", h.sent)
	}
}

func TestColdStartMidRetryDoesNotNotifyAgain(t *testing.T) {
	h := newHarness(t)
	h.replies = []string{"busy", "busy", "busy"}
	h.start()
	h.tickEverySecond(25*time.Minute + 2*time.Second) // two busy attempts, then herdr dies
	attempts := len(h.sent)
	h.now = h.now.Add(time.Hour)
	cold := h.daemon(h.d.Store.Dir)
	if err := cold.Start(false); err != nil {
		t.Fatal(err)
	}
	h.d = cold
	h.tickEverySecond(time.Minute)
	if len(h.sent) != attempts {
		t.Fatalf("attempts after cold start = %d, want still %d", len(h.sent), attempts)
	}
}

func TestHandoffPausesNothing(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.tickEverySecond(time.Minute)
	next := h.daemon(h.d.Store.Dir)
	if err := next.Start(true); err != nil {
		t.Fatal(err)
	}
	if got := h.render(); got != "🍅 24:00" {
		t.Fatalf("Render after handoff = %q, want %q", got, "🍅 24:00")
	}
}

func TestSleepFreezesAtLastTick(t *testing.T) {
	h := newHarness(t)
	h.start()
	h.tickEverySecond(6*time.Minute + 20*time.Second)
	h.now = h.now.Add(40 * time.Minute) // lid closed
	if err := h.d.Tick(); err != nil {
		t.Fatal(err)
	}
	if got := h.render(); got != "⏸ 🍅 18:40" {
		t.Fatalf("Render after sleep = %q, want %q", got, "⏸ 🍅 18:40")
	}
	if len(h.sent) != 0 {
		t.Fatalf("sent = %+v, want nothing", h.sent)
	}
}

func TestPublishBinary(t *testing.T) {
	state, root := t.TempDir(), t.TempDir()
	for _, r := range []string{root, t.TempDir()} { // second run: re-pointed after an upgrade
		if err := PublishBinary(state, r); err != nil {
			t.Fatal(err)
		}
		got, err := os.Readlink(filepath.Join(state, "bin", "herdrmoro"))
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(r, "herdrmoro"); got != want {
			t.Fatalf("symlink -> %q, want %q", got, want)
		}
	}
}
