# USB board policy scans and native configuration timing observations

Source `db6729fe2560fa3cc33dafe06967268ccc32a712`, base `83ad32680`.

Board preparation now checks every captured clock and compiled pin once per invocation. The removed second scan immediately repeated the first without an intervening operation. Initial clock capture/recheck remains paired; controller operations still make fresh checks at their boundaries, and physical readiness still checks both PHY observations with fresh preparation before and after. No hardware state is cached across calls. Tests change clock and pin values during controller writes and PHY reads and require terminal rejection before readiness publication.

The actual shared board/boot/master register fixture drops from 10,711 to 5,699 accesses per coherent bridge observation, retaining all 112 PHY transactions. Fault positions are sampled across the actual compiled read span. These are software work counts, not physical timing qualification.

Native configuration errors now distinguish elapsed-time expiry from a backwards software clock and report elapsed/step durations. Tests assert the exact 20-second boundary, backwards-clock reason and original cancellation error while proving that no computer control opens and failed configuration resources remain retained. The deadline is unchanged.

The new bounded operator also fixes receive-only failure-tail collection: the active/default failure gate remains strict, but the already-failed handler can collect the fragmented panic tail before rethrowing. Byte/time bounds remain active, and the actual pump fixture proves no command writes. The earlier failed image's trace remains immutable.

All ten selected software commands passed, including native firmware/RTL integration and shared core tooling. An initial orchestration index was stale after the planner selected a shorter list. The orchestration parent was paused, its already-completed phase6/7 children were verified terminal, and only that parent was stopped. Their passing receipts were retained; remaining phases ran separately with bounds selected by command role. No test was skipped or test-case timeout widened.

The application, entry loader, secure firmware and U-Boot were rebuilt from the frozen source. Actual sealed-package catalog and compiled source/call-path checks passed. The matched JTAG composition preserves the complete FPGA core payload. This repaired image has not run on hardware; the prior two shared-input failures are retained separately and are not retried.

The single-attempt operator requires qualified recovery, seven fixed uploads and fresh SD readbacks, then the complete ZX81/ZX80/ZX81 serial App launch/return/quit sequence with continued USB delivery and the exact retained lease. Ordinary FES package/ROM/media admission, physical controls/video, native Ethernet and DE10-normal-kit readiness remain open.

The sealed FPGA projects remain prerequisites at `/home/deano/fes-retained/de25-tenfoot-native-boot-20261104-r3/matched-candidate.tar.gz`, SHA256 `1479d5fa439c77457a3c19959a97b810b8ce5a6aa0672bb4567519f2d7229d17`. No new FPGA synthesis, timing signoff or electrical acceptance is claimed.
