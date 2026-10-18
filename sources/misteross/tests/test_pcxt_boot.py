"""Exercise every boot-ROM byte with delayed acceptance and completion."""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
VERILATOR = shutil.which('verilator')


@unittest.skipUnless(VERILATOR, 'requires Verilator')
class BootLoaderTest(unittest.TestCase):
    def test_registered_rom_full_load_backpressure_and_cold_restart(self):
        fixture = r'''
module tb;
    reg clk = 0;
    always #5 clk = ~clk;
    reg cold_reset = 1, enable = 0, ready = 0, done = 0;
    wire valid;
    wire [15:0] address;
    wire [7:0] data;
    pcxt_boot dut(.*);
    task tick;
        @(posedge clk); #1;
    endtask
    function [7:0] expected(input integer index);
        expected = 8'(index ^ (index >> 8));
    endfunction
    task load(input integer limit);
        for (integer i = 0; i < limit; i++) begin
            enable = (i % 7 != 0);
            repeat (2) tick();
            enable = 1;
            while (!valid) tick();
            // No stale read may be offered, including on the first valid edge.
            if (address != 16'(i) || data != expected(i)) $fatal(1, "offer %d", i);
            repeat (i % 5) begin
                tick();
                if (!valid || address != 16'(i) || data != expected(i))
                    $fatal(1, "unstable offer %d", i);
            end
            ready = 1; tick(); ready = 0;
            if (valid) $fatal(1, "accepted offer remains valid");
            repeat (1 + i % 3) tick();
            done = 1; tick(); done = 0;
        end
    endtask
    initial begin
        tick(); cold_reset = 0;
        // Interrupt an actual partial load, then visit every byte after reset.
        load(137);
        cold_reset = 1; tick(); cold_reset = 0;
        load(65536);
        repeat (8) begin
            tick();
            if (valid || address != 16'hffff) $fatal(1, "completed load restarts");
        end
        $finish;
    end
endmodule
'''
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'pcxt_diagnostic.hex').write_text(''.join(
                f'{(i ^ (i >> 8)) & 255:02x}\n' for i in range(65536)))
            (work / 'tb.sv').write_text(fixture)
            result = subprocess.run([
                VERILATOR, '--binary', '--timing', '--top-module', 'tb',
                '--Mdir', str(work / 'obj'), '-Wno-TIMESCALEMOD',
                str(ROOT / 'cores/fes-pcxt/rtl/pcxt_boot.sv'), str(work / 'tb.sv')],
                cwd=work, capture_output=True, text=True, timeout=90)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            result = subprocess.run([str(work / 'obj/Vtb')], cwd=work,
                                    capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
