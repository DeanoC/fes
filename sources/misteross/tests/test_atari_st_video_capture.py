"""Exhaustively compare real ST capture RTL with the original address algorithm."""
# SPDX-License-Identifier: GPL-2.0-or-later

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
VERILATOR = shutil.which(os.environ.get("VERILATOR", "verilator"))

# Hierarchical observation and public simulation state are confined to this
# temporary testbench. Production ports, registers and latency do not change.
TOP = r"""
module st_video_capture_test_top (
    input wire clk, reset,
    input wire [1:0] resolution,
    output wire [1:0] capture_enable, capture_commit,
    output wire [3:0] capture_stage,
    output wire [35:0] mem_addr,
    output wire [1:0] fetch_valid,
    output wire [17:0] fetch_row, raster_row, raster_next_row,
    output wire [13:0] fetch_column,
    output wire [63:0] video_request
);
    st_video #(.CACHED_WORD_PORT(1'b1)) cached (
        .clk(clk), .reset(reset), .hold(1'b0), .screen_base(24'd0),
        .resolution(resolution), .palette(144'd0), .mem_data(16'd0),
        .mem_addr(mem_addr[17:0]), .fetch_valid(fetch_valid[0]),
        .fetch_row(fetch_row[8:0]), .fetch_column(fetch_column[6:0]),
        .raster_row(raster_row[8:0]), .raster_next_row(raster_next_row[8:0]),
        .video_request(video_request[31:0])
    );
    st_video physical (
        .clk(clk), .reset(reset), .hold(1'b0), .screen_base(24'd0),
        .resolution(resolution), .palette(144'd0), .mem_data(16'd0),
        .mem_addr(mem_addr[35:18]), .fetch_valid(fetch_valid[1]),
        .fetch_row(fetch_row[17:9]), .fetch_column(fetch_column[13:7]),
        .raster_row(raster_row[17:9]), .raster_next_row(raster_next_row[17:9]),
        .video_request(video_request[63:32])
    );
    assign capture_enable = {physical.capture_enable, cached.capture_enable};
    assign capture_commit = {physical.capture_commit, cached.capture_commit};
    assign capture_stage = {physical.capture_stage, cached.capture_stage};
endmodule
"""

DRIVER = r"""
#include "Vst_video_capture_test_top.h"
#include "Vst_video_capture_test_top___024root.h"
#include "verilated.h"
#include <cstdint>
#include <cstdlib>
#include <iostream>

int main(int argc, char **argv) {
    Verilated::commandArgs(argc, argv);
    Vst_video_capture_test_top dut;
    dut.clk = 0;
    dut.reset = 0;
    dut.resolution = 0;
    dut.eval();
    std::uint64_t states = 0, active = 0;
    for (unsigned mode = 0; mode < 4; ++mode) {
        dut.resolution = mode;
        const unsigned planes = mode == 0 ? 4 : mode == 2 ? 1 : 2;
        const unsigned top = mode == 2 ? 160 : 60;
        const unsigned height = mode == 2 ? 400 : 600;
        for (unsigned v = 0; v < 750; ++v) {
            for (unsigned h = 0; h < 1650; ++h) {
                // Enumerate the actual elaborated combinational RTL at every
                // legal raster state, without clocking or replacing its logic.
                dut.rootp->st_video_capture_test_top__DOT__cached__DOT__horizontal = h;
                dut.rootp->st_video_capture_test_top__DOT__cached__DOT__vertical = v;
                dut.rootp->st_video_capture_test_top__DOT__physical__DOT__horizontal = h;
                dut.rootp->st_video_capture_test_top__DOT__physical__DOT__vertical = v;
                dut.eval();
                // Independent original word-address oracle, including next
                // line/frame wrap. It contains no optimized phase windows.
                const unsigned ahead = h + planes;
                const bool wrap = ahead >= 1650;
                const unsigned x = wrap ? ahead - 1650 : ahead;
                const unsigned y = wrap ? (v + 1) % 750 : v;
                const unsigned phase = x % (mode == 0 ? 64 : 32);
                const bool enable = mode != 3 && x < 1280 &&
                    y >= top && y < top + height && phase < planes;
                for (unsigned implementation = 0; implementation < 2; ++implementation) {
                    const bool observed = (dut.capture_enable >> implementation) & 1;
                    if (observed != enable || (enable &&
                        ((((dut.capture_stage >> (2 * implementation)) & 3) != (phase & 3)) ||
                         (((dut.capture_commit >> implementation) & 1) != (phase == planes - 1))))) {
                        std::cerr << "capture mismatch: mode=" << mode << " h=" << h
                                  << " v=" << v << " implementation=" << implementation << '\n';
                        return EXIT_FAILURE;
                    }
                }
                ++states;
                active += enable;
            }
        }
    }
    dut.final();
    std::cout << "ST video capture equivalence PASS: " << states
              << " states, " << active << " active capture states; cached and physical RTL\n";
    return EXIT_SUCCESS;
}
"""


class AtariSTVideoCaptureTests(unittest.TestCase):
    @unittest.skipUnless(VERILATOR, "Verilator is required for exhaustive real-RTL capture validation")
    def test_all_raster_states_and_modes_match_original_word_address_oracle(self):
        with tempfile.TemporaryDirectory(prefix="fes-st-capture-") as name:
            root = Path(name)
            top = root / "st_video_capture_test_top.sv"
            driver = root / "capture_tb.cpp"
            build = root / "build"
            top.write_text(TOP)
            driver.write_text(DRIVER)
            command = [VERILATOR, "--cc", "--exe", "--build", "-O2", "-j", "2", "-Wall",
                       "--public-flat-rw", "--top-module", "st_video_capture_test_top",
                       "-I" + str(ROOT / "cores/fes-common/generated"), "--Mdir", str(build),
                       str(top), str(ROOT / "cores/fes-atari-st/rtl/st_video.sv"), str(driver),
                       "-CFLAGS", "-O2 -std=c++17"]
            compiled = subprocess.run(command, cwd=ROOT, text=True, capture_output=True, timeout=120)
            self.assertEqual(compiled.returncode, 0, compiled.stdout + compiled.stderr)
            result = subprocess.run([str(build / "Vst_video_capture_test_top")], text=True,
                                    capture_output=True, timeout=120)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
            self.assertEqual(result.stdout.strip(),
                             "ST video capture equivalence PASS: 4950000 states, "
                             "112000 active capture states; cached and physical RTL")


if __name__ == "__main__":
    unittest.main()
