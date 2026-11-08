"""A pending KF8259 INT that the CPU never acknowledged follows the mask (#1534).

GLaBIOS's warm reboot (Ctrl+Alt+Del) runs with interrupts disabled while the
timer keeps requesting IRQ0. Its POST then re-initialises the PIC, masks every
IRQ and executes STI, and halts with a 5-short/3-long beep code if any
interrupt arrives. An 8259 drives INT from the unmasked requests, so masking
them or ICW1 withdraws it. The imported KF8259 latched INT until an acknowledge,
so the warm POST took a stale interrupt and halted.
"""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
VENDOR = ROOT / 'cores/fes-common/vendor/kf8259'
VERILATOR = os.environ.get('VERILATOR') or shutil.which('verilator')
SOURCES = ('KF8259_Common_Package.svh', 'KF8259_Bus_Control_Logic.sv', 'KF8259_Interrupt_Request.sv',
           'KF8259_Priority_Resolver.sv', 'KF8259_In_Service.sv', 'KF8259_Control_Logic.sv', 'KF8259.sv')

BENCH = r'''
#include "VKF8259.h"
#include "verilated.h"
#include <cstdio>
#include <cstdlib>
static VKF8259 d;
static void tick() { d.clock = 0; d.eval(); d.clock = 1; d.eval(); }
static void check(bool ok, const char *what) { if (!ok) { std::printf("FAIL %s\n", what); std::exit(1); } }
static void write(unsigned address, unsigned value) {
    d.address = address; d.data_bus_in = value; d.chip_select_n = 0; d.write_enable_n = 0; tick();
    d.write_enable_n = 1; tick(); d.chip_select_n = 1; tick();
}
static void init() { write(0, 0x13); write(1, 0x08); write(1, 0x09); }
// Edge mode needs the request low, then held high until the acknowledge,
// as the PIT's square wave holds IRQ0.
static void request(bool high) { d.interrupt_request = high ? 1 : 0; for (int i = 0; i < 4; ++i) tick(); }
static unsigned acknowledge() {
    unsigned vector = 0;
    for (int pulse = 0; pulse < 2; ++pulse) {
        d.interrupt_acknowledge_n = 0; tick(); tick(); d.eval();
        vector = d.data_bus_out;
        d.interrupt_acknowledge_n = 1; tick(); tick();
    }
    return vector;
}
int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    d.chip_select_n = 1; d.read_enable_n = 1; d.write_enable_n = 1; d.address = 0; d.data_bus_in = 0;
    d.cascade_in = 0; d.slave_program_n = 1; d.interrupt_acknowledge_n = 1; d.interrupt_request = 0;
    d.reset = 1; tick(); tick(); d.reset = 0; tick();
    init(); write(1, 0x00);
    // An acknowledged request still delivers its vector and drops INT.
    request(true); check(d.interrupt_to_cpu, "unmasked IRQ0 raises INT");
    check(acknowledge() == 0x08, "IRQ0 vector"); check(!d.interrupt_to_cpu, "acknowledge drops INT");
    write(0, 0x20); request(false);
    // The CPU has interrupts disabled: IRQ0 stays pending. Masking it withdraws
    // INT, and unmasking the still-requesting IRQ0 raises INT again.
    request(true); check(d.interrupt_to_cpu, "pending IRQ0 raises INT");
    write(1, 0xff); check(!d.interrupt_to_cpu, "masking every IRQ withdraws INT");
    write(1, 0xfe); check(d.interrupt_to_cpu, "unmasking the pending request raises INT");
    // GLaBIOS warm POST: re-initialise and walk the IMR test while the timer
    // keeps toggling IRQ0, then mask all and STI. No interrupt may remain.
    init(); check(!d.interrupt_to_cpu, "ICW1 clears IRR and withdraws INT");
    for (unsigned bit = 0; bit < 8; ++bit) { request(false); write(1, 1u << bit); request(true); }
    write(1, 0xff); check(!d.interrupt_to_cpu, "all-masked PIC after the warm POST IMR test holds no INT");
    write(1, 0xfe); check(d.interrupt_to_cpu, "IRQ0 still requesting is delivered once unmasked");
    check(acknowledge() == 0x08, "IRQ0 vector after the warm POST");
    std::printf("PASS\n");
    return 0;
}
'''


@unittest.skipUnless(VERILATOR, 'requires Verilator; set VERILATOR')
class PicIntWithdrawTest(unittest.TestCase):
    def test_masked_or_reinitialised_request_withdraws_int(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'bench.cpp').write_text(BENCH)
            subprocess.run([VERILATOR, '--cc', '--exe', '--build', '-Wno-fatal', '-Wno-lint', '-Wno-style',
                            '--top-module', 'KF8259', '--Mdir', str(work / 'obj'), '-I' + str(VENDOR),
                            *(str(VENDOR / name) for name in SOURCES if name.endswith('.sv')),
                            str(work / 'bench.cpp')], check=True, capture_output=True, text=True)
            result = subprocess.run([str(work / 'obj/VKF8259')], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertIn('PASS', result.stdout)


if __name__ == '__main__':
    unittest.main()
