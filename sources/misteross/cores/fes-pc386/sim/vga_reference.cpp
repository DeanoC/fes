// SPDX-License-Identifier: MIT
// Adapter from pc386::VgaReference to Bochs's standard VGA. scripts/
// pc386_vga_reference.py compiles this file with Bochs's iodev/display/
// vgacore.cc and gui/gui.cc from the pinned, configured checkout the i386
// oracle uses; Bochs stays in the ignored build directory.
//
// Bochs's VGA normally runs inside its device manager with a timer-driven
// GUI. Here the bench owns time: Bochs's timers never fire, the bench applies
// the start address latch when the RTL latches it and asks for one display
// update per RTL frame, with Bochs's VSYNC update mode and a cursor blink of
// eight frames. The GUI renders through Bochs's own text renderer
// (bx_gui_c::text_update_common and draw_char_common) and tile copies into an
// 8-bit index frame, mapped through the DAC.
#include "vga_reference.h"

#include "bochs.h"
#include "gui/gui.h"
#include "gui/keymap.h"
#include "iodev/iodev.h"
#include "iodev/virt_timer.h"
#include "iodev/display/vgacore.h"
#include "memory/memory-bochs.h"
#include "pc_system.h"

#include <cstdarg>
#include <cstdio>
#include <cstring>
#include <stdexcept>
#include <string>
#include <vector>

namespace {

std::string format(const char *fmt, va_list ap) {
    char buffer[512];
    std::vsnprintf(buffer, sizeof buffer, fmt, ap);
    return buffer;
}

// The few parameters Bochs's VGA and GUI read: VSYNC-paced updates, no
// realtime clock sync, no extension, the default mouse toggle.
class Parameters : public bx_simulator_interface_c {
public:
    // Built on first use: a parameter's constructor reaches SIM.
    bx_param_bool_c *get_param_bool(const char *, bx_param_c *) override {
        static bx_param_bool_c flag(nullptr, "fes_flag", "", "", 0);
        return &flag;
    }
    bx_param_num_c *get_param_num(const char *, bx_param_c *) override {
        static bx_param_num_c number(nullptr, "fes_number", "", "", 0, 1000, 0);
        return &number;
    }
    bx_param_enum_c *get_param_enum(const char *, bx_param_c *) override {
        static const char *choices[] = {"none", nullptr};
        static bx_param_enum_c choice(nullptr, "fes_choice", "", "", choices, 0, 0);
        return &choice;
    }
    bx_param_string_c *get_param_string(const char *, bx_param_c *) override {
        static bx_param_string_c text(nullptr, "fes_text", "", "", "", 256);
        return &text;
    }
    int split_option_list(const char *, const char *, char **, int) override { return 0; }
};
Parameters reference_parameters;

// init_standard_vga() records the VGA in CMOS byte 14h.
class Cmos : public bx_cmos_stub_c {
public:
    Bit32u get_reg(Bit8u) override { return 0; }
    void set_reg(Bit8u, Bit32u) override {}
};
Cmos reference_cmos;

}  // namespace

// ---- Platform objects Bochs's VGA and GUI link against --------------------

static logfunctions reference_log;
logfunctions *genlog = &reference_log;
logfunctions *pluginlog = &reference_log;
logfunctions *siminterface_log = &reference_log;
bx_simulator_interface_c *SIM = &reference_parameters;
bx_list_c *root_param = nullptr;
BX_MEM_C bx_mem;
bx_devices_c bx_devices;
bx_pc_system_c bx_pc_system;
bx_virt_timer_c bx_virt_timer;
bx_keymap_c bx_keymap;
bool bx_user_quit = false;
void bx_exit(int) { throw std::runtime_error("Bochs called bx_exit"); }

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
    throw std::runtime_error("Bochs VGA panic: " + message);
}
void logfunctions::fatal1(const char *fmt, ...) {
    va_list ap; va_start(ap, fmt); std::string message = format(fmt, ap); va_end(ap);
    throw std::runtime_error("Bochs VGA fatal: " + message);
}

BX_MEMORY_STUB_C::BX_MEMORY_STUB_C() : len(0), allocated(0), block_size(0), actual_vector(nullptr),
    vector(nullptr), blocks(nullptr), rom(nullptr), bogus(nullptr), used_blocks(0) {}
BX_MEMORY_STUB_C::~BX_MEMORY_STUB_C() {}
BX_MEM_C::BX_MEM_C() {}
BX_MEM_C::~BX_MEM_C() {}
bool BX_MEM_C::registerMemoryHandlers(void *, memory_handler_t, memory_handler_t, memory_direct_access_handler_t,
                                      bx_phy_address, bx_phy_address) { return true; }
void BX_MEM_C::load_ROM(const char *, bx_phy_address, Bit8u) {}

bx_pc_system_c::bx_pc_system_c() {}
int bx_pc_system_c::Reset(unsigned) { return 0; }

bx_devices_c::bx_devices_c() {
    init_stubs();
    pluginCmosDevice = &reference_cmos;
    mem = &bx_mem;
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
void bx_devices_c::paste_bytes(Bit8u *, Bit32s) {}
void bx_devices_c::gen_scancode(Bit32u) {}

bx_keymap_c::bx_keymap_c() {}
bx_keymap_c::~bx_keymap_c() {}
bool bx_keymap_c::isKeymapLoaded() { return false; }

// The bench owns time; Bochs's timers never fire.
static Bit64u reference_usec = 0;
bx_virt_timer_c::bx_virt_timer_c() {}
Bit64u bx_virt_timer_c::time_usec(bool) { return reference_usec; }
int bx_virt_timer_c::register_timer(void *, bx_timer_handler_t, Bit32u, bool, bool, bool, const char *) {
    static int next = 0;
    return next++;
}
void bx_virt_timer_c::activate_timer(unsigned, Bit32u, bool) {}
void bx_virt_timer_c::deactivate_timer(unsigned) {}

namespace {

// An 8-bit index frame of up to 800 x 600.
class ReferenceGui : public bx_gui_c {
public:
    ReferenceGui() {
        new_text_api = true;
        new_gfx_api = false;
        x_tilesize = X_TILESIZE;
        y_tilesize = Y_TILESIZE;
        max_xres = 800;
        max_yres = 600;
        snapshot_mode = false;
        charmap_updated = false;
        cursor_address = 0;
        std::memset(&tm_info, 0, sizeof tm_info);
        indices.assign(800 * 600, 0);
    }
    std::vector<Bit8u> indices;
    unsigned width() const { return guest_xres; }
    unsigned height() const { return guest_yres; }

    void specific_init(int, char **, unsigned) override {}
    void text_update(Bit8u *, Bit8u *, unsigned long, unsigned long, bx_vga_tminfo_t *) override {}
    void graphics_tile_update(Bit8u *tile, unsigned x, unsigned y) override {
        for (unsigned row = 0; row < Y_TILESIZE && y + row < guest_yres; ++row)
            for (unsigned column = 0; column < X_TILESIZE && x + column < guest_xres; ++column)
                indices[(y + row) * guest_xres + x + column] = tile[row * X_TILESIZE + column];
    }
    void handle_events() override {}
    void flush() override {}
    void clear_screen() override {}
    bool palette_change(Bit8u, Bit8u, Bit8u, Bit8u) override { return false; }
    void dimension_update(unsigned x, unsigned y, unsigned fheight, unsigned fwidth, unsigned bpp) override {
        if (x > 800 || y > 600) throw std::runtime_error("Bochs VGA mode larger than 800 x 600");
        guest_textmode = fheight > 0;
        guest_xres = Bit16u(x);
        guest_yres = Bit16u(y);
        guest_bpp = Bit8u(bpp);
        if (fheight) { guest_fheight = Bit8u(fheight); guest_fwidth = Bit8u(fwidth); }
    }
    unsigned create_bitmap(const unsigned char *, unsigned, unsigned) override { return 0; }
    unsigned headerbar_bitmap(unsigned, unsigned, void (*)(void)) override { return 0; }
    void replace_bitmap(unsigned, unsigned) override {}
    void show_headerbar() override {}
    int get_clipboard_text(Bit8u **, Bit32s *) override { return 0; }
    int set_clipboard_text(char *, Bit32u) override { return 0; }
    void mouse_enabled_changed_specific(bool) override {}
    void exit() override {}
    // Bochs's common character renderer, drawing into the index frame.
    void draw_char(Bit8u ch, Bit8u fc, Bit8u bc, Bit16u xc, Bit16u yc, Bit8u fw, Bit8u fh, Bit8u fx, Bit8u fy,
                   bool gfxcharw9, Bit8u cs, Bit8u ce, bool curs, bool font2) override {
        if (xc + fw > guest_xres || yc + fh > guest_yres) return;
        snapshot_buffer = indices.data();
        draw_char_common(ch, fc, bc, xc, yc, fw, fh, fx, fy, gfxcharw9, cs, ce, curs, font2);
        snapshot_buffer = nullptr;
    }
};

class Reference final : public bx_vgacore_c, public pc386::VgaReference {
public:
    Reference() {
        gui_instance = new ReferenceGui;
        bx_gui = gui_instance;
        // What bx_vgacore_c::init does, without the ROM load and GUI start.
        init_standard_vga();
        s.memsize = 0x40000;
        s.memory = new Bit8u[s.memsize];
        std::memset(s.memory, 0, s.memsize);
        s.vgamem_mask = 0x3ffff;
        s.num_x_tiles = Bit16u(s.max_xres / X_TILESIZE + ((s.max_xres % X_TILESIZE) > 0));
        s.num_y_tiles = Bit16u(s.max_yres / Y_TILESIZE + ((s.max_yres % Y_TILESIZE) > 0));
        s.vga_tile_updated = new bool[s.num_x_tiles * s.num_y_tiles]();
        update_mode_vsync = true;
        vsync_realtime = false;
    }
    ~Reference() override { bx_gui = nullptr; delete gui_instance; }

    void out(uint16_t port, uint8_t value) override { write(port, value, 1, true); }
    uint8_t in(uint16_t port) override { return uint8_t(read(port, 1)); }
    void mem_write(uint32_t address, uint8_t value) override { bx_vgacore_c::mem_write(address, value); }
    uint8_t mem_read(uint32_t address) override { return bx_vgacore_c::mem_read(address); }
    void latch_start() override { s.CRTC.start_addr = Bit16u((s.CRTC.reg[0x0c] << 8) | s.CRTC.reg[0x0d]); }

    bool render(pc386::Frame &frame) override {
        // Force a whole redraw: every tile, the character map and the text.
        s.vga_mem_updated |= 0x07;
        for (unsigned n = 0; n < unsigned(s.num_x_tiles) * s.num_y_tiles; ++n) s.vga_tile_updated[n] = true;
        s.text_buffer_update = true;
        std::memset(s.text_snapshot, 0, 0x20000);
        s.blink_counter = BLINK_FRAMES;
        // The blink counter in bx_vgacore_c::update() is a function static;
        // this follows it, starting where it starts (1, not visible).
        if (--blink_counter == 0) {
            blink_counter = BLINK_FRAMES;
            visible = (!s.graphics_ctrl.graphics_alpha || s.attribute_ctrl.mode_ctrl.blink_intensity) && !visible;
        }
        std::fill(gui_instance->indices.begin(), gui_instance->indices.end(), Bit8u(0));
        bool skipped = skip_update();
        update();
        if (skipped) return false;
        frame.width = gui_instance->width();
        frame.height = gui_instance->height();
        frame.pixels.resize(size_t(frame.width) * frame.height);
        for (size_t n = 0; n < frame.pixels.size(); ++n) {
            const auto &entry = s.pel.data[gui_instance->indices[n]];
            auto widen = [](Bit8u v) { v &= 0x3f; return uint32_t(v << 2 | v >> 4); };
            frame.pixels[n] = widen(entry.red) << 16 | widen(entry.green) << 8 | widen(entry.blue);
        }
        return true;
    }
    bool text_mode() const override { return !s.graphics_ctrl.graphics_alpha; }
    bool unprogrammed() const override { return s.CRTC.reg[1] == 0 || (s.CRTC.reg[9] & 0x1f) == 0; }
    bool blink_enabled() const override { return s.attribute_ctrl.mode_ctrl.blink_intensity; }
    bool blink_visible() const override { return visible; }
    const uint8_t *vram() const override { return s.memory; }

private:
    static constexpr unsigned BLINK_FRAMES = 8;
    ReferenceGui *gui_instance;
    unsigned blink_counter = 1;
    bool visible = false;
};

}  // namespace

std::unique_ptr<pc386::VgaReference> pc386::make_vga_reference() { return std::make_unique<Reference>(); }
