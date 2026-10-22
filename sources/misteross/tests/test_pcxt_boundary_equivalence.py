"""Prove the slot-local reset flop preserves the complete boundary pipeline."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
REFERENCE = r"""// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_pcxt_isa.vh"

// One fabric register in each direction, matching the existing linkable-slot
// model. This portable boundary has no physical BEL/CRAM layout yet.
module pcxt_isa_boundary (
    input wire clk,
    input wire reset,
    input wire [`FES_PCXT_ISA_REQUEST_BITS-1:0] request,
    output reg [`FES_PCXT_ISA_REQUEST_BITS-1:0] card_request,
    input wire [`FES_PCXT_ISA_RESPONSE_BITS-1:0] card_response,
    output reg [`FES_PCXT_ISA_RESPONSE_BITS-1:0] response
);
    always @(posedge clk) begin
        if (reset) begin
            card_request <= 0;
            card_request[`FES_PCXT_ISA_RESET_BIT] <= 1;
            response <= 0;
        end else begin
            card_request <= request;
            response <= card_response;
        end
    end
endmodule
"""

@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class BoundaryEquivalenceTest(unittest.TestCase):
    def test_all_request_response_and_reset_sequences(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'gold.sv').write_text(REFERENCE)
            (work / 'gate.sv').write_text((ROOT / 'cores/fes-pcxt/rtl/pcxt_isa_boundary.sv').read_text())
            read = 'read_verilog -sv -I ' + str(ROOT / 'cores/fes-common/generated') + ' '
            command = (read + 'gold.sv; proc; opt; rename pcxt_isa_boundary gold; design -stash gold; ' +
                       read + 'gate.sv; proc; opt; rename pcxt_isa_boundary gate; '
                       'design -copy-from gold -as gold gold; equiv_make gold gate proof; '
                       'hierarchy -top proof; equiv_simple; equiv_induct -seq 2; equiv_status -assert')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=30)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
