// SPDX-License-Identifier: MIT
// Adapter from pc386::FdcReference to Bochs's floppy controller.
// scripts/sim_fes_pc386.py compiles this file with Bochs's iodev/floppy.cc
// from the pinned, configured checkout the i386 oracle uses; Bochs stays in
// the ignored build directory.
//
// Bochs's controller normally runs inside its device manager, with the DMA
// controller, PIC and timers around it. Here the bench is all of those: the
// DMA stub records DRQ and answers TC, the PIC stub records IRQ 6, the hard
// disk stub answers 3F7h's low bits as the real one does (FFh), and the
// controller's one timer fires when the bench's time passes its deadline
// (an activation of 0 us takes the 250 us period it was registered with, as
// Bochs's pc_system does). The controller's state is private in floppy.h, so
// the adapter opens it to reach the port and DMA handlers and to load media
// the way init() and set_media_status() do, without the parameter tree.
#include "fdc_reference.h"

#include "bochs.h"
#include "iodev/iodev.h"
#include "pc_system.h"
#include "gui/gui.h"
#include "iodev/hdimage/hdimage.h"
#define private public
#include "iodev/floppy.h"
#undef private

#include <cstdarg>
#include <cstdio>
#include <fcntl.h>
#include <stdexcept>
#include <string>
#include <unistd.h>

extern bx_floppy_ctrl_c *theFloppyController;

namespace {

std::string format(const char *fmt, va_list ap) {
    char buffer[512];
    std::vsnprintf(buffer, sizeof buffer, fmt, ap);
    return buffer;
}

struct Panic : std::runtime_error {
    using std::runtime_error::runtime_error;
};

class Dma : public bx_dma_stub_c {
public:
    bool drq = false, tc = false;
    bool get_TC() override { return tc; }
    void set_DRQ(unsigned channel, bool value) override { if (channel == 2) drq = value; }
};
class Pic : public bx_pic_stub_c {
public:
    bool irq6 = false;
    void raise_irq(unsigned irq, Bit8u) override { if (irq == 6) irq6 = true; }
    void lower_irq(unsigned irq, Bit8u) override { if (irq == 6) irq6 = false; }
};
class HardDisk : public bx_hard_drive_stub_c {
public:
    Bit32u virt_read_handler(Bit32u, unsigned) override { return 0xff; }
};
Dma reference_dma;
Pic reference_pic;
HardDisk reference_hd;

class Parameters : public bx_simulator_interface_c {};
Parameters reference_parameters;

// The controller's timer.
struct Timer {
    bool active = false;
    Bit64u deadline = 0;
} reference_timer;
Bit64u reference_usec = 0;

}  // namespace

// ---- Platform objects floppy.cc links against ------------------------------

static logfunctions reference_log;
logfunctions *genlog = &reference_log;
logfunctions *pluginlog = &reference_log;
logfunctions *siminterface_log = &reference_log;
bx_simulator_interface_c *SIM = &reference_parameters;
bx_devices_c bx_devices;
bx_pc_system_c bx_pc_system;
bx_gui_c *bx_gui = nullptr;
bx_list_c *root_param = nullptr;
// No status bar (statusbar_id is -1) and no VVFAT image (raw files only).
int bx_gui_c::register_statusitem(const char *, bool) { return -1; }
void bx_gui_c::statusbar_setitem(int, bool, bool) {}
bx_hdimage_ctl_c::bx_hdimage_ctl_c() {}
device_image_t *bx_hdimage_ctl_c::init_image(const char *, Bit64u, const char *) { return nullptr; }
bx_hdimage_ctl_c bx_hdimage_ctl;
void pluginRegisterDeviceDevmodel(plugin_t *, Bit16u, bx_devmodel_c *, const char *) {}
bool bx_user_quit = false;
void bx_exit(int) { throw Panic("Bochs called bx_exit"); }

logfunctions::logfunctions() : name(nullptr), prefix(nullptr), logio(nullptr) {}
logfunctions::logfunctions(iofunctions *) : logfunctions() {}
logfunctions::~logfunctions() {}
void logfunctions::put(const char *) {}
void logfunctions::put(const char *, const char *) {}
void logfunctions::ldebug(const char *, ...) {}
void logfunctions::info(const char *, ...) {}
void logfunctions::lwarn(const char *, ...) {}
void logfunctions::error(const char *, ...) {}
void logfunctions::panic(const char *fmt, ...) {
    va_list ap; va_start(ap, fmt); std::string message = format(fmt, ap); va_end(ap);
    throw Panic(message);
}
void logfunctions::fatal1(const char *fmt, ...) {
    va_list ap; va_start(ap, fmt); std::string message = format(fmt, ap); va_end(ap);
    throw Panic(message);
}

bx_pc_system_c::bx_pc_system_c() {}
int bx_pc_system_c::Reset(unsigned) { return 0; }
int bx_pc_system_c::register_timer(void *, bx_timer_handler_t, Bit32u, bool, bool, const char *) { return 0; }
void bx_pc_system_c::activate_timer(unsigned, Bit32u useconds, bool) {
    reference_timer.active = true;
    reference_timer.deadline = reference_usec + (useconds ? useconds : 250);
}
void bx_pc_system_c::deactivate_timer(unsigned) { reference_timer.active = false; }

bx_devices_c::bx_devices_c() {
    init_stubs();
    pluginDmaDevice = &reference_dma;
    pluginPicDevice = &reference_pic;
    pluginHardDrive = &reference_hd;
}
bx_devices_c::~bx_devices_c() {}
void bx_devices_c::init_stubs() {
    pluginCmosDevice = &stubCmos;
    pluginDmaDevice = &stubDma;
    pluginHardDrive = &stubHardDrive;
    pluginPicDevice = &stubPic;
    pluginPitDevice = &stubPit;
    pluginSpeaker = &stubSpeaker;
    pluginVgaDevice = &stubVga;
#if BX_SUPPORT_IODEBUG
    pluginIODebug = &stubIODebug;
#endif
#if BX_SUPPORT_GAMEPORT
    pluginGameport = &stubGameport;
#endif
    pluginExtFpuIRQ = &stubExtFpuIRQ;
}
bool bx_devices_c::register_io_read_handler(void *, bx_read_handler_t, Bit32u, const char *, Bit8u) { return true; }
bool bx_devices_c::register_io_write_handler(void *, bx_write_handler_t, Bit32u, const char *, Bit8u) { return true; }
bool bx_devices_c::register_irq(unsigned, const char *) { return true; }

namespace {

class Reference final : public pc386::FdcReference {
public:
    ~Reference() override {
        for (unsigned drive = 0; drive < 2 && theFloppyController; ++drive) eject_quietly(drive);
    }

    void power_on(unsigned type_a, unsigned type_b) override {
        // What bx_floppy_ctrl_c::init does for two drives, without the
        // parameter tree; the controller from the last case is left behind
        // (its destructor reaches the parameter tree).
        for (unsigned drive = 0; drive < 2 && theFloppyController; ++drive) eject_quietly(drive);
        theFloppyController = new bx_floppy_ctrl_c();
        auto &s = theFloppyController->s;
        s.floppy_timer_index = 0;
        s.num_supported_floppies = 2;
        for (unsigned i = 0; i < 4; ++i) {
            s.media[i].fd = -1;
            s.media[i].type = BX_FLOPPY_NONE;
            s.media_present[i] = 0;
            s.device_type[i] = FDRIVE_NONE;
        }
        s.device_type[0] = type_a ? Bit8u(1u << (type_a - 1)) : Bit8u(FDRIVE_NONE);
        s.device_type[1] = type_b ? Bit8u(1u << (type_b - 1)) : Bit8u(FDRIVE_NONE);
        s.statusbar_id[0] = s.statusbar_id[1] = -1;
        reference_timer = Timer{};
        reference_dma.drq = reference_dma.tc = false;
        reference_pic.irq6 = false;
        guard([] { theFloppyController->reset(BX_RESET_HARDWARE); });
    }
    void hardware_reset() override {
        guard([] { theFloppyController->reset(BX_RESET_HARDWARE); });
    }
    void out(uint16_t port, uint8_t value) override {
        guard([&] { bx_floppy_ctrl_c::write_handler(theFloppyController, port, value, 1); });
    }
    uint8_t in(uint16_t port) override {
        Bit32u value = 0xff;
        guard([&] { value = bx_floppy_ctrl_c::read_handler(theFloppyController, port, 1); });
        return uint8_t(value);
    }
    bool irq() const override { return reference_pic.irq6; }
    bool drq() const override { return reference_dma.drq; }
    uint8_t dack_from(bool tc) override {
        Bit8u value = 0xff;
        reference_dma.tc = tc;
        guard([&] { bx_floppy_ctrl_c::dma_write_handler(theFloppyController, &value, 1); });
        reference_dma.tc = false;
        return value;
    }
    void dack_to(uint8_t value, bool tc) override {
        Bit8u byte = value;
        reference_dma.tc = tc;
        guard([&] { bx_floppy_ctrl_c::dma_read_handler(theFloppyController, &byte, 1); });
        reference_dma.tc = false;
    }
    void advance_us(uint64_t us) override {
        reference_usec += us;
        if (reference_timer.active && reference_usec >= reference_timer.deadline) {
            reference_timer.active = false;
            guard([] { bx_floppy_ctrl_c::timer_handler(theFloppyController); });
        }
    }
    void insert(unsigned drive, const pc386::FloppyDisk &disk) override {
        // set_media_status(drive, 1) with evaluate_media's raw-image result.
        auto &s = theFloppyController->s;
        auto &media = s.media[drive];
        eject_quietly(drive);
        media.fd = open(disk.path.c_str(), disk.write_protected ? O_RDONLY : O_RDWR);
        if (media.fd < 0) throw std::runtime_error("cannot open " + disk.path);
        media.tracks = disk.cylinders;
        media.heads = disk.heads;
        media.sectors_per_track = disk.spt;
        media.sectors = disk.cylinders * disk.heads * disk.spt;
        media.write_protected = disk.write_protected;
        media.vvfat_floppy = 0;
        media.type = disk.spt == 18 ? BX_FLOPPY_1_44 : disk.spt == 15 ? BX_FLOPPY_1_2 :
                     disk.spt == 36 ? BX_FLOPPY_2_88 : disk.cylinders == 80 ? BX_FLOPPY_720K : BX_FLOPPY_360K;
        s.media_present[drive] = 1;
    }
    void eject(unsigned drive) override {
        // set_media_status(drive, 0).
        eject_quietly(drive);
        theFloppyController->s.DIR[drive] |= 0x80;
    }
    const std::string &panic() const override { return panic_; }

private:
    void eject_quietly(unsigned drive) {
        auto &media = theFloppyController->s.media[drive];
        if (media.fd >= 0) close(media.fd);
        media.fd = -1;
        theFloppyController->s.media_present[drive] = 0;
    }
    template <typename F> void guard(F &&f) {
        if (!panic_.empty()) return;   // Bochs's state after a panic is undefined
        try { f(); } catch (const Panic &e) { panic_ = e.what(); }
    }
    std::string panic_;
};

}  // namespace

std::unique_ptr<pc386::FdcReference> pc386::make_fdc_reference() { return std::make_unique<Reference>(); }
