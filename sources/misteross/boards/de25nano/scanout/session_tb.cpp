// SPDX-License-Identifier: GPL-2.0-or-later
#include "Vfes_de25_session_display.h"
#include "generated/fes_de25_scanout.h"
#include "verilated.h"
#include <array>
#include <cstdint>
#include <deque>
#include <iostream>
#include <stdexcept>

static void require(bool condition, const char* message) {
    if (!condition) throw std::runtime_error(message);
}
class Session {
public:
    Vfes_de25_session_display dut;
    std::deque<std::uint32_t> pending;
    std::array<std::uint32_t, 2> palette{{0, 0x80004000u}};
    std::uint32_t base = 0x26000000u;
    bool responses = true, check_color = false;
    unsigned visible_slot = 1, h = 0, v = 720;
    std::uint64_t pixels = 0, bursts = 0, checked_pixels = 0;
    std::deque<std::uint32_t> history{0, 0};
    Session() {
        dut.pixel_clk = 0; dut.clk_sys = 0; dut.reset_n = 0; dut.exec_reset = 0;
        dut.clock_fault = 0; dut.display_ready = 1;
        dut.request = 0; dut.opcode = 0; dut.index = 0; dut.argument = 0;
        dut.address = 0; dut.read = 0; dut.write = 0; dut.byteenable = 15; dut.writedata = 0;
        dut.avm_waitrequest = 0; dut.avm_readdatavalid = 0; dut.avm_response = 0;
        for (unsigned lane = 0; lane < 4; ++lane) dut.avm_readdata[lane] = 0;
        idle(4); dut.reset_n = 1; idle(2 * 1650 * 750);
        require(dut.locked && bursts == 0, "cold raster lock without DMA");
    }
    void tick() {
        dut.pixel_clk = 0;
        dut.machine = 0x08000000u | (v >= 725 && v < 730 ? 0x04000000u : 0) |
            (h >= 1390 && h < 1430 ? 0x02000000u : 0) |
            (h < 1280 && v < 720 ? 0x01000000u : 0) | 0x204060u;
        dut.avm_readdatavalid = 0;
        dut.eval();
        const bool reply = responses && !pending.empty();
        std::uint32_t value = 0;
        if (reply) {
            value = palette.at((pending.front() - base) / FES_DE25_SCANOUT_SLOT_BYTES);
            pending.pop_front();
        }
        if (dut.avm_read && !dut.avm_waitrequest) {
            require(dut.avm_address >= base &&
                std::uint64_t(dut.avm_address) + 16u * dut.avm_burstcount <=
                std::uint64_t(base) + FES_DE25_SCANOUT_ALLOCATION_BYTES, "outside allocated memory");
            require(dut.avm_burstcount && dut.avm_burstcount <= 16, "burst bound");
            require((dut.avm_address & 4095u) + 16u * dut.avm_burstcount <= 4096, "4KiB bound");
            require(pending.size() + dut.avm_burstcount <= 256, "response credit bound");
            for (unsigned beat = 0; beat < dut.avm_burstcount; ++beat)
                pending.push_back(dut.avm_address + 16u * beat);
            ++bursts;
        }
        for (unsigned lane = 0; lane < 4; ++lane) dut.avm_readdata[lane] = value;
        dut.avm_readdatavalid = reply;
        // Four system edges per three pixel edges, independent of GP requests.
        for (unsigned edge = 0; edge < (pixels % 3 == 0 ? 2u : 1u); ++edge) {
            dut.clk_sys = 0; dut.eval(); dut.clk_sys = 1; dut.eval();
        }
        dut.pixel_clk = 1; dut.eval();
        const auto expected = history.front(); history.pop_front(); history.push_back(dut.machine);
        require((dut.video & 0x0f000000u) == (expected & 0x0f000000u), "machine sync pipeline changed");
        if (check_color && (dut.video & 0x01000000u)) {
            const auto rgba = palette[visible_slot];
            const unsigned alpha = rgba >> 24, weight = 256 - alpha - (alpha >> 7);
            std::uint32_t blended = 0;
            for (unsigned shift : {0u, 8u, 16u}) {
                unsigned channel = ((rgba >> shift) & 255) + ((((0x204060u >> shift) & 255) * weight) >> 8);
                blended |= (channel > 255 ? 255 : channel) << shift;
            }
            require((dut.video & 0xffffffu) == blended, "premultiplied pixel mismatch");
            ++checked_pixels;
        }
        if (++h == 1650) { h = 0; if (++v == 750) v = 0; }
        ++pixels;
    }
    void idle(unsigned count) { while (count--) tick(); }
    void write(std::uint32_t address, std::uint32_t value) {
        dut.address = address; dut.writedata = value; dut.write = 1; tick();
        require(dut.writeresponsevalid && !dut.response, "CSR write completion");
        dut.write = 0; tick();
    }
    std::uint32_t read(std::uint32_t address) {
        dut.address = address; dut.read = 1; tick();
        require(dut.readdatavalid && !dut.response, "CSR read completion");
        const auto value = dut.readdata; dut.read = 0; tick(); return value;
    }
    std::uint32_t snapshot(std::uint32_t address) {
        write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_SNAPSHOT); return read(address);
    }
    void gp_start(unsigned op, unsigned index, unsigned argument) {
        dut.opcode = op; dut.index = index; dut.argument = argument; dut.request = 1;
    }
    std::uint16_t gp_finish(unsigned rejection = 0, bool query = false) {
        unsigned remaining = 3 * 1650 * 750;
        while (!dut.response_valid && remaining--) tick();
        require(dut.response_valid && dut.response_error == (rejection != 0) &&
            (query || dut.response_data == rejection), "GP response mismatch or deadline");
        const auto data = dut.response_data;
        dut.request = 0;
        remaining = 100;
        while (dut.response_valid && remaining--) tick();
        require(!dut.response_valid, "GP response did not retire");
        return data;
    }
    void gp(unsigned op, unsigned index, unsigned argument, unsigned rejection = 0) {
        gp_start(op, index, argument); gp_finish(rejection);
    }
    std::uint16_t info(unsigned index) {
        gp_start(0x12, index, 0); return gp_finish(0, true);
    }
    void publish(unsigned generation) {
        write(FES_DE25_SCANOUT_GRANT_LO_OFFSET, base);
        write(FES_DE25_SCANOUT_GRANT_HI_OFFSET, 0);
        write(FES_DE25_SCANOUT_GRANT_SPAN_OFFSET, FES_DE25_SCANOUT_ALLOCATION_BYTES);
        write(FES_DE25_SCANOUT_GENERATION_LO_OFFSET, generation);
        write(FES_DE25_SCANOUT_GENERATION_HI_OFFSET, 0);
        write(FES_DE25_SCANOUT_SEQUENCE_OFFSET, 1);
        write(FES_DE25_SCANOUT_SLOT_OFFSET, 1);
        write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_SUBMIT);
        require(info(9) & 4, "GP pending must reflect kernel publication");
        for (unsigned frame = 0; frame < 5; ++frame) {
            idle(1650 * 750);
            require(snapshot(FES_DE25_SCANOUT_SNAPSHOT_ERRORS_OFFSET) == 0, "scanout fault");
            if (snapshot(FES_DE25_SCANOUT_SNAPSHOT_DISPLAYED_SEQUENCE_OFFSET) == 1 && dut.ui_active) {
                require(!(info(9) & 4) && info(10) == 1 && info(11) == 0,
                    "GP completion must reflect the actual kernel scanout");
                return;
            }
        }
        throw std::runtime_error("complete frame/visibility deadline");
    }
    void close() {
        check_color = false;
        responses = false; idle(1000);
        write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_STOP);
        require(!(snapshot(FES_DE25_SCANOUT_SNAPSHOT_STATUS_OFFSET) & FES_DE25_SCANOUT_STATUS_DRAINED),
            "premature kernel drain with held responses");
        gp_start(0x14, 0, 0); idle(100);
        require(!dut.response_valid && !dut.quiesced_sys, "premature GP close before memory drain");
        responses = true; gp_finish(); idle(10);
        require(dut.quiesced_sys && !dut.ui_active && pending.empty(), "close did not drain/hide");
        const auto old_bursts = bursts; idle(1000);
        require(bursts == old_bursts && !dut.avm_read, "reads after retirement");
        write(FES_DE25_SCANOUT_COMMAND_OFFSET, FES_DE25_SCANOUT_COMMAND_CLEAR);
        require(snapshot(FES_DE25_SCANOUT_SNAPSHOT_DISPLAYED_SEQUENCE_OFFSET) == 0, "CLEAR sequence");
        require(!(info(9) & 4) && info(10) == 0 && info(11) == 0, "GP post-CLEAR observation");
    }
};

int main() {
    try {
        Session s;
        s.gp(0x13, 0, 1); s.gp(0x14, 0, 1);
        for (unsigned index = 0; index < 3; ++index) s.gp(0x15, index, 0, 1);
        require(s.bursts == 0, "GP submission bypassed kernel ownership");
        s.publish(1); s.idle(1650 * 750); s.check_color = true; s.idle(1650 * 750);
        require(s.checked_pixels >= 1280 * 720, "no complete blended frame");
        s.close();
        s.base = 0x36000000u; s.palette[1] = 0xff802010u;
        s.gp(0x14, 0, 1); s.publish(2); s.idle(1650 * 750);
        s.check_color = true; s.idle(1650 * 750); s.close();
        require(s.checked_pixels >= 2 * 1280 * 720 && !s.dut.faulted, "reallocated opaque frame");
        s.base = 0x46000000u; s.palette[1] = 0;
        s.gp(0x14, 0, 1); s.publish(3); s.idle(1650 * 750);
        s.check_color = true; s.idle(1650 * 750); s.close();
        require(s.checked_pixels >= 3 * 1280 * 720 && !s.dut.faulted, "transparent frame did not preserve machine");
        std::cout << "PASS allocated DE25 session: shared blend/sync, kernel-only publication, bounded relocated reads, held-response drain and allocation/sequence reopen\n";
        return 0;
    } catch (const std::exception& error) {
        std::cerr << "DE25 session: " << error.what() << '\n'; return 1;
    }
}
