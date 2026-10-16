# What can a herdr plugin display and do?

Research for [#2](https://github.com/Wahajolla/herdrmoro/issues/2), part of the map [#1](https://github.com/Wahajolla/herdrmoro/issues/1). Researched 2026-10-16 against **herdr 0.8.2** (the local version, protocol 20). Current stable is 0.9.5; differences that matter are called out.

## Sources

Primary sources only. Short names are used in the citations below.

| Short name | Source |
| --- | --- |
| **plugins doc** | `docs/next/website/src/content/docs/plugins.mdx` at tag `v0.8.2` of [herdrdev/herdr](https://github.com/herdrdev/herdr) (linked from https://herdr.dev/llms.txt) |
| **socket doc** | `socket-api.mdx` at `v0.8.2` |
| **config doc** | `configuration.mdx` at `v0.8.2` |
| **schema** | `herdr api schema --json` from the local 0.8.2 binary |
| **source** | herdr Rust source at tag `v0.8.2` (the repo is public) |
| **CLI** | `herdr --help`, `herdr plugin … --help`, `herdr --default-config` from the local binary |
| **omni** | installed `herdr-omni` 0.17.0 (`~/.config/herdr/plugins/github/herdr-omni-*`) |
| **auto-title** | installed `herdr.auto-title` 0.4.0 (`~/.config/herdr/plugins/github/herdr.auto-title-*`) |
| **live** | probes run against the running local 0.8.2 server |

The top-level framing, from the plugins doc: "There is no separate plugin SDK or restricted command set. The entire Herdr CLI is the plugin API." Also: "Runtime action registration and native non-terminal plugin UI are not part of plugin v1."

---

## 1. Can a plugin render text in herdr's own UI?

**There is no plugin-owned UI surface. The exact spot we wanted, the top-right corner of the sidebar's "spaces" header, cannot be reached.** Three herdr-native surfaces can show text a plugin produces, and all three need the user to add a line to `config.toml`.

### The "spaces" header is hard-coded (confirmed)

`render_workspace_list` in `src/ui/sidebar.rs` (source) draws the section header as a fixed literal and draws nothing else on that row:

```rust
Paragraph::new(Line::from(vec![Span::styled(
    " spaces",
    Style::default().fg(p.overlay0).add_modifier(Modifier::BOLD),
)])),
Rect::new(area.x, area.y, area.width, 1),
```

There's no config key, token or API for it. The plugins doc also rules out native non-terminal plugin UI in v1. The 0.9.5 docs don't add a header hook either.

### Option A: a custom workspace token in Space rows (confirmed, closest to the sidebar)

- A plugin calls `workspace.report_metadata` (CLI: `herdr workspace report-metadata <ws> --source <id> --token name=value [--ttl-ms N] [--seq N]`) to set a named token on a workspace (socket doc, "Agent state reporting"; schema `WorkspaceReportMetadataParams`).
- The user adds `$name` to `[ui.sidebar.spaces].rows` in `config.toml`. The default is `rows = [["state_icon", "workspace"], ["branch", "git_status"]]`. The token can be styled with `{ token = "$pomodoro", fg = "#f38ba8", bold = true }` (config doc, "Sidebar row layouts").
- **Live check:** `herdr workspace report-metadata wC --source research:probe --token pomo_probe="25:00" --ttl-ms 4000` followed by `herdr workspace get wC` returned `"tokens":{"pomo_probe":"25:00"}`.
- **Limits:**
  - Tokens are **per workspace**. A global timer has to be reported to every workspace, and to new ones on `workspace.created`. Otherwise it shows on one row only.
  - Tokens render **inline, left to right, joined by ` · `**. Nothing right-aligns them. For example, `["state_icon", "workspace", "$pomodoro"]` renders as `● herdrmoro · 🍅 12:34`. Long values get truncated to the row width (`resolved_token_spans`, source).
  - The expanded desktop sidebar is the only place they render. Collapsed and mobile views keep their compact layouts (config doc).
  - Values are capped at 80 characters. A report names at most 16 tokens, and a workspace holds at most 32.
  - **Tokens are not restored after a server restart**, so the plugin has to report them again.
  - Each change emits `workspace.metadata_updated` to API subscribers, but not to plugin `[[events]]` hooks.

### Option B: the `ui.tab_bar_right` status area (confirmed, top-right of the tab row)

- This is a "tmux-style status area at the right edge of the tab row" (config doc). It's empty by default.
- A `{ type = "command", command = "...", interval_seconds = N, timeout_seconds = M }` entry runs a shell command on the server, first right away and then every `N` seconds (minimum 1). It shows **the last line of successful output**.
- The command gets `HERDR_BIN_PATH`, the socket path and active ids. Validation is in `src/config/tab_bar.rs` (source), and the key is present in the 0.8.2 `--default-config`.
- This needs a user `config.toml` entry. Polling once a second is fine for a mm:ss timer; a command could just read the plugin's state file.
- It's visible in every workspace, so it suits a global timer. But it sits in the tab row, not the sidebar.

### Option C: other text surfaces (confirmed, weaker fits)

- `client.window_title.set` sets the outer terminal window title (socket doc).
- `pane.report_metadata` sets a pane title, a display-agent name, state labels and `$tokens` for Agent sidebar rows. These are per pane and agent-oriented.
- `tab.rename` and `workspace.rename` overwrite user-visible labels. That's what auto-title does to tab names. It's invasive.

**Recommendation for the map:** the "top-right of the spaces section" placement isn't possible as specified. The nearest native options are Option A (a `$pomodoro` token on the Space rows) and Option B (the tab-bar right status area, which is global and auto-refreshing). Both require the user to paste a snippet into `config.toml`. The floating-panel fallback is weaker than either (see Q2).

## 2. Pane placements

Placements are `overlay` (the manifest default), `popup`, `split`, `tab` and `zoomed` (schema `PluginPanePlacement`; plugins doc "Panes").

- **`popup`:**
  - It's a session-modal terminal. It "receives all terminal input, including Escape, and closes when the command exits or a `popup.close` request is sent."
  - `width` and `height` take cells or percentages, with a default of half size and a minimum clamp.
  - **No position field exists**: `PluginPaneOpenParams` has only `width`, `height`, `placement`, `direction`, `target_pane_id` and so on. The popup can't be pinned to a corner.
  - It has no pane id and isn't part of the pane, layout or persistence APIs.
  - **It can't stay visible while you work elsewhere, because it takes all input.** So it doesn't work as a persistent timer display. It does work as an on-demand panel.
- **`split`, `tab`, `zoomed` and `overlay`** become normal herdr panes after they open.
  - A split can be opened unfocused (`--no-focus` / `focus: false`) and sized small with `pane.resize` or `layout.set_split_ratio`.
  - It stays visible while you work in sibling panes, **but only in its own tab**. Panes belong to one tab, and there's no sticky or global pane (not found in docs, schema or source).
  - `overlay` is a temporary zoomed pane that restores focus when it closes.
- **CLI quirk (0.8.2):** `herdr plugin pane open --placement` accepts only `overlay | split | tab | zoomed` and has no `--width`/`--height` flags. Popup placement and size come from the manifest (omni does this) or from a raw `plugin.pane.open` socket call.
- No floating, non-modal, positioned overlay window exists. Experimental pane graphics in 0.8.2 draw images over a pane only, and 0.9.5 removed that socket API.

## 3. Manifest format (`herdr-plugin.toml`)

Taken from `RawPluginManifest` in `src/app/api/plugins/manifest.rs` (source) and the plugins doc.

- **Top level:** `id` (required), `name` (required), `version` (required) and `min_herdr_version` (required in practice, because link or install is refused when it's missing or newer than the binary). Then `description` (optional) and `platforms` (optional `["linux","macos","windows"]`; linking without it gives a warning).
  - Plugin ids may use letters, digits, `.`, `:`, `_` and `-`.
- **`[[build]]`:** `command` (argv) and `platforms`.
  - Runs only on GitHub `plugin install`, never on `plugin link`. A failure aborts the install.
  - Gets no runtime env or socket.
  - auto-title uses `go build -o herdr-auto-title ./cmd/herdr-auto-title`.
- **`[[startup]]`:** `command` and `platforms`. Covered in Q7.
- **`[[actions]]`:** `id`, `title`, `description?`, `contexts` (`global | workspace | tab | pane | selection`), `platforms?` and `command`.
- **`[[events]]`:** `on` (an event name) and `command`.
- **`[[panes]]`:** `id`, `title`, `description?`, `platforms?`, `placement` (default `overlay`), `width?`, `height?` and `command`.
- **`[[link_handlers]]`:** `id`, `title`, `pattern` (a Rust regex), `action` and `platforms?`.
- **Rules for all entries:**
  - Local ids (action, pane, link handler) may not contain dots. They're qualified as `<plugin.id>.<action>`.
  - Commands are argv arrays with **no shell**. They run with the plugin root as the working directory.
- **There's no manifest field for keybindings, config UI or sidebar layout.** Those all live in the user's `config.toml`.
- **Examples:**
  - omni: `[[build]]` plus a popup `[[panes]]` (90% by 90%). It has no actions.
  - auto-title: `[[build]]` plus one `[[startup]]` and `min_herdr_version = "0.8.2"`.

## 4. Actions

- **Declaration:** `[[actions]]` in the manifest only. Runtime registration doesn't exist in v1.
- **Keybinding:** the user adds this to `config.toml` (config doc "Custom command keybindings"; `CommandKeybindType::PluginAction` in `src/config/keybinds.rs`):
  ```toml
  [[keys.command]]
  key = "prefix+p"
  type = "plugin_action"
  command = "<plugin.id>.<action-id>"
  description = "pomodoro: start/pause"
  ```
  A plugin can't ship its own bindings. omni's README tells users to add a `type = "shell"` binding that runs `$HERDR_BIN_PATH plugin pane open …`.
- **Other ways to invoke:**
  - `herdr plugin action invoke <id>`
  - the socket call `plugin.action.invoke`
  - link handlers
- **Execution:**
  - Every invocation **spawns the action's argv command as a fresh process**: `start_plugin_command` in `src/app/api/plugins/runtime.rs` (source).
  - The process gets `HERDR_PLUGIN_ACTION_ID`, `HERDR_PLUGIN_CONTEXT_JSON` and the standard env.
  - stdout and stderr are captured, capped at 64 KiB, and stored in the plugin log (`herdr plugin log list`).
  - At most 32 plugin commands can be in flight across all plugins. Herdr sets no timeout.
- **Reaching a long-running process:** **herdr provides no channel.** There's no plugin-to-plugin messaging, no custom events and no "send to daemon" call (not found in docs, schema or source). The action command has to reach the daemon itself. Possible ways:
  - a Unix socket or FIFO in `HERDR_PLUGIN_STATE_DIR`
  - a signal to a PID from a pidfile
  - writing the state file directly, which the daemon watches or polls
  - a stateless design: store the phase start timestamp and paused flag on disk, let actions edit that file, and let readers compute the remaining time.

## 5. Notifications

- CLI: `herdr notification show <title> [--body TEXT] [--position top-left|top-right|bottom-left|bottom-right] [--sound none|done|request]`. Socket: `notification.show` (socket doc; schema `NotificationShowParams`).
- Text is sanitized. The title is capped at 80 characters and the body at 240.
- The response says `shown`, `disabled`, `rate_limited`, `no_foreground_client` or `busy`.
- **Important:** delivery follows `ui.toast.delivery`, and **the default is `off`**: `ToastDelivery` `#[default] Off` in `src/config/model.rs`.
  - With the default, `notification.show` returns `disabled` and shows nothing.
  - The local `config.toml` has no `[ui.toast]` section, so it's off on this machine today.
  - The plugin's setup docs must tell users to set `delivery = "herdr"`, `"terminal"` or `"system"`.
  - `position` applies only with `herdr` delivery.
- The config doc says "Herdr suppresses popups for the active tab". That's stated for agent notifications. Whether it also applies to explicit `notification.show` is **not verified**.

## 6. State and config directories

- `HERDR_PLUGIN_CONFIG_DIR` is `<config_dir>/plugins/config/<id>`. On this machine `herdr plugin config-dir herdr-omni` printed `~/.config/herdr/plugins/config/herdr-omni`.
- `HERDR_PLUGIN_STATE_DIR` is `<state_dir>/plugins/<id>`, which is `~/.local/state/herdr/plugins/<id>` on macOS or `$XDG_STATE_HOME/herdr/plugins/<id>` (`src/plugin_paths.rs`, `src/config/io.rs`).
- Both are created on install or link. There's no CLI to print the state dir; only the env var exposes it.
- **Sanctioned place for state:** "put local runtime state under `HERDR_PLUGIN_STATE_DIR`". User-editable config goes in `HERDR_PLUGIN_CONFIG_DIR`. Never use `HERDR_PLUGIN_ROOT`, because managed checkouts get replaced (plugins doc).
- "There is no Herdr-managed plugin storage API in v1": herdr only discovers the paths, and the plugin owns the format, migrations and cleanup. omni keeps its updater state in `HERDR_PLUGIN_STATE_DIR`.

## 7. Socket events and the background-process lifecycle

- **Startup hooks:**
  - They run once per enabled plugin after the session is restored and the socket is ready.
  - They **run again when a new server takes over during live handoff**, but not on client attach, config reload, link or enable (plugins doc).
  - The docs call them "one-shot initialization commands rather than supervised daemons".
  - In practice a startup command may stay alive. auto-title's binary runs forever: "A one-shot launch, not a supervised daemon: the process stays alive itself" (its manifest).
  - Herdr only `wait()`s on the child and **never kills it**. There's no `kill` in the 0.8.2 plugin runtime source, so disabling the plugin doesn't stop it either (not found).
  - A long-lived startup command keeps one of the 32 in-flight command slots for its whole life.
- **Socket and subscriptions:**
  - The socket speaks NDJSON over a Unix socket (`HERDR_SOCKET_PATH`).
  - A plain request gets one response, then the connection closes. auto-title dials once per call and polls `session.snapshot` on a timer instead of subscribing.
  - `events.subscribe` keeps a connection open for pushed events.
- **Subscribable events (schema `Subscription`):**
  - workspace: `created`, `updated`, `metadata_updated`, `renamed`, `moved`, `reordered`, `closed`, `focused`
  - tab: `created`, `closed`, `focused`, `renamed`, `moved`
  - pane: `created`, `updated`, `closed`, `focused`, `moved`, `exited`, `agent_detected`, `output_matched`, `agent_status_changed`, `scroll_changed`
  - `layout.updated`
  - worktree: `created`, `opened`, `removed`
- **Manifest `[[events]]` hooks** accept a narrower list that leaves out `workspace.metadata_updated`, `pane.updated`, `pane.output_changed` and `layout.updated` (`PLUGIN_HOOK_EVENT_KINDS`, source).
- **No server-shutdown, config-reload or handoff event exists** (none in the schema). A background process has to notice shutdown on its own:
  - EOF on an open `events.subscribe` connection
  - failed dials or `ping`
  - SIGTERM/SIGINT, which auto-title handles with `signal.NotifyContext`
- 0.9.5 adds: "Stop plugin work before shutting down its server: processes surviving server exit are not protected from cleanup."
- **Consequences for a daemon design:**
  1. Enforce a single instance, for example with a lock file in the state dir, because live handoff starts the startup hook a second time.
  2. Exit when the server goes away.
  3. Report the sidebar tokens again after startup, because they aren't persisted.
  4. Persist its own timer state, which the map already settled.

---

## Confirmed vs. not found

| Claim | Status |
| --- | --- |
| No API or config to draw into the "spaces" header | Confirmed (source: hard-coded literal) |
| Workspace `$token` in Space rows | Confirmed (docs, schema, live probe) |
| `ui.tab_bar_right` command entry | Confirmed (docs, default config, source) |
| Popup has no position field, is modal, takes all input | Confirmed (docs, schema) |
| Global or sticky pane across tabs | Not found |
| Keybindings declared in the manifest | Not found (user `config.toml` only) |
| herdr channel from action to daemon | Not found |
| Server shutdown or config-reload event | Not found |
| herdr killing startup processes on exit or disable (0.8.2) | Not found |
| `notification.show` suppressed for the active tab | Not verified |
