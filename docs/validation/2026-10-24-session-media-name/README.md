# Running-core media filename

Scope: FogCast session presentation, hostclient decoding, tenfoot selector,
FogCast's Go runtime adapter, and libmister-runtime's eject error reporting. Base
`4bd64a993f125d9fa56730631ed5097077102df4`; final executable source
`ac755a54e88fa3676d8a54c97b17467b6851c672`. Host USB/SMB media browsing
is covered separately by PR #1041. After a post-dispatch eject exchange becomes
ambiguous, libmister-runtime preserves its input-phase `io_failed` result after
best-effort realignment. FogCast maps that outcome as uncertain; pre-dispatch
failures and acknowledged GP rejections remain no-change results. This changes
runtime error behavior without changing the protocol schema, FPGA behavior,
browsing API, or catalog schema.

The header shows the last confirmed live-insert filename for the captured target
and package generation. Empty/loading units take precedence. A confirmed eject
clears the filename and records that legacy `fes.simple-computer` media is empty;
an uncertain mutation reply clears the filename and any prior confirmed-eject
marker, leaving the media state unknown until the next session observation.
Completed no-change rejections retain the confirmed filename. Launch selections
and adopted sessions whose original filenames were never recorded show that the
name is unavailable.

## Follow-up review fix

After merging `origin/main`, commit `82816f10` clears the stale legacy
`media_ejected` marker as well as the filename when an insert or eject outcome
is uncertain. Its host-only checks pass:

- `go test ./ui/tenfoot ./internal/misterruntime`
- `make check`
- `make test-changed TEST_CHANGED_ARGS='--base origin/main'`

The affected run passed its parent, host Go, browser UI, and runtime software
lanes. No kit or hardware was used for this follow-up.

## Initial software checks

- At the original validation head, focused hostclient, host API and complete
  tenfoot tests pass. Tests cover immediate UI clearing after a lost reply and
  confirmed reinsertion/eject.
- `make test-changed TEST_CHANGED_ARGS='--base 4bd64a99'`: planner checks,
  844 selected parent regressions (one platform-specific skip), generated
  consistency, host Go tests, and the ordinary package race suite pass.
  The concurrency-focused race lane initially fails an unchanged mesh cache
  test (`second GET hammered the kit`, configured reads 2). Its cause is not
  established here. That exact lane passes on a separate retry (69.924 seconds).
  The successful preceding checks are retained rather than repeated.
- Host UI: 358 unit/harness checks pass. The first Chromium process reports `no held fixture response` during a
  settings test, which also prevents its following four scenarios
  from resetting their fixture. Rerunning that integration process alone
  passes all 77 checks. No browser source is changed.
- `make check` passes: 21 generated consumers and 37 fixture copies.
  `make deadcode` and committed whitespace checks pass.
- The managed ARM bundle builds against the retained baseline image. Its
  complete artifact/toolchain receipt is [software-receipt.json](software-receipt.json).

## Kit B diagnostic

This retained exact-bundle diagnostic used FogCast/runtime source
`ac755a54`, before the later eject transport classification and local marker
fixes. It remains evidence for that installed artifact and does not qualify
those subsequent changes.

The claimed kit installs and activates bundle
`b2113aeeca0f65d0e6d585b268c9580fc875fa7c59998714f6d4cff02e598b77`
at software generation 53 on image
`4db7486452057d8a77479125c8ee8e9a27db4f378a9d9735900e90f6f3fe7a26`.
The exact installed ARM launcher SHA-256 is
`a5a430e498dd88fbaf946b3b98bfdaf95ccdc4fd53b8a2cde8828489887a04de`.
The existing sealed C64 package is
`713b811056616b153683a5f1c7e924c290bd6cdc0008c3cf69d7c27448f5af93`.
It is linked with the repository's open diagnostic firmware (vacant sockets).
The disk is the repository's synthetic 35-track diagnostic D64, named `swap.d64`.
No stock ROM or third-party game bytes are needed for this check.

A private container host starts with a verified empty catalog. An isolated
instance of the exact installed launcher uses its paired listener and synthetic
evdev keyboard. Through the actual launcher selector:

1. Insert `swap.d64`; the unit reports ready and the host reports that name.
2. Close and reopen the selector; HDMI shows **Inserted: swap.d64**.
3. Eject and reopen; HDMI shows **No media inserted** and the unit reports empty.

The session, game, package and core generation 1 remain identical throughout.
[Inserted HDMI capture](inserted.png) and [empty HDMI capture](empty.png) are
actual ASUS capture-device frames, not mockups. Machine raster is visible around
the panel. The open machine diagnostic is not completed by this UI test; this is
not C64 fidelity, a successful program load, physical USB-input qualification or
a complete appliance-flow acceptance claim.

An earlier setup attempt finds that `fogcast-api` ignores explicit per-config
state/staging paths and opens the default home catalog. It is stopped before
core launch; the completed diagnostic uses the isolated container instead.
That separate entrypoint defect is tracked in #1046.

The task's raw logs and private observations remain in
`out/dev/media-name-hil/` and `/tmp/media-name-*.log` in this worktree's host.
[Machine-readable observations](evidence.json) retain independent artifact and
session identities. Kit B is restored and released: baseline bundle `35d58489` is selected and active
at software generation 54, boot image and boot ID are unchanged, idle RBF and
launcher configuration hashes match the initial readings, the USB-storage quirk
is preserved, and runtime idle/free lease are verified. The owned launcher,
input helpers, SSH tunnel and host container are removed. Kit A is untouched.
