// SPDX-License-Identifier: MIT
// Exact BIU regression: a DMA read is pending as a store enters the commit
// register. The read must wait until that store reaches DDR.
#include "Vfes_i386_biu.h"
#include "verilated.h"

#include <cstdint>
#include <cstdio>
#include <vector>

namespace {

constexpr uint32_t DDR_BASE_WORD = 0x03400000;
constexpr uint32_t RAM_BYTES = 1u << 24;

uint32_t byte_offset(uint32_t word_address, unsigned beat = 0) {
    return (word_address - DDR_BASE_WORD) * 16 + beat * 16;
}

void fill_beat(Vfes_i386_biu &top, const std::vector<uint8_t> &memory, uint32_t address) {
    const uint32_t offset = byte_offset(address);
    for (unsigned word = 0; word < 4; ++word) {
        uint32_t value = 0;
        for (unsigned lane = 0; lane < 4; ++lane)
            value |= uint32_t(memory[offset + 4 * word + lane]) << (8 * lane);
        top.ddr_readdata[word] = value;
    }
}

int run() {
    VerilatedContext context;
    Vfes_i386_biu top(&context);
    std::vector<uint8_t> memory(RAM_BYTES, 0);
    bool read_pending = false;
    bool read_before_store = false;
    bool wrote_store = false;
    uint32_t read_address = 0;
    uint32_t write_address = 0;
    unsigned write_beat = 0;

    top.clk = 0;
    top.cold_reset = 1;
    top.reset = 0;
    top.attr_upper = 0;
    top.ram_top = 16;
    top.st_valid = 0;
    top.st_line0 = 0x3000 >> 5;
    top.st_line1 = top.st_line0 + 1;
    top.st_attr0 = 0;  // ATTR_RAM
    top.st_attr1 = 0;
    top.st_dword = 0;
    top.st_data = 0;
    top.st_mask = 0;
    top.drain = 0;
    top.d_req = 0;
    top.d_mmio = 0;
    top.d_addr = 0;
    top.d_be = 0;
    top.i_req = 0;
    top.i_line = 0;
    top.ddr_waitrequest = 0;
    top.ddr_readdata[0] = top.ddr_readdata[1] = 0;
    top.ddr_readdata[2] = top.ddr_readdata[3] = 0;
    top.ddr_readdatavalid = 0;
    top.mmio_ready = 1;
    top.mmio_rvalid = 0;
    top.mmio_rdata = 0;
    top.dma_valid = 0;
    top.dma_write = 0;
    top.dma_address = 0x3000;
    top.dma_wdata = 0;
    top.flush_request = 0;

    auto cycle = [&]() {
        top.clk = 0;
        top.ddr_waitrequest = 0;
        top.ddr_readdatavalid = read_pending;
        if (read_pending) fill_beat(top, memory, read_address);
        top.eval();

        const bool accept_read = top.ddr_read && !top.ddr_waitrequest;
        const bool accept_write = top.ddr_write && !top.ddr_waitrequest;
        const uint32_t command_address = top.ddr_address;
        if (accept_read) read_before_store |= !wrote_store;
        if (accept_write) {
            if (write_beat == 0) write_address = command_address;
            const unsigned beat = write_beat++;
            const uint32_t offset = byte_offset(write_address, beat);
            for (unsigned lane = 0; lane < 16; ++lane) {
                if ((top.ddr_byteenable >> lane) & 1)
                    memory[offset + lane] = uint8_t(top.ddr_writedata[lane / 4] >> (8 * (lane % 4)));
            }
            if (write_beat == top.ddr_burstcount) {
                write_beat = 0;
                wrote_store = true;
            }
        }

        top.clk = 1;
        top.eval();
        if (read_pending) read_pending = false;
        if (accept_read) {
            read_address = command_address;
            read_pending = true;
        }
        return bool(top.dma_done);
    };

    for (unsigned n = 0; n < 3; ++n) cycle();
    top.cold_reset = 0;

    // First latch a DMA read to 3000h. On the next edge, present the byte
    // store to 3000h: dma_busy and s_part are both active while c_part is
    // still empty. Without the current-store hold, the BIU accepts the read
    // here and returns the old zero byte.
    top.dma_valid = 1;
    cycle();
    top.st_valid = 1;
    top.st_data = 0xa5;
    top.st_mask = 1;
    cycle();
    top.st_valid = 0;

    bool done = false;
    for (unsigned n = 0; n < 128 && !done; ++n) done = cycle();
    const uint8_t result = uint8_t(top.dma_rdata);
    top.final();
    if (!done || result != 0xa5 || read_before_store || memory[0x3000] != 0xa5) {
        std::fprintf(stderr, "FAIL BIU DMA/store: done=%d data=%02x early=%d wrote=%d memory=%02x\n", int(done),
                     result, int(read_before_store), int(wrote_store), memory[0x3000]);
        return 1;
    }
    std::puts("PASS BIU DMA waits for committing store");
    return 0;
}

}  // namespace

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    return run();
}
