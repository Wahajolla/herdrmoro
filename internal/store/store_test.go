package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wahajolla/herdrmoro/internal/timer"
)

func TestMissingStateIsIdle(t *testing.T) {
	s, err := Store{Dir: t.TempDir()}.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s != (timer.State{}) {
		t.Fatalf("Load = %+v, want idle", s)
	}
}

func TestUpdateRoundTrips(t *testing.T) {
	st := Store{Dir: filepath.Join(t.TempDir(), "not", "yet", "created")}
	ends := time.Date(2026, 10, 16, 9, 25, 0, 0, time.UTC)
	want := timer.State{Phase: timer.Focus, Status: timer.Running, Sessions: 2, EndsAt: ends, Notified: true}
	if _, err := st.Update(func(timer.State) timer.State { return want }); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !got.EndsAt.Equal(ends) {
		t.Fatalf("EndsAt = %v, want %v", got.EndsAt, ends)
	}
	got.EndsAt = ends
	if got != want {
		t.Fatalf("Load = %+v, want %+v", got, want)
	}
}

func TestConcurrentUpdatesAreNotLost(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	const writers = 50
	var wg sync.WaitGroup
	for range writers {
		wg.Go(func() {
			if _, err := st.Update(func(s timer.State) timer.State { s.Sessions++; return s }); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	got, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Sessions != writers {
		t.Fatalf("Sessions = %d, want %d", got.Sessions, writers)
	}
}

func TestConfigDefaults(t *testing.T) {
	want := timer.Config{Focus: 25 * time.Minute, ShortBreak: 5 * time.Minute, LongBreak: 15 * time.Minute, SessionsBeforeLong: 4}
	if got := LoadConfig(t.TempDir()); got != want {
		t.Fatalf("LoadConfig(no file) = %+v, want %+v", got, want)
	}
}

func TestConfigOverridesSomeKeys(t *testing.T) {
	dir := t.TempDir()
	toml := "# my pomodoro\nfocus_minutes = 50   # long focus\nsessions_before_long=3\nunknown_key = 7\nshort_break_minutes = oops\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	want := timer.Config{Focus: 50 * time.Minute, ShortBreak: 5 * time.Minute, LongBreak: 15 * time.Minute, SessionsBeforeLong: 3}
	if got := LoadConfig(dir); got != want {
		t.Fatalf("LoadConfig = %+v, want %+v", got, want)
	}
}
