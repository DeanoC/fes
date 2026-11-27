#include "Vfes_de25_managed_ram_scan.h"
#include "verilated.h"
#include <cstdint>
#include <iostream>
#include <stdexcept>

static void check(bool ok, const char *why) { if (!ok) throw std::runtime_error(why); }
struct Rig {
    Vfes_de25_managed_ram_scan d;
    uint16_t mem[16]{};
    bool pending_write = false, pending_read = false, hold_response = false;
    bool corrupt = false;
    unsigned writes = 0, reads = 0;
    unsigned read_addr = 0, read_left = 0;
    void cycle() {
        d.clk = 0; d.waitrequest = 0; d.readdatavalid = 0; d.writeresponsevalid = 0;
        if (!hold_response && pending_write) { d.writeresponsevalid = 1; pending_write = false; }
        if (!hold_response && pending_read) { d.readdatavalid = 1; d.readdata = mem[read_addr++] ^ (corrupt ? 1 : 0); pending_read = --read_left != 0; }
        d.response = 0; d.eval();
        if (d.write) {
            check(!pending_write && !pending_read && d.burstcount == 1, "unexpected write");
            auto &word = mem[d.address / 2];
            if (d.byteenable & 1) word = (word & 0xff00) | (d.writedata & 0xff);
            if (d.byteenable & 2) word = (word & 0x00ff) | (d.writedata & 0xff00);
            pending_write = true; ++writes;
        }
        if (d.read) {
            check(!pending_write && !pending_read && d.burstcount > 0, "unexpected read");
            read_addr = d.address / 2; read_left = d.burstcount; pending_read = true; ++reads;
        }
        d.clk = 1; d.eval();
    }
    Rig() { d.reset_n = 0; d.run_permitted = 0; d.hold_requested = 0; d.calibrated = 1; cycle(); d.reset_n = 1; cycle(); cycle(); }
    void launch() { d.run_permitted = 1; cycle(); cycle(); }
    void finish() { for (int i = 0; i < 5000 && d.result == 1; ++i) cycle(); }
};
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    try {
        { Rig r; check(r.d.result == 0 && r.d.drained, "startup not held"); r.launch(); r.finish();
          check(r.d.result == 2 && r.d.drained && r.writes && r.reads, "good scan did not pass");
          unsigned writes = r.writes; r.d.run_permitted = 0; r.cycle(); r.cycle();
          check(r.d.result == 0, "held scan retains PASS");
          r.d.run_permitted = 1; r.cycle();
          check(r.d.result != 2, "new run reused stale PASS");
          r.cycle(); r.finish();
          check(r.d.result == 2 && r.writes > writes, "new release did not rerun scan"); }
        { Rig r; r.corrupt = true; r.launch(); r.finish(); check(r.d.result == 3, "corruption passed"); }
        { Rig r; r.launch(); while (!r.pending_write) r.cycle(); r.hold_response = true; r.d.hold_requested = 1;
          for (int i = 0; i < 40; ++i) r.cycle(); check(!r.d.drained && r.d.result != 2, "unanswered write counted drained");
          r.hold_response = false; for (int i = 0; i < 100 && !r.d.drained; ++i) r.cycle();
          check(r.d.drained && r.d.result != 2, "stopped scan passed or failed to drain"); }
        { Rig r; r.d.calibrated = 0; r.launch(); for (int i = 0; i < 20; ++i) r.cycle();
          check(r.d.result == 0 && r.writes == 0, "uncalibrated start"); }
        std::cout << "PASS managed SDRAM scan: pass, corruption, held response drain and readiness\n";
    } catch (const std::exception &e) { std::cerr << "FAIL " << e.what() << '\n'; return 1; }
}
