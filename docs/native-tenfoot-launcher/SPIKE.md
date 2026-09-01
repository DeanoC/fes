# Spike: native 10-foot launcher (SDL3)

Branch: `feat/native-tenfoot-launcher` (off `main` @ 47ba2d4 / #91).
Do **not** land on `main` until ready to PR. Kit / MiSTer / mailbox stay on Deano's separate Codex track — this branch is frontend-only.

## Goal (~1 week)

Native Big Box / Steam Big Picture–style cover grid on Mac (Linux next). **No browser** for render or gamepad. Talk to the existing FogCast Go host API over HTTP (same host that serves today's `ui_shell`).

## Stack (locked for this spike)

- **SDL3** + small retained UI (not Electron / Tauri / Flutter / Qt for v1)
- Gamepad-first focus graph (d-pad / sticks), keyboard OK for debug
- Async cover loads; don't block the frame loop
- Launch path: call existing FogCast host API (same endpoints the web UI uses) — do not reimplement RetroArch / MiSTer plumbing

## Done when

1. Cover grid shows library titles from the live host API
2. Gamepad-only navigate + select works
3. Selecting a title starts a launch through FogCast host (prove end-to-end on Mac against local host)
4. README in this folder: how to build/run, API base URL, known gaps (GPU release on launch, TV safe area = later)

## Out of scope

- Kit / FPGA / misteross / Main_MiSTer changes
- Replacing the browser shell on `main`
- Themes / QML / ES-DE fork
- 10-foot TV polish (safe area, overscan) beyond "it runs fullscreen"

## Executor

SuperGrok Build (Luna under Foggy). Not Codex. Not Grok Bot tokens.
