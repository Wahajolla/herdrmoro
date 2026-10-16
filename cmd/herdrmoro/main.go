// Command herdrmoro is the whole plugin: the actions, the tab-bar renderer and
// the startup daemon, as subcommands of one binary.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wahajolla/herdrmoro/internal/store"
	"github.com/Wahajolla/herdrmoro/internal/timer"
)

const pluginID = "herdrmoro"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: herdrmoro daemon | toggle | skip | reset | render")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "herdrmoro:", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	stateDir, configDir := dirs()
	st := store.Store{Dir: stateDir}
	switch cmd {
	case "toggle", "skip", "reset":
		cfg := store.LoadConfig(configDir)
		_, err := st.Update(func(s timer.State) timer.State {
			now := time.Now()
			switch cmd {
			case "toggle":
				return timer.Toggle(s, cfg, now)
			case "skip":
				return timer.Skip(s, cfg, now)
			default:
				return timer.Reset(s)
			}
		})
		if err != nil {
			return err
		}
		return ensureDaemon(stateDir)
	case "render":
		s, err := st.Load()
		if err != nil {
			return err
		}
		fmt.Println(timer.Render(s, store.LoadConfig(configDir), time.Now()))
		return nil
	case "daemon":
		return runDaemon(stateDir, configDir, len(args) > 0 && args[0] == "--adopt")
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// configDirFile is where the daemon records herdr's config dir for processes
// that run without the plugin environment, like the tab-bar command.
const configDirFile = "config_dir"

// dirs finds the state and config dirs. Actions and the daemon get them from
// herdr's environment. The tab-bar command runs without it, through the
// symlink at <state dir>/bin/herdrmoro, so the state dir is found from that
// path and the config dir from what the daemon recorded there.
func dirs() (stateDir, configDir string) {
	stateDir = os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if stateDir == "" {
		if bin := filepath.Dir(os.Args[0]); filepath.Base(bin) == "bin" && filepath.IsAbs(bin) {
			stateDir = filepath.Dir(bin)
		} else {
			home, _ := os.UserHomeDir()
			stateDir = filepath.Join(home, ".local", "state", "herdr", "plugins", pluginID)
		}
	}
	configDir = os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	if configDir == "" {
		if b, err := os.ReadFile(filepath.Join(stateDir, configDirFile)); err == nil {
			configDir = strings.TrimSpace(string(b))
		}
	}
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config", "herdr", "plugins", "config", pluginID)
	}
	return stateDir, configDir
}
