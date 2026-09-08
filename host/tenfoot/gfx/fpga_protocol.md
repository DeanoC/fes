# FogCast 2D UI command list (FC2D)

This is the versioned command stream a tenfoot `gfx.Device` can emit for a
future MiSTer custom 2D accelerator. The Go encoder and decoder live in this
package (`EncodeStream`, `DecodeStream`, `Replay`).

**Hardware status:** there is no programmed 2D core in this slice. `gfx.NewFPGA`
records this stream and rasters through Software (`RasterMode` =
`software-replay`). `IsStub()` stays true until a real mailbox/DMA transport
exists. This document does not claim HDMI FPGA UI is live on silicon.

Bitstream, Main_MiSTer, and misteross RBF work stay on the separate FPGA kit
track. FogCast owns host-side encode, software replay, and UI primitives.

## Version

| Field | Value |
| --- | --- |
| Magic | `FC2D` (4 ASCII bytes) |
| Version | `1` (`uint16` little-endian) |
| Endianness | little-endian |
| Pixel format 0 | tightly packed RGBA8, stride = width × 4 |

A decoder must reject any other magic or version. There is no silent fallback.

## Stream layout

```
Header (16 bytes)
  magic      [4]byte   "FC2D"
  version    u16       1
  flags      u16       see Flags
  logical_w  u16       Device logical width
  logical_h  u16       Device logical height
  reserved   u32       0

Command*
  op         u8
  flags      u8        per-command bits
  nbytes     u16       payload length; 0xFFFF means extended
  [if nbytes == 0xFFFF]
    nbytes32 u32       actual payload length
  payload    nbytes (or nbytes32) bytes
```

`nbytes == 0xFFFF` is required whenever the payload is 65535 bytes or larger
(texture uploads). Smaller payloads use the 16-bit length. The decoder treats
`0xFFFF` as extended even if a 65535-byte payload could have used a bare
`u16`.

Commands are packed with no padding. A future bitstream may copy each command
into a 4-byte-aligned staging buffer; v1 does not insert pad bytes.

## Flags

Header `flags`:

| Bit | Name | Meaning |
| --- | --- | --- |
| 0 | `software_replay` | Stream was produced with CPU raster (today's path). |
| 1 | `hardware` | Proposed: submitted to a programmed 2D core. Not set in this slice. |

Command `flags` for `Draw` (op `0x05`):

| Bit | Name | Meaning |
| --- | --- | --- |
| 0 | `has_src` | Payload source rect is used; otherwise Draw samples the full texture. |

Other commands ignore command flags in v1.

## Operations

Rectangles are four IEEE-754 `float32` values `x, y, w, h` in logical pixels,
matching `gfx.Rect`. A bitstream may snap them to 16.16 fixed point; the v1
encoder stores the Device floats losslessly.

Colours are `r, g, b, a` bytes. `a = 255` is opaque.

Texture ids are `u32` and match the Device handle the encoder assigned.

| Op | Name | Payload |
| --- | --- | --- |
| `0x01` | BeginFrame | `frame u32` (1-based index in this stream) |
| `0x02` | Clear | `rgba[4]` |
| `0x03` | SetBlend | `mode u8` (`0` = replace, `1` = source-over alpha) |
| `0x04` | FillRect | `x,y,w,h f32` + `rgba[4]` |
| `0x05` | Draw | `tex_id u32`, `has_src u8`, `pad[3]`, `src x,y,w,h f32`, `dst x,y,w,h f32` |
| `0x06` | Present | empty |
| `0x10` | CreateTexture | `tex_id u32`, `w u16`, `h u16`, `format u8`, `pad u8`, `crc32 u32`, `pixels[w*h*4]` |
| `0x11` | UpdateTexture | same as CreateTexture |
| `0x12` | DestroyTexture | `tex_id u32` |
| `0x20` | DebugText | `x i32`, `y i32`, `scale u8`, `pad[3]`, UTF-8 bytes |
| `0x21` | DrawText | `x i32`, `y i32`, `size_px u16`, `pad[2]`, `rgba[4]`, UTF-8 bytes |
| `0xFF` | Close | empty |

`format` `0` is RGBA8. `crc32` is IEEE CRC of the tightly packed pixel bytes.
Replay rejects a mismatch. An unknown `op` is an error.

`CreateTexture` / `UpdateTexture` inline pixels so encode/decode and CI work
without a DMA heap. That is a software convenience. A programmed core may
refuse large inline payloads; see proposed v2 below.

`DrawText` is the CGO-free UI face (embedded Go Regular, `size_px`, RGBA). It
is recorded for software replay. It is not a hardware text accelerator; a
programmed 2D core may raster it through Software until a later opcode exists.
`DebugText` stays the 8×8 HUD path.

`FillRect` honours the last `SetBlend`. Textured `Draw` uses source-over of
the texture's alpha, matching Software and SDL today.

## Device mapping

| `gfx.Device` | Command |
| --- | --- |
| `BeginFrame` | `BeginFrame` |
| `Clear` | `Clear` |
| `SetBlend` | `SetBlend` |
| `FillRect` | `FillRect` |
| `Draw` | `Draw` |
| `Present` | `Present` |
| `CreateRGBA` | `CreateTexture` |
| `UpdateRGBA` | `UpdateTexture` |
| `Destroy` | `DestroyTexture` |
| `DebugText` | `DebugText` |
| `DrawText` | `DrawText` |
| `Close` | `Close` |

`gfx.Replay` / `ReplayBytes` apply a decoded stream to any Device (Software
for tests, linuxfb for a kit proof). Texture ids in the stream are mapped onto
the destination Device's handles.

## Proposed mailbox / registers / DMA

The following is **proposed**. It is not implemented in FogCast, Main_MiSTer,
or a FogCast-owned RBF in this PR.

A future custom core in the FPGA fabric would consume this command list. The
HPS would submit one stream per frame through a small Avalon-MM slave and
DMA-visible SDRAM. Suggested 32-bit registers:

| Offset | Name | Role |
| --- | --- | --- |
| `0x00` | `VERSION` | Core protocol version; must match stream version. |
| `0x04` | `CTRL` | bit0 start, bit1 reset, bit2 IRQ enable. |
| `0x08` | `STATUS` | bit0 busy, bit1 vsync, bit2 error, bit3 mailbox full. |
| `0x0C` | `CMD_ADDR` | SDRAM byte address of the stream (64-byte aligned). |
| `0x10` | `CMD_BYTES` | Stream length. |
| `0x14` | `TEX_ADDR` | Texture heap base (DMA-visible). |
| `0x18` | `FB_ADDR` | Output framebuffer. |
| `0x1C` | `FB_STRIDE` | Bytes per row. |
| `0x20` | `FB_WH` | width `[15:0]`, height `[31:16]`. |
| `0x24` | `DOORBELL` | Write 1 to submit `CMD_ADDR` / `CMD_BYTES`. |
| `0x28` | `IRQ_STATUS` | Write 1 to clear vsync/error. |

Proposed submit sequence:

1. Host encodes FC2D v1 (`gfx.EncodeStream`).
2. Host copies texture pixels into DMA-visible SDRAM when the core wants a heap.
3. Host writes `CMD_ADDR`, `CMD_BYTES`, `TEX_ADDR`, and `FB_*`.
4. Host rings `DOORBELL`.
5. Core parses commands, blits through the 2D pipeline, waits vblank, raises IRQ.
6. `Present` completes when `STATUS.vsync` is set.

Until that core exists, `NewFPGA` rasters with Software, header bit
`software_replay` is set, `hardware` is clear, and `IsStub()` is true.

### Proposed v2 heap descriptors

v2 (not implemented) would keep the same ops but replace inline pixels with a
heap offset:

```
CreateTexture v2 payload
  tex_id u32, w u16, h u16, format u8, pad u8, crc32 u32, heap_off u32, byte_len u32
```

The HPS would DMA pixels to `TEX_ADDR + heap_off` before the doorbell. v1
stays the software/CI stream.

## Honesty labels

| Path | Label |
| --- | --- |
| `gfx.NewFPGAStub` | `fpga-stub` — thin Software wrapper, no command stream |
| `gfx.NewFPGA` | `fpga` — records FC2D v1, software-replay raster, `IsStub() == true` |
| Programmed 2D core | not in this tree; `IsStub()` would be false only with a live transport |

Do not report “hardware FPGA 2D live” from the `fpga` backend in this slice.
