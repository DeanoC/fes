"""Prove the PIC's parallel priority logic against its original equations."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
VENDOR = ROOT / 'cores/fes-common/vendor/kf8259'
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')


@unittest.skipUnless(YOSYS, 'requires Yosys; set YOSYS to the locked executable')
class PicPriorityTest(unittest.TestCase):
    def test_all_requests_masks_rotations_and_nested_modes(self):
        # All inputs, including the highest-service vector, are unconstrained.
        # The reference retains the imported rotate/priority/rotate equations;
        # no assumption about reachable PIC states is needed for this proof.
        fixture = '''
        `include "KF8259_Common_Package.svh"
        module proof(input [2:0] rotation,
            input [7:0] requests, service, highest, mask, special_mask,
            input fully_nested, output ok);
          wire [7:0] actual;
          KF8259_Priority_Resolver dut(rotation, mask, special_mask,
              fully_nested, highest, requests, service, actual);
          wire [7:0] rq = KF8259_Common_Package::rotate_right(requests & ~mask, rotation);
          wire [7:0] hs = KF8259_Common_Package::rotate_right(highest, rotation);
          wire [7:0] isr = KF8259_Common_Package::rotate_right(service & ~special_mask, rotation);
          wire [7:0] adjusted = fully_nested ? ((isr & ~hs) | {hs[6:0],1'b0}) : isr;
          reg [7:0] priority_mask;
          always @* begin
            if      (adjusted[0]) priority_mask = 8'h00;
            else if (adjusted[1]) priority_mask = 8'h01;
            else if (adjusted[2]) priority_mask = 8'h03;
            else if (adjusted[3]) priority_mask = 8'h07;
            else if (adjusted[4]) priority_mask = 8'h0f;
            else if (adjusted[5]) priority_mask = 8'h1f;
            else if (adjusted[6]) priority_mask = 8'h3f;
            else if (adjusted[7]) priority_mask = 8'h7f;
            else                 priority_mask = 8'hff;
          end
          wire [7:0] expected = KF8259_Common_Package::rotate_left(
              KF8259_Common_Package::resolv_priority(rq) & priority_mask, rotation);
          // The in-service register uses the same circular selection on its
          // arbitrary next-state vector. Its clock/reset boundary is unchanged.
          wire [7:0] selected = KF8259_Common_Package::resolve_rotated_priority(requests, rotation);
          wire [7:0] selected_ref = KF8259_Common_Package::rotate_left(
              KF8259_Common_Package::resolv_priority(
                  KF8259_Common_Package::rotate_right(requests, rotation)), rotation);
          assign ok = actual == expected && selected == selected_ref;
        endmodule
        '''
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'proof.sv').write_text(fixture)
            command = ('read_verilog -sv -I ' + str(VENDOR) + ' ' +
                       str(VENDOR / 'KF8259_Priority_Resolver.sv') +
                       ' proof.sv; prep -top proof -flatten; '
                       'sat -verify -prove ok 1 -show-inputs -show-outputs')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
