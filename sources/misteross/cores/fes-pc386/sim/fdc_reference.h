// SPDX-License-Identifier: MIT
// The floppy replay's reference model (docs/design/pc386-removable-media.md,
// "Device references"): Bochs's floppy controller (iodev/floppy.cc,
// LGPL-2.1-or-later), built from the pinned Bochs checkout the i386 oracle
// uses by scripts/sim_fes_pc386.py. Test-only; it never enters a package or
// an RTL source list.
#pragma once

#include <cstdint>
#include <memory>
#include <string>

namespace pc386 {

// One disk in a drive: a raw image file the model reads and writes, and its
// geometry.
struct FloppyDisk {
    std::string path;
    unsigned cylinders = 80, heads = 2, spt = 18;
    bool write_protected = false;
};

class FdcReference {
public:
    virtual ~FdcReference() = default;
    // Drive types are CMOS 10h codes (1 360K, 2 1.2M, 3 720K, 4 1.44M,
    // 5 2.88M); a hardware reset follows.
    virtual void power_on(unsigned type_a, unsigned type_b) = 0;
    // A hardware reset with the disks as they are (power on with them in).
    virtual void hardware_reset() = 0;
    virtual void out(uint16_t port, uint8_t value) = 0;
    virtual uint8_t in(uint16_t port) = 0;
    virtual bool irq() const = 0;
    virtual bool drq() const = 0;
    // One DMA byte: from the controller (a read command) or to it, with TC.
    virtual uint8_t dack_from(bool tc) = 0;
    virtual void dack_to(uint8_t value, bool tc) = 0;
    // Bench-owned time: the controller's timer fires once it is due.
    virtual void advance_us(uint64_t us) = 0;
    virtual void insert(unsigned drive, const FloppyDisk &disk) = 0;
    virtual void eject(unsigned drive) = 0;
    // A panic or fatal error in Bochs, which makes the case undefined there.
    virtual const std::string &panic() const = 0;
};

std::unique_ptr<FdcReference> make_fdc_reference();

}  // namespace pc386
