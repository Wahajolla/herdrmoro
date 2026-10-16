# herdrmoro

A pomodoro timer for [herdr](https://herdr.dev) that lives in the tab bar.

```
 1 · api  2 · web  3 · docs                                     🍅 18:40
```

One global timer runs the classic cycle: 25 minutes of focus, a 5-minute break, and a 15-minute break after every fourth focus. When a phase ends you get a herdr notification, and the next phase waits until you start it.

| Tab bar | Means |
|---|---|
| `🍅 18:40` | focus running |
| `☕ 04:10` / `🌴 14:00` | short / long break running |
| `⏸ 🍅 18:40` | paused |
| `☕ ready ▶` | phase over; start the next one when ready |
| _(nothing)_ | idle |

## Install

Requires herdr 0.8.2 or newer on macOS or Linux, and Go to build the plugin.

```sh
herdr plugin install Wahajolla/herdrmoro --ref v0.1.0
```

## Set up

The plugin never edits your herdr config. Add these to `~/.config/herdr/config.toml` yourself, then run `herdr server reload-config`.

**Required: show the timer in the tab bar.** If you already have a `tab_bar_right` list, append this entry to it.

```toml
[ui]
tab_bar_right = [
  { type = "command", command = "~/.local/state/herdr/plugins/herdrmoro/bin/herdrmoro render", interval_seconds = 1, timeout_seconds = 1 },
]
```

The entry stays empty until the plugin has run once. That happens the first time you start the timer, or when herdr restarts.

**Recommended: turn on notifications.** herdr's toasts are off by default. With them off, the tab bar changing to `ready ▶` is the only sign that a phase ended.

```toml
[ui.toast]
delivery = "herdr"   # or "terminal" / "system"
```

**Optional: keybindings.** The plugin registers none. These keys don't clash with herdr's or macOS's defaults, but some terminals keep ⌘ shortcuts for themselves.

```toml
[[keys.command]]
key = "cmd+alt+p"
type = "plugin_action"
command = "herdrmoro.toggle"

[[keys.command]]
key = "cmd+alt+s"
type = "plugin_action"
command = "herdrmoro.skip"

[[keys.command]]
key = "cmd+alt+r"
type = "plugin_action"
command = "herdrmoro.reset"
```

## Use

Run the actions from herdr's action list or Omni:

- **Pomodoro: Start / pause** starts focus when idle, pauses and resumes, and starts the waiting phase at `ready ▶`.
- **Pomodoro: Skip phase** ends the current phase early, or skips the one waiting. A skipped focus still counts toward the long break.
- **Pomodoro: Reset** goes idle and starts the cycle over.

When herdr restarts or the machine sleeps, a running timer pauses at the moment herdr was last alive. Start it again when you're back.

## Configure

Optional. Create `config.toml` in the folder `herdr plugin config-dir herdrmoro` prints (normally `~/.config/herdr/plugins/config/herdrmoro/`). Any key you leave out keeps its default:

```toml
focus_minutes        = 25
short_break_minutes  = 5
long_break_minutes   = 15
sessions_before_long = 4
```

Changes apply from the next phase you start.

## Upgrade

Install again with the new tag:

```sh
herdr plugin install Wahajolla/herdrmoro --ref v0.2.0
```

Your timer state and config live outside the plugin folder, so they're kept.

## Troubleshooting

The plugin keeps its state in `~/.local/state/herdr/plugins/herdrmoro/`. `daemon.log` there records notifications that weren't shown, for example because toasts are off.
