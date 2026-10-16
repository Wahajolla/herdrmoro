// Package daemon is the startup process: it sends the phase-end notification,
// keeps the heartbeat, and pauses the timer when herdr restarts or the machine
// sleeps. The display and the actions work without it.
package daemon

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/Wahajolla/herdrmoro/internal/store"
	"github.com/Wahajolla/herdrmoro/internal/timer"
)

const (
	// sleepGap is how far the clock may jump between two 1 s ticks before the
	// daemon decides the machine slept.
	sleepGap = 5 * time.Second
	// retryWindow is how long busy or rate-limited notifications are retried.
	retryWindow = 60 * time.Second
)

// Notifier shows a herdr notification and returns herdr's reason: shown,
// disabled, busy, rate_limited or no_foreground_client.
type Notifier func(title, body string) (reason string, err error)

type Daemon struct {
	Store  store.Store
	Config func() timer.Config
	Now    func() time.Time
	Notify Notifier
	Log    io.Writer

	lastTick       time.Time
	retryUntil     time.Time
	loggedDisabled bool
}

func (d *Daemon) heartbeatPath() string { return filepath.Join(d.Store.Dir, "heartbeat") }

// Start runs once before the first tick. On a cold start (herdr itself
// restarted) a running timer is frozen at the last heartbeat and any
// notification still owed is dropped. On a live handoff nothing changes.
func (d *Daemon) Start(handoff bool) error {
	d.lastTick = d.Now()
	if handoff {
		return nil
	}
	last := d.Now()
	if info, err := os.Stat(d.heartbeatPath()); err == nil {
		last = info.ModTime()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	_, err := d.Store.Update(func(s timer.State) timer.State { return timer.PauseAt(s, d.Config(), last) })
	return err
}

// Tick is one pass of the 1 s loop.
func (d *Daemon) Tick() error {
	now := d.Now()
	cfg := d.Config()
	slept := !d.lastTick.IsZero() && now.Sub(d.lastTick) > sleepGap
	last := d.lastTick
	d.lastTick = now

	s, err := d.Store.Update(func(s timer.State) timer.State {
		if slept {
			return timer.PauseAt(s, cfg, last)
		}
		return timer.Settle(s, cfg, now)
	})
	if err != nil {
		return err
	}
	if s.Status == timer.Running {
		if err := d.touchHeartbeat(now); err != nil {
			return err
		}
	}
	if s.Status == timer.Ready && !s.Notified {
		return d.notify(s, cfg, now)
	}
	d.retryUntil = time.Time{}
	return nil
}

func (d *Daemon) touchHeartbeat(now time.Time) error {
	f, err := os.OpenFile(d.heartbeatPath(), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	f.Close()
	return os.Chtimes(d.heartbeatPath(), now, now)
}

func (d *Daemon) notify(s timer.State, cfg timer.Config, now time.Time) error {
	if d.retryUntil.IsZero() {
		d.retryUntil = now.Add(retryWindow)
	}
	title, body := Message(s, cfg)
	reason, err := d.Notify(title, body)
	switch {
	case err == nil && (reason == "busy" || reason == "rate_limited"), err != nil:
		if now.Before(d.retryUntil) {
			return nil
		}
		why := reason
		if err != nil {
			why = err.Error()
		}
		d.logf("gave up on the %q notification after %s: %s", title, retryWindow, why)
	case reason == "disabled":
		if !d.loggedDisabled {
			d.loggedDisabled = true
			d.logf("notifications disabled: herdr toasts are off (ui.toast.delivery = \"off\"), so phase ends are not announced")
		}
	}
	d.retryUntil = time.Time{}
	_, uerr := d.Store.Update(func(cur timer.State) timer.State {
		// Only settle the notification for the phase it was about.
		if cur.Status == timer.Ready && !cur.Notified && cur.Phase == s.Phase && cur.Sessions == s.Sessions {
			cur.Notified = true
		}
		return cur
	})
	return uerr
}

func (d *Daemon) logf(format string, args ...any) {
	if d.Log != nil {
		fmt.Fprintf(d.Log, "%s %s\n", d.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
	}
}

// Message is the fixed notification text for a Ready state: the phase that
// waits names the one that just ended.
func Message(s timer.State, cfg timer.Config) (title, body string) {
	mins := func(p timer.Phase) int { return int(cfg.Duration(p) / time.Minute) }
	switch s.Phase {
	case timer.ShortBreak:
		return "🍅 Focus done", fmt.Sprintf("Short break next (%d min). Start it when ready.", mins(timer.ShortBreak))
	case timer.LongBreak:
		return "🍅 Focus done", fmt.Sprintf("Long break next (%d min). Start it when ready.", mins(timer.LongBreak))
	default:
		return "☕ Break over", fmt.Sprintf("Focus next (%d min). Start it when ready.", mins(timer.Focus))
	}
}

// PublishBinary points <stateDir>/bin/herdrmoro at the installed binary in
// root, so the tab-bar command has a path that survives reinstalls.
func PublishBinary(stateDir, root string) error {
	bin := filepath.Join(stateDir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return err
	}
	link := filepath.Join(bin, "herdrmoro")
	tmp := link + ".tmp"
	os.Remove(tmp)
	if err := os.Symlink(filepath.Join(root, "herdrmoro"), tmp); err != nil {
		return err
	}
	return os.Rename(tmp, link)
}
