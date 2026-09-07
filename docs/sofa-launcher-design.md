# On-kit controller launcher

Status: approved in conversation. Implementation and diagnostic integration are
complete on the designated kit. The launcher now consumes the merged shared
`fbgrid` primitive for a live 4×3 catalog view; the SDL layout remains owned by
the separate UI task. This task supplies host, runtime, controller and boot
interfaces plus the kit integration view.

## Outcome

With the configured FogCast host running, powering on the designated kit shows
a library on HDMI. A connected USB controller selects and launches Pong, controls
the game, and returns to the library by holding Select + Start for one second.
The same session path lists and launches supported Mega Drive and ordinary SNES
titles when available. Pong is the first complete hardware acceptance case.

The generated card contains launcher configuration. Users do not create files
or run SSH commands to use the launcher. An unavailable host produces a visible
connecting screen and automatic retry; this milestone requires the host for
catalogue and launch services and does not promise offline game launch.

## Findings

- FES main is d95bc62; selected FogCast is 98e4aa9, tree-equivalent to upstream
  main 7bbb334. No selected component was modified during investigation.
- FogCast's production tenfoot shell uses SDL3. Its pure-Go linuxfb command is
  a test-pattern/input spike, not a library launcher.
- The designated kit at 192.168.10.84 reports MiSTer_fb, 640×480, and USB pad
  081f:e401 at event0/js0. The virtual FogCast pad is a separate device.
- Writing framebuffer RAM does not enable its HDMI output. Native Menu video
  bring-up configures HDMI timing but never sends UIO_SET_FBUF.
- Native runtime gameplay input selects FogCast's virtual pad. Reading the
  physical pad for menu navigation alone would not make Pong playable.
- The public host HTTP server binds only to loopback and rejects other Host
  headers. An on-kit client cannot simply use the workstation's LAN address.
- Remote branch feat/tenfoot-linuxfb-grid currently equals upstream main. Its
  name may indicate another task; coordinate ownership before overlapping edits.

## Approach and alternatives

Use a small pure-Go on-kit shell over the existing graphics, catalogue, session,
and remote-input services. Add an explicitly configured, authenticated launcher
connection to the existing host process. Keep physical transitions in the native
runtime and keep the host as the session/kit-lease owner.

Running the whole host/catalogue on the ARM kit would change storage and resource
assumptions and duplicate the workstation's library. An SSH tunnel can help a
diagnostic but introduces tunnel provisioning and supervision into normal boot.
Neither is selected for the product path.

## Component responsibilities

### FogCast launcher and host connection

Introduce a CGO-free Linux launcher command using the existing software/linuxfb
graphics backend and tenfoot client/model helpers where independent of SDL.
Render the live catalog as a bounded 4×3 grid with a focused title, safe-area
margins, and explicit connecting, empty library, loading, playing, busy, and
retry-Stop states. Full SDL settings, attract video, and preview parity are
outside this slice.

The existing host process gets an opt-in LAN listener restricted to the
launcher operations: library/platform reads, connection/session reads,
launch/Stop, and controller input. Reuse existing application services. Keep
the browser listener loopback-only. Authenticate the launcher with a separately
generated secret, tied to the configured target identity; do not expose general
settings, filesystem paths, or development-RBF upload on this listener.

Generate the secret and launcher configuration during explicit host/kit setup,
then include the prepared configuration in normal media assembly. Store secrets
in owner-only files and exclude their values from manifests/logs. Host address
and port are explicit configuration for this milestone; target discovery does
not imply that host discovery already exists.

### Controller delivery and session ownership

Read physical evdev devices, excluding the virtual FogCast pad and avoiding
duplicate event/js reads. Normalize the fixture's actual buttons and axis ranges;
do not assume BTN_SOUTH or signed axes on the 081f:e401 pad. Handle unplug/replug
without restarting. Menu confirm fires on a fresh press, not held-state startup.

During a game, send normalized input through a bounded launcher input stream
to the host's existing RemoteInput.SendEvent path. The host retains kit/input
leases and forwards through the existing agent virtual pad. No lease token is
handed to the launcher and no second session owner is introduced. This first
path adds a LAN round trip for a locally attached pad; measure it during the
Pong test and report responsiveness honestly.

Bind the input stream to the active session and one source. A session change,
launcher disconnect, controller disconnect, or stream timeout neutralizes held
input. Never replay queued input into a later game. Existing desktop remote
input and kit-controller input must not silently compete.

Holding Select + Start for one second requests ordinary host Stop once per
hold. Individual Start/Select still retain gameplay meaning. Require release
before rearming the shortcut. Do not map East/B to Stop during gameplay.
Stop/save failure preserves the session and retry path; it cannot be displayed
as a successful return to the library.

### libmister-runtime display lifecycle

Enable a fixed 640×480 32bpp HPS framebuffer as part of native idle Menu
bring-up, using the existing HDMI output timing and owned SPI interface.
Validate framebuffer geometry, stride, and backing address. Reapply the setup
after every successful Stop that reloads Menu. Game programming owns the display
transition; the launcher never sends SPI commands or programs the FPGA.

First perform a bounded diagnostic of the MiSTer framebuffer mode and Menu
status/enable sequence using the Main comparison implementation. Exact words
and sequencing are not declared hardware-proven by this design. Successful
idle bring-up must include successful framebuffer setup before publishing idle.

The launcher suspends framebuffer presentation while a game/development core
owns HDMI and resumes only after confirmed idle. It does not draw a now-playing
screen over gameplay. Read/reopen framebuffer geometry on return as needed.

### FES assembly

Package/supervise the launcher after runtime and agent startup. It must tolerate
late networking and late host startup. Generate its configuration through the
existing provisioned-media workflow. Update component locks/pins together only
when reviewed component commits are available and committing is authorized.

Reuse unchanged FPGA bundles, compiler, and base packages for development.
Changes to package contents/init require the stabilized full image build before
formal acceptance. Diagnostic images remain explicitly separate evidence.

## Validation and completion

1. Focused tests cover input normalization/chord timing, reconnect and
   neutralization, authentication/route restriction, session ownership, and
   display setup/Stop ordering. Tests never open real framebuffer/input devices.
2. Cross-build the ARM launcher; run parent consistency checks against selected
   revisions when available. Do not claim uncommitted worker changes were
   tested by a parent pinned build.
3. Under the existing kit lease, verify a visible framebuffer diagnostic,
   then boot into the launcher with generated configuration.
4. With the real USB controller: browse, launch Pong, move paddles, hold the
   shortcut, return, and relaunch. Capture settled HDMI output. Simulated input
   is useful for automation but does not replace physical-controller acceptance.
5. Exercise controller unplug/replug and host loss/recovery. Confirm held
   buttons release and no old input reaches a new session. Confirm busy leases
   are respected; do not take over another task's kit.
6. Regress existing Mega Drive/SNES launch and Stop when test content exists,
   including ordinary SNES save behavior. Finish idle with the lease free.

For root-image deployment, retain the live image under a backup filename before
installing the staged image; delete it only after a verified new boot. Never
overwrite or unlink the live loop-backed image's last filename.

## Delivery boundary

Use separate component worktrees under out/dev/sofa-launcher. Preserve unrelated
branches, especially misteross/nextpnr work. No new FPGA synthesis is planned
unless the existing Menu core demonstrably lacks the required framebuffer ABI.
Do not commit, push, or open PRs until authorized. The host and runtime interfaces described here were approved before implementation.
