"""Coleco frozen CPU-edge source and netlist checks."""

import json
import tempfile
import unittest
from pathlib import Path

from scripts import coleco_expansion


class ColecoExpansionTest(unittest.TestCase):
    def test_socket_bels_and_qsf_are_fixed(self):
        self.assertEqual(coleco_expansion.SOCKET_RECT, '24 1 28 11')
        bels = coleco_expansion.socket_bels()
        self.assertEqual(len(bels), 42)
        self.assertEqual(bels['plug_addr_ff_0'], 'MISTRAL_FF.24.1.2')
        self.assertEqual(bels['plug_addr_ff_23'], 'MISTRAL_FF.24.3.10')
        self.assertEqual(bels['plug_rdata_ff_10'], 'MISTRAL_FF.28.11.2')
        qsf = coleco_expansion.shell_qsf('set_global_assignment -name TOP_LEVEL_ENTITY top\n')
        self.assertEqual(qsf.count('FES_RESERVED_RECT'), 1)
        self.assertIn(coleco_expansion.SOCKET_RECT, qsf)
        with self.assertRaises(ValueError):
            coleco_expansion.shell_qsf(qsf)

    def test_rtl_bels_match_build_contract(self):
        rtl = (Path(__file__).parents[1] /
               'cores/fes-coleco/rtl/coleco_expansion_socket.v').read_text()
        for name, bel in coleco_expansion.socket_bels().items():
            rtl_name = name.replace('plug_addr_ff_', 'plug_request_ff_').replace(
                'plug_rdata_ff_', 'plug_response_ff_')
            self.assertIn(f'`COLECO_SOCKET_FF({rtl_name}, "{bel}",', rtl)

    def test_netlist_rejects_missing_or_wrong_boundary(self):
        bels = coleco_expansion.socket_bels()
        design = {'modules': {'top': {'netnames': {
            'system_clock.pll_outclk': {'bits': [902]},
            'plug_request': {'bits': list(range(31))},
        }, 'cells': {}}}}
        cells = design['modules']['top']['cells']
        cells['system_clock.clocks_MISTRAL_CLKBUF_Q'] = {
            'type': 'MISTRAL_CLKBUF',
            'connections': {'A': [902], 'Q': [900]},
        }
        for name, bel in bels.items():
            index = int(name.rsplit('_', 1)[1])
            output = [index] if name.startswith('plug_addr_') else [index + 100]
            cells[coleco_expansion.raw_cell_name(name)] = {
                'type': 'MISTRAL_FF', 'attributes': {'BEL': bel},
                'connections': {'CLK': [900], 'Q': output},
            }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'synth.json'
            path.write_text(json.dumps(design))
            coleco_expansion.prepare_shell_netlist(path)
            changed = json.loads(path.read_text())['modules']['top']['cells']
            self.assertIn('plug_addr_ff_0', changed)
            self.assertNotIn('socket.plug_request_ff_0', changed)
            for mutation in ('missing', 'bel', 'clock', 'width', 'source'):
                broken = json.loads(json.dumps(design))
                cell = broken['modules']['top']['cells']['socket.plug_request_ff_0']
                if mutation == 'missing':
                    del broken['modules']['top']['cells']['socket.plug_request_ff_0']
                elif mutation == 'bel':
                    cell['attributes']['BEL'] = 'MISTRAL_FF.1.1.1'
                elif mutation == 'clock':
                    cell['connections']['CLK'] = [901]
                elif mutation == 'width':
                    broken['modules']['top']['netnames']['plug_request']['bits'].pop()
                else:
                    broken['modules']['top']['cells'][
                        'system_clock.clocks_MISTRAL_CLKBUF_Q']['connections']['A'] = [901]
                path.write_text(json.dumps(broken))
                with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                    coleco_expansion.prepare_shell_netlist(path)

    def test_routed_shell_rejects_any_other_cell_in_reserved_rectangle(self):
        cells = {name: {'type': 'MISTRAL_FF',
                        'attributes': {'NEXTPNR_BEL': bel}}
                 for name, bel in coleco_expansion.socket_bels().items()}
        design = {'modules': {'top': {'cells': cells}}}
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / 'routed.json'
            path.write_text(json.dumps(design))
            coleco_expansion.validate_routed_shell(path)
            cells['foreign'] = {'type': 'MISTRAL_ALUT4',
                                'attributes': {'NEXTPNR_BEL': 'MISTRAL_MCOMB.25.3.0'}}
            path.write_text(json.dumps(design))
            with self.assertRaisesRegex(ValueError, 'foreign'):
                coleco_expansion.validate_routed_shell(path)


if __name__ == '__main__':
    unittest.main()
