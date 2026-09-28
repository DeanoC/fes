# SG-1000 sound and the shared FES audio contract

Date: 2026-09-28
Status: proposed design for review

## Purpose and boundary

Give the package-only SG-1000 core audible SN76489-compatible sound while extending the existing declared FES audio contract to `fes.simple-computer`. Cores should normally supply signed PCM to the existing shared I2S output RTL. A core with a justified custom serializer may drive the same board I2S pins if it meets the same electrical, frame, sample, and silence rules. The HPS runtime alone configures the ADV7513 over I2C. This gives future cores a consistent host-facing capability without requiring all sound generators to use one implementation.

The first implementation slice covers the shared declaration, runtime admission and HDMI enablement, and SG-1000 PSG/output. SMS and ZX81 migrate in separate changes with their own evidence. The SG-1000 cartridge format, input mapping, video, ROM linking, and package-only standing stay as they are. This design introduces no host PCM upload, audio mailbox, mixing API, direct I2C audio transport, or factory-image promotion.

## Current paths and the gap

`fes.audio.pcm-s16-stereo-48k` 1.0 already exists for `fes.application` and `fes.computer`. It specifies signed 16-bit stereo samples at 48 kHz over board I2S, including Hold/reset silence; the runtime configures and enables ADV7513 audio after package identity succeeds. Coleco and Apple II use `fes_audio_output` with the 12.288 MHz audio PLL. SMS and ZX81 have core-local I2S serializers, but their `fes.simple-computer` ABI cannot declare audio and the runtime's simple-computer launch does not enable ADV7513 audio. Their existing serializers therefore do not establish a supported HDMI-audio package path. SG-1000 has neither PSG nor I2S outputs today.

The common interface is a **package and board contract**, not a requirement that every core instantiate the same Verilog module. I2S carries PCM to the HDMI transmitter; I2C programs transmitter registers. Both responsibilities remain separate.

## Shared declaration and compatibility

Add `fes.audio.pcm-s16-stereo-48k` 1.0 as a supported `fes.simple-computer` interface with capability bit 4, leaving bits 0–3 and `fes.simple-computer` ABI 1.0 unchanged. Presence is optional across packages but a package that emits audio lists the interface as **required**; silent packages omit it. `fes_computer_gp` gains an `ENABLE_AUDIO` parameter defaulting to zero, and an enabled instance reports bit 4 in its live identity. The sealed manifest, generated definitions, and live capability must agree exactly. An old runtime rejects an audio-requiring package during admission, before programming. Existing silent packages remain valid. No new GP opcode or payload is added.

The schema/YAML in mister-packages is the source of truth. Regenerate C++, Go, and Verilog consumers and golden oracles in the same FES change. Update all validators that enumerate recognized `fes.simple-computer` interfaces. The runtime advertises the new supported interface, admits it only at the defined version and when required, includes bit 4 in the expected identity, and rejects a missing, extra, or mismatched live bit. FogCast only needs updates where its generated contracts or package-validation fixtures enumerate the new interface; this does not add a network API.

The existing application/computer audio contract defines the transport for this simple-computer use as well: left channel while LRCLK is low, 16 signed sample bits MSB-first after one I2S delay bit, 32-bit slots with zero padding, 48,000 stereo frames/s, 3.072 MHz SCLK/BCLK, and 12.288 MHz MCLK. Board pins are HDMI I2S data T13, LRCLK T11, MCLK U11, SCLK T12 at 3.3 V. A different serializer is allowed only if it produces this same signal contract and is independently tested against it. This is not a second package interface.

## Lifecycle and silence

For every launch, runtime decides HDMI audio from the admitted required interface after live identity matches. It configures the ADV7513 48 kHz I2S path and enables audio packets while the identified core remains held, before releasing execution. A silent package receives video-only transmitter configuration. Stop and replacement hold/retire the core and disable packets before programming another bitstream; a failed identity never reaches audio enablement. Existing application/computer audio behavior remains unchanged.

The FPGA side emits zero samples at initial reset, on execution Hold, and after loss of valid audio clock. With a running clock, Hold must mute by the next stereo frame boundary after synchronization while clocks continue. A package with a custom serializer has the same duty; simply tying its data pin low without safe reset/Hold behavior is insufficient. There is no assumption that packet disable alone makes a malformed audio core safe.

## SG-1000 producer

The SG-1000 machine gains a signed 16-bit mono PSG sample, duplicated into left and right PCM channels. Reuse the existing common `fes_sn76489.sv` generator used by Coleco: its TI 15-bit noise behavior fits the original SG-1000 better than the SMS core's Sega 16-bit noise variant. Keep console address decode in the SG-1000 machine and leave SMS's generator and `0x7E`/`0x7F` decode unchanged. SG-1000 accepts writes throughout I/O `0x40–0x7F` and captures each CPU OUT bus cycle once even if TV80 holds the write strobe across enables. A read or memory access never writes the PSG. The PSG starts muted at reset. Its system-domain clock enable should target the SG-1000 nominal PSG rate near 3.579545 MHz from the existing 52 MHz domain; the present SMS `/16` approximation is about 3.25 MHz and must not silently be described as exact. This timing choice and the resulting measured rate belong in simulation evidence. The [SG-1000 port map](https://www.smspower.org/Development/IOPortMapSG) and [chip description](https://www.smspower.org/Development/SN76489) document the mapped writes and TI/Sega noise distinction.

The SG-1000 board top adds the existing `fes_audio_pll` and `fes_audio_output`, maps MCLK/SCLK/LRCLK/data to the established HDMI pins, and feeds `hold=exec_reset` with valid-lock handling. The shared output block transfers each stereo pair coherently into its audio clock domain. Set `ENABLE_AUDIO=1` only in the sound-capable SG-1000 top. Preserve the default-off GP parameter for existing simple-computer cores and tests. Add audio RTL, pin constraints, timing constraints, source hashes, and package required interface to both production OSS and maintained Quartus oracle recipes. Bump the SG-1000 package version for the changed capabilities and bitstream. The fixed 16 KiB linked cartridge and format-3 ROM map remain intact.

The additional PLL, pins, and routing may pressure timing. A successful simulation or Quartus oracle is not a substitute for the current locked OSS producer's authenticated route, exact pin checks, all required timing domains, and socket/ROM-map checks. If the route cannot close, keep the change in development and record the measured gap; do not weaken the package acceptance gate or switch the product path to Quartus.

## Migration order

1. **Shared contract and SG-1000:** extend the simple-computer ABI/runtime, enable SG-1000 capability and PSG, seal the SG-1000 package, and run an exact-artifact kit audio diagnostic if a leased kit and audible or captured output are available. An open synthetic ROM that writes known tone and noise registers avoids reliance on commercial ROMs.
2. **SMS:** separately declare and verify audio. Its existing serializer may stay if frame rate, slot alignment, cross-domain sample handling, and Hold/reset silence are demonstrated; otherwise migrate it to `fes_audio_output`. Preserve SMS port decode and require a new seal, timing result, and hardware classification.
3. **ZX81:** separately apply the same declaration and serializer choice to its own GP implementation and output path. Preserve current keyboard/tape behavior and independently qualify audio.

No earlier kit evidence transfers to a newly sealed RBF. A core is considered audio-capable only when its manifest, live identity, runtime setup, physical output, and dated exact-artifact check agree.

## Verification and acceptance

- **Shared and runtime:** schema/oracle/generated-consumer checks; package admission cases for silent and audio-required simple-computer packages; old or unsupported interface rejection; required-versus-optional validation; identity bit absent/extra/mismatch; transmitter I2C audio setup and packet enable only for verified audio packages; Hold/Stop/failure behavior. Parent `make check` and affected component tests cover the integrated commit.
- **RTL:** SG-1000 machine tests drive synthetic Z80 OUT instructions at the low and high ends of `0x40–0x7F`, reject outside/read/memory cycles, prove one write per held cycle, and inspect tone/noise register/sample responses. Verify reset/Hold/PLL-loss silence, L/R equality, 48 kHz frame timing, I2S delay/bit order, and no torn PCM pair. Re-run SMS PSG regression with its original decode and both SG-1000 default/OSS simulation lanes.
- **Build:** commit source first, then seal from that clean FES commit. Check source/provenance and ROM-map authentication, actual audio pad routing and electrical assignments, and system/pixel/audio timing with the pinned HIP/nextpnr path. Run the Quartus oracle where its recipe requires it. The FES parent package-only preparation does not by itself prove a hardware result.
- **Hardware:** with a designated leased kit, load the sealed package and an open tone/noise diagnostic ROM, observe HDMI video and audible or captured sound, verify mute across Stop and a silent-package transition, and record FES/runtime/RBF/image identities. If sound cannot be observed, report host/synthesis evidence only and leave hardware audio acceptance pending.

Owners: mister-packages owns the interface definition and generated contracts; libmister-runtime owns admission, identity and ADV7513 policy; misteross owns PSG, serializer selection, board pins and bitstream evidence; FES owns package registration/integration and exact-artifact handoff. FogCast remains the library/session owner, with no new audio transport in this design.
