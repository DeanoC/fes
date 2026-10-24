"""Prove grant snapshots preserve DMA state and valid transaction payloads.

Inactive TC is deliberately unobserved: the actual motherboard latches it only
when dma_valid selects an accepted request. An active-service snapshot invariant
covers terminal-count use during COMPLETE as well as external transactions.
"""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
YOSYS = os.environ.get('YOSYS') or shutil.which('yosys')
REFERENCE_CAPTURE = """                        transfer_address <= {page[service_channel == 2 ? 1 : service_channel == 3 ? 2 : 3], current_address[service_channel]};
                        terminal <= current_count[service_channel] == 0;
"""


@unittest.skipUnless(YOSYS, 'requires Yosys')
class DmaSnapshotTest(unittest.TestCase):
    def prove(self, mutation=None):
        actual = (ROOT / 'cores/fes-pcxt/rtl/pcxt_dma.sv').read_text()
        start = actual.index('            // Snapshot on each grant')
        end = actual.index('            if (guest_ce && minimum_wait', start)
        original = actual[:start] + actual[end:]
        anchor = '                        if (service_channel == 0) refresh_pending<=refresh_edge;\n'
        self.assertEqual(original.count(anchor), 1)
        original = original.replace(anchor, anchor + REFERENCE_CAPTURE)
        if mutation == 'terminal':
            needle = 'terminal <= current_count[service_channel] == 0;'
            self.assertEqual(actual.count(needle), 1)
            actual = actual.replace(needle, 'terminal <= current_count[service_channel] != 0;')
        elif mutation == 'address':
            needle = 'current_address[service_channel]};'
            self.assertEqual(actual.count(needle), 1)
            actual = actual.replace(needle, "(current_address[service_channel] ^ 16'h0001)};")
        def instrument(source):
            source = source.replace('module pcxt_dma (',
                'module pcxt_dma (\n    output wire [20:0] proof_active_snapshot,')
            source = source.replace('assign cycle_tc = !memory_phase && terminal;',
                'assign cycle_tc = cycle_valid && !memory_phase && terminal;')
            position = source.index('    wire unused_command')
            return source[:position] + """    assign proof_active_snapshot = (state == DEV_SEND || state == DEV_WAIT ||
        state == MEM_SEND || state == MEM_WAIT || state == COMPLETE) ?
        {transfer_address, terminal} : 21'b0;
""" + source[position:]
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            (work / 'gold.sv').write_text(instrument(original))
            (work / 'gate.sv').write_text(instrument(actual))
            (work / 'blacklist.txt').write_text('terminal\ntransfer_address\n')
            command = ('read_verilog -sv gold.sv; proc; memory; opt; rename pcxt_dma gold; '
                       'design -stash gold; read_verilog -sv gate.sv; proc; memory; opt; '
                       'rename pcxt_dma gate; design -copy-from gold -as gold gold; '
                       'equiv_make -blacklist blacklist.txt gold gate equiv; '
                       'hierarchy -top equiv; equiv_simple; equiv_induct -seq 8; '
                       'equiv_status -assert')
            result = subprocess.run([YOSYS, '-Q', '-p', command], cwd=work,
                                    capture_output=True, text=True, timeout=90)
            output = result.stdout + result.stderr
            if mutation:
                self.assertNotEqual(result.returncode, 0, output)
                self.assertIn('unproven $equiv cells', output)
                self.assertIn('cycle_tc_gold' if mutation == 'terminal' else 'cycle_address_gold', output)
            else:
                self.assertEqual(result.returncode, 0, output)
                self.assertIn('Equivalence successfully proven!', output)

    def test_state_and_valid_transaction_equivalence(self):
        self.prove()

    def test_wrong_terminal_count_rejected(self):
        self.prove('terminal')

    def test_wrong_transfer_address_rejected(self):
        self.prove('address')
