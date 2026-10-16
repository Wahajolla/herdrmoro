package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Wahajolla/herdrmoro/internal/daemon"
	"github.com/Wahajolla/herdrmoro/internal/store"
	"github.com/Wahajolla/herdrmoro/internal/timer"
)

// runDaemon is the startup process. With adopt, it was started by an action
// because herdr only runs startup hooks when its server starts, so nothing
// restarted and nothing is paused.
func runDaemon(stateDir, configDir string, adopt bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// herdr never kills startup processes, so the daemon leaves when herdr's
	// socket closes.
	go func() {
		watchHerdr(ctx)
		cancel()
	}()

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	if root := os.Getenv("HERDR_PLUGIN_ROOT"); root != "" {
		if err := daemon.PublishBinary(stateDir, root); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(stateDir, configDirFile), []byte(configDir+"\n"), 0o644); err != nil {
		return err
	}

	// A held lock means another daemon is still running: a live handoff.
	lockPath := filepath.Join(stateDir, "daemon.lock")
	unlock, ok, err := store.TryLock(lockPath)
	if err != nil {
		return err
	}
	handoff := !ok || adopt
	if !ok {
		locked := make(chan func(), 1)
		go func() {
			if u, err := store.Lock(lockPath); err == nil {
				locked <- u
			}
		}()
		select {
		case unlock = <-locked:
		case <-ctx.Done():
			return nil
		}
	}
	defer unlock()

	logFile, err := os.OpenFile(filepath.Join(stateDir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	d := &daemon.Daemon{
		Store:  store.Store{Dir: stateDir},
		Config: func() timer.Config { return store.LoadConfig(configDir) },
		Now:    time.Now,
		Notify: herdrNotify,
		Log:    logFile,
	}
	if err := d.Start(handoff); err != nil {
		return err
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
			if err := d.Tick(); err != nil {
				fmt.Fprintln(logFile, time.Now().Format(time.RFC3339), "tick:", err)
			}
		}
	}
}

// ensureDaemon starts a daemon in the background when none holds
// daemon.lock: right after install or link, before herdr's server has
// restarted and run the startup hook.
func ensureDaemon(stateDir string) error {
	if os.Getenv("HERDR_SOCKET_PATH") == "" {
		return nil // not run by herdr, so a daemon would have no way to exit
	}
	unlock, free, err := store.TryLock(filepath.Join(stateDir, "daemon.lock"))
	if err != nil || !free {
		return err
	}
	unlock()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "daemon", "--adopt")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// watchHerdr holds an event subscription on herdr's socket and returns when
// it closes. Without a socket it waits for ctx.
func watchHerdr(ctx context.Context) {
	path := os.Getenv("HERDR_SOCKET_PATH")
	if path == "" {
		<-ctx.Done()
		return
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		conn.Close()
	}()
	req := `{"id":"herdrmoro","method":"events.subscribe","params":{"subscriptions":[{"type":"workspace.created"}]}}` + "\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		return
	}
	r := bufio.NewReader(conn)
	for {
		if _, err := r.ReadBytes('\n'); err != nil {
			return
		}
	}
}

func herdrNotify(title, body string) (string, error) {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "notification", "show", title, "--body", body, "--sound", "done").Output()
	if err != nil {
		return "", err
	}
	var resp struct {
		Result struct {
			Reason string `json:"reason"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", fmt.Errorf("unexpected herdr output %q: %w", out, err)
	}
	return resp.Result.Reason, nil
}
