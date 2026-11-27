#include "Vfes_de25_ramtest_hold_guard.h"
#include "verilated.h"
#include <iostream>
#include <stdexcept>
static void check(bool ok, const char *why) { if (!ok) throw std::runtime_error(why); }
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    try {
        Vfes_de25_ramtest_hold_guard d;
        auto cycle = [&] { d.clk = 0; d.eval(); d.clk = 1; d.eval(); };
        d.gpo = 0; d.mailbox_gpi = 0; d.drained = 1; cycle();
        d.gpo = 0x82000001; cycle(); check(d.mailbox_gpo == d.gpo, "release did not forward");
        d.mailbox_gpi = 0x00800000; cycle();
        d.drained = 0; d.gpo = 0x02000000; cycle();
        check(d.hold_requested && d.mailbox_gpo == 0x82000001, "undrained hold forwarded");
        for (int i = 0; i < 20; ++i) { cycle(); check(d.mailbox_gpo == 0x82000001, "held command changed"); }
        d.drained = 1; cycle(); check(d.mailbox_gpo == d.gpo, "drained hold not forwarded");
        d.mailbox_gpi = 0; cycle(); check(!d.hold_requested, "acknowledged hold remains pending");
        d.drained = 0; d.gpo = 0x82010000; cycle();
        check(!d.hold_requested && d.mailbox_gpo == d.gpo, "invalid hold hidden from mailbox");
        std::cout << "PASS Execution Hold waits for the accepted SDRAM traffic to drain\n";
    } catch (const std::exception &e) { std::cerr << "FAIL " << e.what() << '\n'; return 1; }
}
