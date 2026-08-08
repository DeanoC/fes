# FogCast POC6 development guide

## Purpose and accepted baseline

POC6 is complete and shipped for the real-game host-to-MiSTer HDMI and
managed-lifecycle scope recorded in `POC6-RESULTS.md`. This document explains
how to operate and extend that accepted testbed without weakening its ownership,
security, or evidence boundaries.

The retained baseline is:

```text
host-only catalog game
-> RetroArch + libretro core
-> macOS screen capture
-> VideoToolbox H.264
-> authenticated RTP/control
-> target-owned ARMv7 decode bridge
-> /dev/fb0
-> disposable native presentation hook
-> MiSTer HDMI
```

The disposable presentation hook remains intentionally installed at the
canonical target path `/media/fat/MiSTer`. It is required for this testbed;
stock `Main_MiSTer` did not present arbitrary `/dev/fb0` writes on HDMI. Run
exactly one presentation owner from that path.

The accepted artifact hashes and physical evidence are in
`POC6-RESULTS.md`. Rebuilding after a source change necessarily creates new
hashes and requires new validation; do not silently relabel a new binary with
an old acceptance hash.

## Known deferrals

- Host-emulator controller capture/injection is
  [issue #3](https://github.com/DeanoC/FogCast-POC/issues/3). The existing
  remote input path sends host-originated events toward the MiSTer input bridge;
  it is not a RetroArch injector. Keep target remote input disabled for
  host-only sessions.
- Physical glass-to-glass latency is
  [issue #1](https://github.com/DeanoC/FogCast-POC/issues/1) and
  [issue #2](https://github.com/DeanoC/FogCast-POC/issues/2). Encode timing,
  RTP timing, decoder counters, and framebuffer writes are not substitutes for
  the required physical measurement.

These deferrals do not reopen the accepted POC6 video/lifecycle result, but do
prevent a broader "one controller" or measured-latency claim.

## Safety and ownership invariants

Preserve these rules during follow-on work:

1. The host session owns RetroArch, screen capture, the H.264 sender, and the
   target cast lease.
2. `mister-agent` owns bridge process creation, status, and termination.
3. Cast identity is the pair `(session, generation)`. Status must report both,
   and stop must be conditioned on both. A stale owner must not stop a
   replacement generation.
4. Do not invoke target cast start/stop manually during a host-owned session.
   Use the host session API so composition ownership remains coherent.
5. Exactly one native presentation owner runs from `/media/fat/MiSTer`.
6. Do not use a bare TCP readiness probe against the cast bridge. It consumes
   the bridge's single authenticated control connection. Use HTTP health and
   status instead.
7. Give each teardown component its own finite deadline. Preserve unresolved
   ownership and retry real failed cleanup operations; never turn a later
   no-op into false success.
8. Never log raw downstream errors where paths, addresses, credentials, or
   injected diagnostics could escape.
9. Keep target addresses, NAS paths, bearer tokens, runtime-installed media
   token material, SSH credentials, and deployment automation outside Git.
   Private files should be owner-only (`0600`). Do not place secrets in process
   arguments.
10. Treat synthetic gradients as presentation diagnostics only. A physical
    acceptance claim requires recognizable fresh game content on HDMI.

## Prerequisites

- macOS arm64 host with the pinned Go toolchain (`go@1.26.5`).
- arm64 RetroArch and the intended arm64 libretro core.
- macOS Screen Recording permission granted to the terminal or service that
  launches `fogcast-api`. Keep RetroArch on the captured main display and
  remove pause overlays before collecting game evidence.
- Authorized NAS shares mounted locally and bounded-listing verified before a
  scan or launch.
- Managed target tunnel running:

  ```sh
  export MISTER_TARGET_HOST=PRIVATE_TARGET_HOST
  # Optional for unattended use; this existing private file must be mode 0600.
  export MISTER_SSH_PASSWORD_FILE=/absolute/private/password-file
  scripts/dev-target-tunnel.sh
  ```

  Keep the resolved address in the local environment; the tracked helper has no
  target-address default. If no password file is configured, the helper prompts
  once, stores the password in a mode-`0600` temporary file for its process
  lifetime, and removes that file on exit. It never places the password in argv.

- Retained target image, exact deployed agent/bridge, and disposable
  `/media/fat/MiSTer` presentation hook.
- For physical evidence, ShadowCast 3 configured for the accepted 1920x1080,
  30 FPS, UYVY mode.

The tunnel's HTTP endpoint is authoritative target readiness:

```sh
curl --fail --silent --show-error --max-time 5 \
  http://127.0.0.1:18182/v1/health
```

A ready testbed reports `ready=true`, `mister_process=true`, and
`command_pipe=true`. A delayed supervisor or tunnel notification is not live
state; query health again.

## Build and local validation

Unset inherited Python variables for Go commands on the managed host:

```sh
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- make build
env -u PYTHONHOME -u PYTHONPATH CGO_ENABLED=1 \
  mise exec go@1.26.5 -- go build -buildvcs=false -trimpath \
  -o bin/fogcast-api ./cmd/fogcast-api
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- make test
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- make check
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- go vet ./...
git diff --check
```

Run the explicit CGO-enabled `fogcast-api` build **after** `make build`.
The generic Makefile target currently emits that one binary with
`CGO_ENABLED=0`; it is suitable for non-hardware checks but selects the capture
stub and cannot run the POC6 AVFoundation/VideoToolbox path. A hardware-capable
binary links the macOS AVFoundation, CoreMedia, CoreVideo, VideoToolbox,
Foundation, and CoreGraphics frameworks.

Relevant outputs are:

```text
bin/fogcast-api                 macOS arm64 host API
bin/mister-agent-linux-armv7    target agent
```

The accepted target decode bridge was produced by the POC6-specific ARMv7
build/deployment workflow. It is not part of the production Buildroot image
configuration. Do not modify production Buildroot settings merely to iterate on
this disposable testbed.

That FFmpeg-capable ARMv7 bridge build and the disposable native-hook build are
not yet reproducible from a fresh checkout of this repository. Their accepted
binaries are retained on the authorized testbed and identified by hash in
`POC6-RESULTS.md`; the native-hook source/build checkout also remains external
to this repository. The current testbed is therefore usable for host,
session-lifecycle, API, and controller development without rebuilding either
artifact. Any work that changes the bridge or hook must first add or separately
approve a reproducible cross-build and deployment workflow rather than relying
on deleted `/tmp` scripts or undocumented local state.

## Private host configuration

Keep the working host configuration outside the repository, normally under
`~/.config/fogcast/`, with mode `0600`. The following is a schema example, not a
copy-paste deployment file: replace every placeholder locally and keep the
result untracked.

```toml
base_url = "http://127.0.0.1:18182"
token = "PRIVATE_TARGET_API_TOKEN"
request_timeout_seconds = 12
upload_timeout_seconds = 60

[[libraries]]
id = "snes-main"
system = "snes"
root = "/absolute/local/mount/SNES"

[host_emulator]
binary = "/absolute/path/to/RetroArch"
core = "/absolute/path/to/snes9x_libretro.dylib"
systems = ["snes"]

[remote_input]
enabled = false

[media]
enabled = true
session = "UNIQUE_NONEMPTY_SESSION"
generation = 1
ssrc = 1
rtp_listen = "127.0.0.1:LOCAL_RTP_PORT"
rtp_destination = "MISTER_TARGET_HOST:TARGET_RTP_PORT"
control_address = "MISTER_TARGET_HOST:TARGET_CONTROL_PORT"
decoder = "none"
capture_device = "screen"
width = 1920
height = 1080
fps_numerator = 30
fps_denominator = 1
bitrate = 4000000
gop = 60
mtu = 1200
```

Notes:

- TOML does not interpolate environment variables. Replace placeholders in the
  private file; do not commit the resolved values.
- `capture_device = "screen"` selects the Darwin main-display capture path.
- `generation` must be nonzero. Change it when deliberately creating a new
  cast identity during lifecycle development.
- The destination/control ports must match the private target cast
  configuration.
- Host-only composition intentionally does not attach the target input bridge.

## Target cast configuration

Preserve the target's existing validated base configuration and private token.
POC6 adds these `mister-agent` fields:

```toml
cast_binary = "/absolute/target/path/to/remote-play-fbbridge"
cast_rtp_address = "TARGET_BIND_HOST:TARGET_RTP_PORT"
cast_control_address = "TARGET_BIND_HOST:TARGET_CONTROL_PORT"
cast_framebuffer = "/dev/fb0"
cast_native_cmd = "/dev/MiSTer_cmd"
cast_native_mode = "8888 1 1920 1080"
cast_token_file = "/absolute/owner-only/runtime/token-file"
cast_generation = 1
```

All cast paths must be absolute. When `cast_binary` is set, RTP, control,
framebuffer, native-command, and native-mode fields are mandatory. The agent
installs the protected cast token into an owner-only runtime file and scrubs it
on teardown. The current host passes its configured bearer token for this
purpose; it does not generate a distinct per-session token. Do not reuse an
unsafe shared token path or deploy through a symlink.
`cast_generation` is retained in the target configuration schema, but the
current controller takes the authoritative session/generation identity from
each authenticated start request. Do not use the static field as an ownership
or stale-stop check.

Target deployment remains an authorized hardware operation: stop the old agent
with a finite deadline, atomically replace and hash-verify the new ARMv7 binary,
restart supervision, and re-query `/v1/health`. Never use a temporary HTTP file
server unless it is explicitly authorized and verified stopped afterward.

## Normal development session

### 1. Verify mounts and refresh the catalog

Before scanning, verify each authorized NAS mount with a bounded listing so a
stalled network filesystem cannot hang the workflow. Run a scan on a fresh
checkout/catalog, after changing library roots, or whenever library contents
have changed:

```sh
for mount in "$HOME/FogCastMounts/SNES" "$HOME/FogCastMounts/Genesis"; do
  ruby -e '
    child = spawn("/bin/ls", "-1", ARGV.fetch(0), out: File::NULL)
    deadline = Process.clock_gettime(Process::CLOCK_MONOTONIC) + 5
    loop do
      done = Process.waitpid(child, Process::WNOHANG)
      exit($?.exitstatus || 1) if done
      break if Process.clock_gettime(Process::CLOCK_MONOTONIC) >= deadline
      sleep 0.05
    end
    Process.kill("TERM", child) rescue nil
    sleep 0.2
    Process.kill("KILL", child) rescue nil
    exit 124
  ' "$mount" || {
    echo "mount probe failed or timed out: $mount" >&2
    exit 1
  }
done
```

The probe runs each filesystem access in a separately supervised child. If it
times out, do not scan or repeatedly force-unmount; inspect the mount/helper
state and follow the macOS SMB recovery procedure first.

```sh
bin/fogcast \
  --config /absolute/path/to/private-fogcast.toml \
  scan
```

`fogcast-api` opens the existing SQLite catalog; it does not scan libraries and
there is no host scan route. A missing or stale scan therefore produces a
missing or stale games query even when configuration and mounts are correct.

### 2. Start the host API

```sh
bin/fogcast-api \
  --config /absolute/path/to/private-fogcast.toml \
  --listen 127.0.0.1:8787
```

Keep it in a tracked terminal or service supervisor so shutdown output and exit
status are observable.

### 3. Verify host and target readiness

```sh
curl --fail --silent --show-error --max-time 5 \
  http://127.0.0.1:8787/api/v1/health

curl --fail --silent --show-error --max-time 5 \
  'http://127.0.0.1:8787/api/v1/games?q=ActRaiser'
```

The local `/api/v1/health` route returns HTTP 200 even when the target is
unavailable. Inspect its nested JSON and require both `target.reachable=true`
and `target.ready=true`; `curl --fail` alone is not a readiness assertion. The
direct target `/v1/health` check in Prerequisites remains authoritative.

Use the returned public `id`; do not derive a game ID from a ROM path.

### 4. Launch through the session boundary

```sh
curl --fail --silent --show-error --max-time 30 \
  -H 'Content-Type: application/json' \
  --data '{"game_id":"PUBLIC_GAME_ID"}' \
  http://127.0.0.1:8787/api/v1/session/launch
```

A successful host-only cast reaches:

```text
state=active
execution=host_only
media=active
```

Do not call `/v1/cast/start` separately. The host coordinator creates the
session/generation-conditioned target lease and retains partial cleanup
ownership when startup is ambiguous.

### 5. Observe without changing ownership

```sh
curl --fail --silent --show-error --max-time 5 \
  http://127.0.0.1:8787/api/v1/session

curl --fail --silent --show-error --max-time 5 \
  'http://127.0.0.1:8787/api/v1/session/events?after=0'
```

For target-side diagnosis, create a private curl configuration outside Git with
mode `0600`:

```text
# ~/.config/fogcast/target-curl.conf
header = "Authorization: Bearer PRIVATE_TARGET_API_TOKEN"
fail
silent
show-error
max-time = 5
```

Then query status without putting the token in argv:

```sh
curl --config "$HOME/.config/fogcast/target-curl.conf" \
  http://127.0.0.1:18182/v1/cast/status
```

Status must match the host-owned session and generation. Persistent
target-status failure is a terminal session condition; transient failures are
tolerated only within the bounded monitor policy.

Metrics prove activity, not HDMI presentation. For video-plane changes, also
capture fresh recognizable game content from ShadowCast and compare it with a
fresh host frame.

### 6. Stop through the session boundary

The stop request body must be empty:

```sh
curl --fail --silent --show-error --max-time 15 \
  -X POST http://127.0.0.1:8787/api/v1/session/stop
```

Confirm the later session state is `idle` with `media=stopped`. Then verify no
RetroArch process, host media socket, target bridge process, or target media
socket remains. Repeated or concurrent stop may retry incomplete teardown but
must continue reporting the first unresolved failure.

### 7. Shut down the host API

Send SIGTERM or interrupt the tracked API process and wait for it to exit. Its
shutdown cleanup result is part of the process result. Verify the loopback API
listener and RetroArch are absent; do not infer cleanup from an old process
notification.

## Recovery and rollback

Unexpected sender or bridge termination, and graceful target-agent shutdown,
should propagate through managed teardown. An ungraceful target-agent crash or
SIGKILL is different: the agent cannot run its deferred cast stop, the bridge
has no parent-death linkage, and the host cannot complete target stop through a
dead API. Treat that state as potentially orphaned. Through the authorized
target workflow, reconcile and terminate any surviving bridge with a finite
deadline, verify media sockets are absent, restart the agent, and require live
target health before launching another host session.

If target status reports another active `(session, generation)`, do not issue an
unconditioned stop. Reconcile the owner first; stale stop correctly returns a
conflict and must not kill a replacement.

If follow-on work no longer needs the POC6 video plane, restore stock only
through the established target provenance/rollback workflow. Stop the host
session and target agent first, verify no cast bridge remains, replace the
canonical presentation binary from the known stock backup, restore the intended
target image if required, restart supervision, and revalidate health and normal
FPGA presentation. Do not guess a stock binary or overwrite the retained hook
without a verified rollback source.

## Code map for further development

- `cmd/fogcast-api/main.go`: host composition root; per-session capture,
  sender, target lease, monitoring, and cleanup ownership.
- `internal/hostapi/session.go`: public session state machine, replacement
  serialization, events, and autonomous teardown.
- `internal/mediasession/session.go`: multi-component startup/rollback and
  independent bounded cleanup.
- `internal/remotemedia/managed_sender.go`: sender process/runtime ownership.
- `internal/remotemedia/sender.go`: H.264/RTP send path and safe idle-frame
  repetition.
- `internal/cast/controller.go`: target bridge process, token lifecycle, and
  session/generation-conditioned stop.
- `internal/httpapi/cast.go`: authenticated target cast endpoints.
- `cmd/remote-play-fbbridge/main.go`: ARMv7 RTP/H.264 receive, FFmpeg decode,
  framebuffer writes, and receiver statistics.
- `host/client.go`: authenticated target API client.
- `fogcast/config.go` and `internal/agentconfig/config.go`: strict host and
  target configuration schemas.

## Recommended next work

Choose one scope explicitly:

1. **Controller completion:** implement issue #3 behind a host-emulator input
   seam, with press/release lifecycle and attributable game-pixel HIL evidence.
2. **Library/control productization:** improve discovery and session UX while
   preserving the accepted media and lifecycle boundaries.
3. **Video-plane refinement:** iterate on scaling, presentation, or recovery
   using the retained hook, without redesigning POC4 transport absent new
   evidence. If this changes the bridge or hook binary, first establish a
   reproducible ARMv7/native-hook build and authorized deployment path.
4. **Physical latency:** close issues #1/#2 only when the required source/display
   fixture is available; record real glass-to-glass distributions.

For every change, test normal stop, startup rollback, sender death, bridge death,
target-agent shutdown, host-API shutdown, repeated launch, and stale-generation
replacement. A clean current-tree independent review remains the shipping gate.
