// SPDX-License-Identifier: MIT
// The VGA trace replay's reference model (docs/pc386-machine-design.md,
// "Verification", item 3): Bochs's standard VGA (iodev/display/vgacore.cc,
// LGPL-2.1-or-later) with its common GUI renderer (gui/gui.cc), built from
// the pinned Bochs checkout by scripts/pc386_vga_reference.py. Test-only; it
// never enters a package or an RTL source list.
#pragma once

#include "pc386_presenter_model.h"

#include <cstdint>
#include <memory>

namespace pc386 {

class VgaReference {
public:
    virtual ~VgaReference() = default;
    // Ports Bochs's VGA claims (3B4h, 3B5h, 3BAh, 3C0h-3CFh, 3D4h, 3D5h,
    // 3DAh); the rest of 3B0h-3DFh is unclaimed and reads FFh.
    static bool claims(uint16_t port) {
        return port == 0x3b4 || port == 0x3b5 || port == 0x3ba || (port >= 0x3c0 && port <= 0x3cf) ||
               port == 0x3d4 || port == 0x3d5 || port == 0x3da;
    }
    virtual void out(uint16_t port, uint8_t value) = 0;
    virtual uint8_t in(uint16_t port) = 0;
    virtual void mem_write(uint32_t address, uint8_t value) = 0;   // physical, A0000h-BFFFFh
    virtual uint8_t mem_read(uint32_t address) = 0;
    // The CRTC start address latch, which the bench applies when the RTL
    // latches it (at vertical retrace).
    virtual void latch_start() = 0;
    // One display update, as Bochs's VSYNC-paced timer makes it, with the
    // blink toggling every eight updates. The frame is what Bochs's display
    // shows, 6-bit DAC values widened by replication; false when Bochs skips
    // the update (display or sequencer off).
    virtual bool render(Frame &frame) = 0;
    virtual bool text_mode() const = 0;
    // Bochs's update draws an 80 x 25, 16-line text screen while the CRTC's
    // display end or maximum scan line is still zero.
    virtual bool unprogrammed() const = 0;
    virtual bool blink_enabled() const = 0;
    // Bochs blinks the cursor and blinking characters together: visible or
    // not in the last render. Its counter stops in graphics modes.
    virtual bool blink_visible() const = 0;
    virtual const uint8_t *vram() const = 0;   // 256 KiB: planar offset * 4 + plane
};

std::unique_ptr<VgaReference> make_vga_reference();

}  // namespace pc386
