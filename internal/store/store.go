// Package store owns the plugin's files: state.json (the only channel between
// the actions, the renderer and the daemon) and the user's config.toml.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Wahajolla/herdrmoro/internal/timer"
)

// Store is state.json and its lock in one directory, normally
// $HERDR_PLUGIN_STATE_DIR.
type Store struct {
	Dir string
}

func (s Store) path() string { return filepath.Join(s.Dir, "state.json") }

// Load reads the state without the lock. Writes are atomic renames, so a reader
// always sees a whole file. A missing file is the idle state.
func (s Store) Load() (timer.State, error) {
	var st timer.State
	data, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(data, &st)
}

// Update applies fn to the current state under an exclusive lock on
// state.lock and writes the result back atomically.
func (s Store) Update(fn func(timer.State) timer.State) (timer.State, error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return timer.State{}, err
	}
	unlock, err := Lock(filepath.Join(s.Dir, "state.lock"))
	if err != nil {
		return timer.State{}, err
	}
	defer unlock()

	cur, err := s.Load()
	if err != nil {
		return timer.State{}, err
	}
	next := fn(cur)
	if next == cur {
		return next, nil
	}
	data, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return timer.State{}, err
	}
	tmp, err := os.CreateTemp(s.Dir, "state-*.json")
	if err != nil {
		return timer.State{}, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return timer.State{}, err
	}
	if err := tmp.Close(); err != nil {
		return timer.State{}, err
	}
	return next, os.Rename(tmp.Name(), s.path())
}

// Lock blocks until it holds an exclusive flock on path, creating the file if
// needed. The returned func releases it.
func Lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}

// TryLock is Lock without blocking: ok is false when another process holds it.
func TryLock(path string) (unlock func(), ok bool, err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { f.Close() }, true, nil
}

// LoadConfig reads config.toml from dir, normally $HERDR_PLUGIN_CONFIG_DIR.
// Every key is a whole number on a `key = value` line; a missing file, an
// unknown key or a bad value falls back to the default for that key.
func LoadConfig(dir string) timer.Config {
	c := timer.Config{Focus: 25 * time.Minute, ShortBreak: 5 * time.Minute, LongBreak: 15 * time.Minute, SessionsBeforeLong: 4}
	f, err := os.Open(filepath.Join(dir, "config.toml"))
	if err != nil {
		return c
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil || n <= 0 {
			continue
		}
		switch strings.TrimSpace(key) {
		case "focus_minutes":
			c.Focus = time.Duration(n) * time.Minute
		case "short_break_minutes":
			c.ShortBreak = time.Duration(n) * time.Minute
		case "long_break_minutes":
			c.LongBreak = time.Duration(n) * time.Minute
		case "sessions_before_long":
			c.SessionsBeforeLong = n
		}
	}
	return c
}
