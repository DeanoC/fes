"""Exercise every boot-ROM byte with delayed acceptance and completion."""
from pathlib import Path
import importlib.util
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
VERILATOR = shutil.which('verilator')
SPEC = importlib.util.spec_from_file_location('pcxt_sim_boot', ROOT / 'scripts/sim_fes_pcxt.py')
SIM = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SIM)


class BootImageTest(unittest.TestCase):
    def test_banks_reconstruct_exact_image(self):
        data = bytes(((i * 73) ^ (i >> 3) ^ (i >> 9)) & 255 for i in range(65536))
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            SIM.boot_roms(work, data)
            restored = b''.join(bytes.fromhex((work / f'pcxt_diagnostic.bank{bank}.hex').read_text())
                                for bank in range(8))
            self.assertEqual(restored, data)
            self.assertEqual(len(list(work.iterdir())), 8)
            for size in (0, 65535, 65537):
                with self.assertRaises(ValueError):
                    SIM.boot_roms(work, bytes(size))


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
    // Observe before nonblocking updates, just as the receiving port does.
    // This catches a stale first offer even when load() resumes after a later
    // edge, by which time a delayed ROM read could have corrected its output.
    always @(posedge clk)
        if (!cold_reset && valid && data !== expected(int'(address)))
            $fatal(1, "stale data at receiver edge: address=%h data=%h", address, data);
    task load(input integer limit);
        for (integer i = 0; i < limit; i++) begin
            enable = (i % 7 != 0);
            repeat (2) tick();
            enable = 1;
            while (!valid) tick();
            // Also check the offered address and stability between accepts.
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
            SIM.boot_roms(work, bytes((i ^ (i >> 8)) & 255 for i in range(65536)))
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
