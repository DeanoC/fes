# Source build validation, 2026-09-05

The `native-source-dev` integration was built and tested on powerboat from
the parent working tree on `feat/native-parent`, before publication as `DeanoC/fes`.
This is a bounded integration check, not a replacement for the component's
full hardware acceptance suite.

## Inputs and build results

- FogCast: `cd85971bf0bffe36e69c381917f618620b901726`
- libmister-runtime: `443b603de991b56b5f4d0d11c5bc88a3f83fad13`
- misteross: `7912a3e7ee82a24c9aed82dfbb30a8974b7eec07`
- Quartus: `Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition`
- Mega Drive source: `7365a137cfd8fa6f041e964d8b953159c0ec42d9`
- Exported RBF: `195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e`, 4,306,912 bytes.
- Both clean image passes: `5e8d059892051f1f8490cd0666339774862c6342af38dc7e3fd74bfd78940d00`.

Three Quartus invocations reproduced the same RBF. The successful complete
image build reused that validated export after earlier integration failures
exposed missing online package-fetch and agent-build steps; both steps are
now explicit. Each image pass compiled from a fresh Buildroot work directory
with networking disabled. A new `make verify PROFILE=native-source-dev`
invocation passed structural, two-pass hash and QEMU packaging gates.
A subsequent `make build PROFILE=native-source-dev` reused host and image.
All ten parent tests and `git diff --check` passed.

The bundle recipe digest was checked against the pinned
`scripts/rebuild_core.py`: `3407571e63834158c71cbdcb891cde885b249bbc52a418f55f940ac95b4f6064`.
The generated FogCast lock changes its Mega Drive hash/size and records the
source-bundle kind and recipe digest. The child's installed `build-inputs`
still retains its upstream release-path field; the adjacent bundle manifest
and parent `inputs.json` describe the source rebuild provenance.

## Hardware check

The parent-built API ran on `127.0.0.1:8789` with the existing target
configuration. Before deployment, it launched Sonic 2 on the installed
baseline and captured a recognizable title screen, then stopped to idle.
The baseline image was saved as `out/hardware-backup/linux.img`, SHA-256
`12302f3c84df43c032608dc92742db41113b5646821cfbe4d9ba6a643875a0c1`.

The exact verified source image was deployed to `192.168.10.239`. Target-side
hashing confirmed the image and RBF hashes above. Installed runtime SHA-256
was `f100441df7b50df5aa3a7659ea41773ad36501dabafc678688fe444e120e9098`;
agent SHA-256 was `bd458351fcc7a2bcd4f913b6b4c8880669b891b35ad9cbaa47fa260b9fc552be`.
The FPGA manager reported `operating`.

Public host launch used game ID
`megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1` and returned active
native Mega Drive execution. A local helper used the pinned production
remote-input library. After early Start attempts landed in the attract
sequence, a capture gate matched the title screen and dispatched Start.
Opened captures showed Emerald Hill gameplay, rightward movement, ring
collection and a jump. Input transport reported no sequence gaps or state
resyncs. Stop and immediate relaunch returned idle and active respectively;
the tested gameplay followed a relaunch on the same boot.

The child's native-runtime smoke checked the installed input identities,
runtime/agent process ownership, absence of Main/command FIFO, Stop to idle,
and reboot to fresh ready idle. It passed with boot ID changing from
`5218c9f8-2263-4611-a482-a08b366f6c5f` to
`d9ecae53-af1d-4e77-b691-82e91b1ebd97`.

The source image remains installed and idle. The temporary test API is
stopped after validation. The original installed host process remains.
This check does not establish audio quality, every game, or a long soak.

## Local evidence

Under `out/`: `native-source-build-complete.log`, `native-source-verify.log`,
`native-source-reuse.log`, `source-parent-tests.log`, and the profile's
receipts, manifests, bundle and `verification.json`.

Under `out/hardware-evidence/`: `installed.txt`, `control-launch.json`,
`control-003.png`, `source-launch.json`, `source-relaunch.json`,
`gated-title.png`, `gated-gameplay.png`, `title-gate.log`, `input-move.log`,
`move-015.png`, `move-020.png`, `post-input-stop.json`,
`post-input-relaunch.json`, `final-stop.json`, and
`native-runtime-smoke.log`. Capture startup emitted some MJPEG decode
warnings; the named inspected frames were readable and showed game output.
