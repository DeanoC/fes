"""Prove cached commit eligibility preserves the complete ISA endpoint protocol."""
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

// Card-side prepare/commit adapter. Decode and prepare have no side effects.
// The device performs read/write side effects only on device_commit.
module pcxt_isa_endpoint (
    input wire clk,
    input wire reset,
    input wire [`FES_PCXT_ISA_REQUEST_BITS-1:0] request,
    output reg [`FES_PCXT_ISA_RESPONSE_BITS-1:0] response,
    output reg device_valid,
    output reg [`FES_PCXT_ISA_REQUEST_BITS-1:0] device_request,
    input wire device_claim,
    input wire device_ready,
    input wire [7:0] device_rdata,
    output wire device_commit,
    input wire [7:0] irq,
    input wire [2:0] drq
);
    reg seen, tag, claimed, prepared, committed;
    reg [7:0] data;
    wire valid = request[`FES_PCXT_ISA_VALID_BIT];
    wire same_tag = request[`FES_PCXT_ISA_TAG_BIT] == tag;
    wire clear = reset || request[`FES_PCXT_ISA_RESET_BIT] || !valid;
    assign device_commit = !clear && device_valid && seen && same_tag &&
                           claimed && prepared && !committed &&
                           request[`FES_PCXT_ISA_COMMIT_BIT];

    always @* begin
        response = 0;
        response[`FES_PCXT_ISA_PRESENT_BIT] = 1'b1;
        if (!reset && !request[`FES_PCXT_ISA_RESET_BIT]) begin
            response[`FES_PCXT_ISA_IRQ_LOW +: 8] = irq;
            response[`FES_PCXT_ISA_DRQ_LOW +: 3] = drq;
            response[`FES_PCXT_ISA_READ_DATA_LOW +: 8] = data;
            response[`FES_PCXT_ISA_SEEN_BIT] = seen;
            response[`FES_PCXT_ISA_RESPONSE_TAG_BIT] = tag;
            response[`FES_PCXT_ISA_CLAIM_BIT] = claimed;
            response[`FES_PCXT_ISA_READY_BIT] = prepared;
            response[`FES_PCXT_ISA_DONE_BIT] = committed;
        end
    end

    always @(posedge clk) begin
        if (clear) begin
            device_valid <= 0;
            device_request <= 0;
            seen <= 0;
            tag <= 0;
            claimed <= 0;
            prepared <= 0;
            committed <= 0;
            data <= 0;
        end else if (!device_valid) begin
            device_valid <= 1;
            device_request <= request;
            tag <= request[`FES_PCXT_ISA_TAG_BIT];
        end else if (!seen) begin
            // The device decodes the latched request in one fabric cycle.
            seen <= 1;
            claimed <= device_claim;
        end else if (claimed && !prepared && device_ready) begin
            prepared <= 1;
            data <= device_rdata;
        end else if (device_commit) begin
            committed <= 1;
        end
    end
endmodule
"""

@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class EndpointEquivalenceTest(unittest.TestCase):
    def prove(self, candidate):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'gold.sv').write_text(REFERENCE)
            (work / 'gate.sv').write_text(candidate)
            read = 'read_verilog -sv -I ' + str(ROOT / 'cores/fes-common/generated') + ' '
            command = (read + 'gold.sv; proc; opt; rename -hide w:*; rename pcxt_isa_endpoint gold; design -stash gold; ' +
                       read + 'gate.sv; proc; opt; rename -hide w:*; rename pcxt_isa_endpoint gate; '
                       'design -copy-from gold -as gold gold; miter -equiv -flatten gold gate proof; '
                       'hierarchy -top proof; opt_clean; sat -seq 8 -tempinduct -maxsteps 20 '
                       '-set-def-inputs -set-at 1 in_reset 1 -set-init-zero -prove trigger 0 -verify')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=30)
            return result

    def test_all_streams_after_reset(self):
        # No cuts at matching state/output names: eligibility is a reachable-state
        # invariant. Reset clears every state bit in both implementations; zero
        # initialization only defines the otherwise unspecified pre-reset outputs.
        candidate = (ROOT / 'cores/fes-pcxt/rtl/pcxt_isa_endpoint.sv').read_text()
        result = self.prove(candidate)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertIn('Induction step proven', result.stdout)

    def test_rejects_repeated_commit(self):
        candidate = (ROOT / 'cores/fes-pcxt/rtl/pcxt_isa_endpoint.sv').read_text()
        broken = candidate.replace('committed <= 1;\n            commit_ready <= 0;',
                                   'committed <= 1;')
        self.assertNotEqual(broken, candidate)
        result = self.prove(broken)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('proof did fail', result.stdout + result.stderr)
