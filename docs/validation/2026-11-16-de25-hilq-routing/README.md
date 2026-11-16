# DE25 HILQ routing groundwork

This change adds tested shared policy for mapping deployed DE25 PR paths and
`core:de25-*` labels to Kit D25, selecting no A/B fallback, and resolving the
two Quartus Pro package producers. The recovery predicate requires both the
observed J8G board identity and a serial-bearing `/dev/disk/by-id/` device
identity before it can approve a card write.

Validation: `python3 -m unittest tests.test_de25_hilq -v` (6 tests) and
`make check` passed on `a06654d36` plus this change.

This is routing and recovery-guard groundwork only. It does not claim that the
Powerboat HILQ queue, lander-watch, or autopilot dispatches D25; the exact-head
D25 runner, automatic promotion, and live recovery write path remain separate
work. Kit A/B and `current-restore.json` were not touched. The DE25 Quartus
sealer entrypoint prepared on Powerboat refuses a Lite/local fallback, but is
not invoked by the current queue.
