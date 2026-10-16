// Package timer is the pomodoro state machine. Every function is pure: it takes
// the state and the current time and returns the new state, so the file store,
// the actions and the daemon all share one set of rules.
package timer

import (
	"fmt"
	"time"
)

type Phase string

const (
	Focus      Phase = "focus"
	ShortBreak Phase = "short_break"
	LongBreak  Phase = "long_break"
)

type Status string

const (
	// Idle is the zero value: never started, or reset. It behaves like Ready
	// with Focus waiting, but renders nothing.
	Idle    Status = ""
	Running Status = "running"
	Paused  Status = "paused"
	// Ready means the previous phase is over and Phase is waiting for a start.
	Ready Status = "ready"
)

type Config struct {
	Focus              time.Duration
	ShortBreak         time.Duration
	LongBreak          time.Duration
	SessionsBeforeLong int
}

func (c Config) Duration(p Phase) time.Duration {
	switch p {
	case ShortBreak:
		return c.ShortBreak
	case LongBreak:
		return c.LongBreak
	default:
		return c.Focus
	}
}

// State is what state.json holds. The time left is stored as timestamps so
// nothing has to tick for readers to stay correct.
type State struct {
	Phase  Phase  `json:"phase,omitempty"`
	Status Status `json:"status,omitempty"`
	// Sessions counts focus phases finished (or skipped) since the last reset.
	Sessions int `json:"sessions"`
	// EndsAt is set while Running.
	EndsAt time.Time `json:"ends_at,omitzero"`
	// RemainingMS is set while Paused.
	RemainingMS int64 `json:"remaining_ms,omitempty"`
	// Notified is false while a Ready state still owes its phase-end
	// notification.
	Notified bool `json:"notified"`
}

func (s State) phase() Phase {
	if s.Phase == "" {
		return Focus
	}
	return s.Phase
}

// Remaining is the time left in the current phase at now.
func (s State) Remaining(now time.Time) time.Duration {
	switch s.Status {
	case Running:
		return max(s.EndsAt.Sub(now), 0)
	case Paused:
		return time.Duration(s.RemainingMS) * time.Millisecond
	default:
		return 0
	}
}

// Settle turns a running phase whose time is up into Ready for the next phase,
// still owing its notification.
func Settle(s State, c Config, now time.Time) State {
	if s.Status == Running && !now.Before(s.EndsAt) {
		s = advance(s, c)
		s.Notified = false
	}
	return s
}

// advance moves to Ready with the phase after the current one. A focus phase
// counts toward the long break however it ended.
func advance(s State, c Config) State {
	next := Focus
	if s.phase() == Focus {
		s.Sessions++
		next = ShortBreak
		if c.SessionsBeforeLong > 0 && s.Sessions%c.SessionsBeforeLong == 0 {
			next = LongBreak
		}
	}
	return State{Phase: next, Status: Ready, Sessions: s.Sessions, Notified: true}
}

// Toggle is the Start / pause action.
func Toggle(s State, c Config, now time.Time) State {
	s = Settle(s, c, now)
	switch s.Status {
	case Running:
		return State{Phase: s.phase(), Status: Paused, Sessions: s.Sessions, RemainingMS: s.Remaining(now).Milliseconds(), Notified: true}
	case Paused:
		return State{Phase: s.phase(), Status: Running, Sessions: s.Sessions, EndsAt: now.Add(s.Remaining(now)), Notified: true}
	default: // Idle or Ready
		return State{Phase: s.phase(), Status: Running, Sessions: s.Sessions, EndsAt: now.Add(c.Duration(s.phase())), Notified: true}
	}
}

// Skip ends the current phase early, or skips the waiting one, without a
// notification.
func Skip(s State, c Config, now time.Time) State {
	return advance(Settle(s, c, now), c)
}

// Reset goes idle and starts the cycle over.
func Reset(State) State {
	return State{Notified: true}
}

// PauseAt freezes a running phase at t, the last moment herdr was known to be
// alive. A phase that had already run out by t counts as complete, with no
// notification owed.
func PauseAt(s State, c Config, t time.Time) State {
	if s.Status == Running {
		if !t.Before(s.EndsAt) {
			return advance(s, c)
		}
		s.Status = Paused
		s.RemainingMS = s.EndsAt.Sub(t).Milliseconds()
		s.EndsAt = time.Time{}
	}
	s.Notified = true
	return s
}

func icon(p Phase) string {
	switch p {
	case ShortBreak:
		return "☕"
	case LongBreak:
		return "🌴"
	default:
		return "🍅"
	}
}

// Render is the one tab-bar line. Idle renders empty so herdr hides the entry.
func Render(s State, c Config, now time.Time) string {
	s = Settle(s, c, now)
	switch s.Status {
	case Running:
		return icon(s.phase()) + " " + mmss(s.Remaining(now))
	case Paused:
		return "⏸ " + icon(s.phase()) + " " + mmss(s.Remaining(now))
	case Ready:
		return icon(s.phase()) + " ready ▶"
	default:
		return ""
	}
}

// mmss rounds up, so a fresh 25-minute phase shows 25:00 and the last
// partial second shows 00:01.
func mmss(d time.Duration) string {
	secs := int64((d + time.Second - 1) / time.Second)
	return fmt.Sprintf("%02d:%02d", secs/60, secs%60)
}
