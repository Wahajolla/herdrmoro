#!/usr/bin/env bash
# PROTOTYPE, throwaway. Answers "Where and how does the timer render?"
# (herdrmoro ticket #4). Not the plugin. Fakes the timer; nothing real runs.
#
#   proto.sh render            one line for ui.tab_bar_right (surface B)
#   proto.sh tokens            push $pomodoro to every workspace once (surface A)
#   proto.sh tokens-loop       ...every second, until Ctrl-C
#   proto.sh panel             redraw loop for a small split pane (surface C)
#   proto.sh v next|prev|A..D  switch text variant
#   proto.sh s <scenario>      switch scenario (restarts its fake clock)
#   proto.sh state             show the current variant and scenario
#
# Scenarios: idle focus focus-paused short long done
set -u
DIR="$(cd "$(dirname "$0")" && pwd)"
ST="$DIR/.proto-state"   # PROTOTYPE, wipe me
VARIANTS=(A B C D)
NAMES=([0]="Minimal icon" [1]="Labelled" [2]="Progress bar" [3]="Coarse minutes")

[ -f "$ST" ] || printf 'variant=A\nscenario=focus\nstarted=%s\n' "$(date +%s)" > "$ST"
# shellcheck disable=SC1090
. "$ST"

save() { printf 'variant=%s\nscenario=%s\nstarted=%s\n' "$variant" "$scenario" "$started" > "$ST"; }

# --- fake timer -------------------------------------------------------------
# Sets: phase (focus|short|long|none) running(1/0) remaining total session done
fake() {
  local now el; now=$(date +%s); el=$(( now - started ))
  session=2; done=0; running=1
  case "$scenario" in
    idle)         phase=none;  total=1500; remaining=1500; running=0 ;;
    focus)        phase=focus; total=1500; remaining=$(( 1500 - 380 - el )) ;;
    focus-paused) phase=focus; total=1500; remaining=$(( 1500 - 380 )); running=0 ;;
    short)        phase=short; total=300;  remaining=$(( 300 - 50 - el )) ;;
    long)         phase=long;  total=900;  remaining=$(( 900 - 60 - el )); session=4 ;;
    done)         phase=short; total=300;  remaining=300; running=0; done=1 ;;
  esac
  (( remaining < 0 )) && remaining=0
}

mmss() { printf '%02d:%02d' $(( $1 / 60 )) $(( $1 % 60 )); }
mins() { printf '%dm' $(( ($1 + 59) / 60 )); }
icon() { case "$1" in focus) printf '🍅';; short) printf '☕';; long) printf '🌴';; *) printf '🍅';; esac; }
label() { case "$1" in focus) printf 'Focus';; short) printf 'Break';; long) printf 'Long break';; esac; }
dots() { local i out=""; for i in 1 2 3 4; do (( i <= $1 )) && out+="●" || out+="○"; done; printf '%s' "$out"; }
bar() {
  local w=8 f i out=""; f=$(( ( ($2 - $1) * w ) / $2 ))
  for (( i = 0; i < w; i++ )); do (( i < f )) && out+="▰" || out+="▱"; done; printf '%s' "$out"
}

# One line of text, per variant. Empty output = herdr hides the entry.
line() {
  fake
  local p=""; (( running )) || p="⏸ "
  case "$variant" in
    A) # Minimal: icon + mm:ss; paused swaps the icon for ⏸; idle shows nothing.
      [ "$phase" = none ] && return
      (( done )) && { printf '%s ready ▶' "$(icon "$phase")"; return; }
      (( running )) && printf '%s %s' "$(icon "$phase")" "$(mmss "$remaining")" \
                    || printf '⏸ %s %s' "$(icon "$phase")" "$(mmss "$remaining")" ;;
    B) # Labelled: words, session n/4, explicit paused.
      [ "$phase" = none ] && { printf '🍅 Pomodoro: start'; return; }
      (( done )) && { printf '%s next · start' "$(label "$phase")"; return; }
      printf '%s%s %s · %s/4' "$p" "$(label "$phase")" "$(mmss "$remaining")" "$session" ;;
    C) # Progress: icon, bar, time, session dots.
      [ "$phase" = none ] && { printf '🍅 %s' "$(dots 0)"; return; }
      (( done )) && { printf '%s %s ▶ %s' "$(icon "$phase")" "$(bar 0 "$total")" "$(dots "$session")"; return; }
      printf '%s%s %s %s %s' "$p" "$(icon "$phase")" "$(bar "$remaining" "$total")" "$(mmss "$remaining")" "$(dots "$session")" ;;
    D) # Coarse minutes: less to watch tick; only the last minute shows seconds.
      [ "$phase" = none ] && return
      (( done )) && { printf '%s ▶' "$(icon "$phase")"; return; }
      local t; (( remaining < 60 )) && t="${remaining}s" || t=$(mins "$remaining")
      printf '%s%s %s' "$p" "$(icon "$phase")" "$t" ;;
  esac
}

push_tokens() {
  local text ws; text=$(line)
  for ws in $(herdr workspace list | grep -o '"workspace_id":"[^"]*"' | cut -d'"' -f4); do
    if [ -n "$text" ]; then
      herdr workspace report-metadata "$ws" --source herdrmoro-proto --token "pomodoro=$text" >/dev/null
    else
      herdr workspace report-metadata "$ws" --source herdrmoro-proto --clear-token pomodoro >/dev/null
    fi
  done
}

big_digits() { # 3-row block digits for the panel
  local s="$1" r c out
  local -a D0=("█▀█" "█ █" "▀▀▀") D1=(" ▀█" "  █" "  ▀") D2=("▀▀█" "█▀▀" "▀▀▀") D3=("▀▀█" " ▀█" "▀▀▀") \
    D4=("█ █" "▀▀█" "  ▀") D5=("█▀▀" "▀▀█" "▀▀▀") D6=("█▀▀" "█▀█" "▀▀▀") D7=("▀▀█" "  █" "  ▀") \
    D8=("█▀█" "█▀█" "▀▀▀") D9=("█▀█" "▀▀█" "▀▀▀") DC=(" " "▪" " ")
  for r in 0 1 2; do
    out="  "
    for (( c = 0; c < ${#s}; c++ )); do
      local ch="${s:c:1}" ref
      [ "$ch" = ":" ] && ref="DC[$r]" || ref="D${ch}[$r]"
      out+="${!ref} "
    done
    printf '%s\n' "$out"
  done
}

panel() {
  trap 'printf "\e[?25h"; exit' INT TERM
  printf '\e[?25l'
  while :; do
    . "$ST"; fake
    printf '\e[H\e[2J'
    if [ "$phase" = none ]; then
      printf '\n  🍅 Pomodoro\n\n  idle: run "Start" from the\n  action menu\n'
    else
      local col; case "$phase" in focus) col=31;; short) col=32;; long) col=36;; esac
      printf '\n  \e[1;%sm%s %s\e[0m%s\n\n' "$col" "$(icon "$phase")" "$(label "$phase")" "$( (( running )) || printf '  \e[2m(paused)\e[0m')"
      printf '\e[%sm' "$col"; big_digits "$(mmss "$remaining")"; printf '\e[0m\n'
      printf '  %s  %s\n' "$(bar "$remaining" "$total")" "$(dots "$session")"
      (( done )) && printf '\n  \e[1mPhase over. Start the next one.\e[0m\n'
    fi
    sleep 1
  done
}

case "${1:-render}" in
  render) line; echo ;;
  tokens) push_tokens ;;
  tokens-loop) while :; do . "$ST"; push_tokens; sleep 1; done ;;
  panel) panel ;;
  v)
    i=0; for k in "${!VARIANTS[@]}"; do [ "${VARIANTS[$k]}" = "$variant" ] && i=$k; done
    case "${2:-next}" in
      next) i=$(( (i + 1) % 4 )) ;; prev) i=$(( (i + 3) % 4 )) ;;
      *) for k in "${!VARIANTS[@]}"; do [ "${VARIANTS[$k]}" = "$2" ] && i=$k; done ;;
    esac
    variant=${VARIANTS[$i]}; save; echo "variant $variant (${NAMES[$i]})" ;;
  s) scenario="${2:?scenario}"; started=$(date +%s); save; echo "scenario $scenario" ;;
  state) cat "$ST"; printf 'now: '; line; echo ;;
  *) sed -n '2,13p' "$0"; exit 1 ;;
esac
