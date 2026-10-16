# PROTOTYPE verdict: Where and how does the timer render? (ticket #4)

Winner: surface B (`ui.tab_bar_right` command entry) with variant A (minimal icon) text,
plus paused showing the phase icon. Sidebar tokens (one copy per workspace row) and the
split pane were rejected; labelled, progress-bar and coarse-minute texts lost to minimal.
Run: add a `tab_bar_right` command entry pointing at `proto.sh render`, then
`proto.sh v A..D` and `proto.sh s idle|focus|focus-paused|short|long|done`.
