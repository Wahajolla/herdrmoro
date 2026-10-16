package timer

import (
	"testing"
	"time"
)

var (
	t0  = time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	cfg = Config{Focus: 25 * time.Minute, ShortBreak: 5 * time.Minute, LongBreak: 15 * time.Minute, SessionsBeforeLong: 4}
)

func at(d time.Duration) time.Time { return t0.Add(d) }

func TestIdleRendersNothing(t *testing.T) {
	if got := Render(State{}, cfg, t0); got != "" {
		t.Fatalf("Render(idle) = %q, want empty", got)
	}
}

func TestToggleFromIdleStartsFocus(t *testing.T) {
	s := Toggle(State{}, cfg, t0)
	if got := Render(s, cfg, t0); got != "🍅 25:00" {
		t.Fatalf("Render = %q, want %q", got, "🍅 25:00")
	}
	if got := Render(s, cfg, at(6*time.Minute+20*time.Second)); got != "🍅 18:40" {
		t.Fatalf("Render = %q, want %q", got, "🍅 18:40")
	}
}

// run applies steps in order, each at its own offset from t0.
type step struct {
	at time.Duration
	do func(State, Config, time.Time) State
}

func run(steps ...step) State {
	var s State
	for _, st := range steps {
		s = st.do(s, cfg, at(st.at))
	}
	return s
}

func skip(s State, c Config, now time.Time) State   { return Skip(s, c, now) }
func reset(s State, _ Config, _ time.Time) State    { return Reset(s) }
func toggle(s State, c Config, now time.Time) State { return Toggle(s, c, now) }

func TestStartPauseTable(t *testing.T) {
	cases := []struct {
		name  string
		steps []step
		when  time.Duration
		want  string
	}{
		{"running pauses", []step{{0, toggle}, {6*time.Minute + 20*time.Second, toggle}}, 10 * time.Minute, "⏸ 🍅 18:40"},
		{"paused resumes", []step{{0, toggle}, {6*time.Minute + 20*time.Second, toggle}, {10 * time.Minute, toggle}}, 11 * time.Minute, "🍅 17:40"},
		{"focus over waits for a short break", []step{{0, toggle}}, 25 * time.Minute, "☕ ready ▶"},
		{"ready starts the waiting break", []step{{0, toggle}, {26 * time.Minute, toggle}}, 26*time.Minute + 50*time.Second, "☕ 04:10"},
		{"break over waits for focus", []step{{0, toggle}, {25 * time.Minute, toggle}}, 30 * time.Minute, "🍅 ready ▶"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Render(run(tc.steps...), cfg, at(tc.when)); got != tc.want {
				t.Fatalf("Render = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLongBreakAfterFourFocusSessions(t *testing.T) {
	s := run(
		step{0, toggle}, step{25 * time.Minute, toggle}, // focus 1, break 1
		step{30 * time.Minute, toggle}, step{55 * time.Minute, toggle}, // focus 2, break 2
		step{60 * time.Minute, toggle}, step{85 * time.Minute, toggle}, // focus 3, break 3
		step{90 * time.Minute, toggle}, // focus 4
	)
	if got := Render(s, cfg, at(116*time.Minute)); got != "🌴 ready ▶" {
		t.Fatalf("after 4th focus Render = %q, want %q", got, "🌴 ready ▶")
	}
	s = Toggle(s, cfg, at(116*time.Minute))
	if got := Render(s, cfg, at(117*time.Minute)); got != "🌴 14:00" {
		t.Fatalf("long break Render = %q, want %q", got, "🌴 14:00")
	}
}

func TestSkip(t *testing.T) {
	cases := []struct {
		name  string
		steps []step
		want  string
	}{
		{"running focus lands on a waiting break", []step{{0, toggle}, {time.Minute, skip}}, "☕ ready ▶"},
		{"paused focus lands on a waiting break", []step{{0, toggle}, {time.Minute, toggle}, {2 * time.Minute, skip}}, "☕ ready ▶"},
		{"running break lands on waiting focus", []step{{0, toggle}, {time.Minute, skip}, {2 * time.Minute, toggle}, {3 * time.Minute, skip}}, "🍅 ready ▶"},
		{"idle skips the waiting focus", []step{{0, skip}}, "☕ ready ▶"},
		{"ready skips the waiting break", []step{{0, toggle}, {time.Minute, skip}, {2 * time.Minute, skip}}, "🍅 ready ▶"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := run(tc.steps...)
			if got := Render(s, cfg, at(time.Hour)); got != tc.want {
				t.Fatalf("Render = %q, want %q", got, tc.want)
			}
			if !s.Notified {
				t.Fatal("skip must not owe a notification")
			}
		})
	}
}

func TestSkippedFocusCountsTowardLongBreak(t *testing.T) {
	// focus skipped, break skipped, three times; the 4th focus runs out.
	s := run(
		step{0, skip}, step{0, skip},
		step{0, toggle}, step{time.Minute, skip}, step{time.Minute, skip},
		step{2 * time.Minute, skip}, step{2 * time.Minute, skip},
		step{3 * time.Minute, toggle},
	)
	if got := Render(s, cfg, at(28*time.Minute)); got != "🌴 ready ▶" {
		t.Fatalf("Render = %q, want %q", got, "🌴 ready ▶")
	}
}

func TestReset(t *testing.T) {
	for name, steps := range map[string][]step{
		"from running": {{0, toggle}},
		"from paused":  {{0, toggle}, {time.Minute, toggle}},
		"from ready":   {{0, skip}},
		"from idle":    {},
	} {
		t.Run(name, func(t *testing.T) {
			s := Reset(run(steps...))
			if got := Render(s, cfg, at(time.Hour)); got != "" {
				t.Fatalf("Render = %q, want empty", got)
			}
		})
	}
	// The cycle starts over: three focus sessions, reset, then four more are
	// needed before a long break.
	s := run(step{0, skip}, step{0, skip}, step{0, skip}, step{0, skip}, step{0, skip}, step{0, skip}, step{0, reset}, step{0, skip})
	if got := Render(s, cfg, t0); got != "☕ ready ▶" {
		t.Fatalf("first focus after reset Render = %q, want %q", got, "☕ ready ▶")
	}
}

func TestRanOutPhaseOwesNotification(t *testing.T) {
	s := Settle(Toggle(State{}, cfg, t0), cfg, at(25*time.Minute))
	if s.Status != Ready || s.Notified {
		t.Fatalf("Settle = %+v, want Ready owing a notification", s)
	}
}

func TestPauseAt(t *testing.T) {
	running := Toggle(State{}, cfg, t0)
	if got := Render(PauseAt(running, cfg, at(6*time.Minute+20*time.Second)), cfg, at(time.Hour)); got != "⏸ 🍅 18:40" {
		t.Fatalf("frozen Render = %q, want %q", got, "⏸ 🍅 18:40")
	}
	done := PauseAt(running, cfg, at(26*time.Minute))
	if got := Render(done, cfg, at(time.Hour)); got != "☕ ready ▶" || !done.Notified {
		t.Fatalf("ran-out PauseAt = %q notified=%v, want %q with no notification owed", got, done.Notified, "☕ ready ▶")
	}
	owed := Settle(running, cfg, at(25*time.Minute))
	if !PauseAt(owed, cfg, at(26*time.Minute)).Notified {
		t.Fatal("PauseAt must drop a notification owed from before the restart")
	}
}
