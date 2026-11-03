"""Vendor-profile drift controls; synthetic project files are not FPGA evidence."""
import json
import hashlib
from pathlib import Path
import tempfile
import unittest
import xml.etree.ElementTree as ET

from boards.de25nano.scanout import vendor_reference as reference


class VendorReferenceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.project = Path(self.temporary.name)
        self.expected = json.loads((Path(__file__).parent /
            'fixtures/de25_vendor_hdmi_profile.json').read_text())
        (self.project / 'fes_combined').mkdir()
        (self.project / 'fes_mailbox').mkdir()
        (self.project / 'output_files').mkdir()
        assignments, fitted = [], []
        for name, pin in self.expected['pins'].items():
            assignments += [f'set_location_assignment PIN_{pin["pin"]} -to {name}',
                            f'set_instance_assignment -name IO_STANDARD "{pin["standard"]}" -to {name}']
            fitted.append(f'{name} : {pin["pin"]} : output : {pin["standard"]} : bank voltage : bank')
        (self.project / 'golden_top.qsf').write_text('\n'.join(assignments) + '\n')
        (self.project / 'output_files/golden_top.pin').write_text('\n'.join(fitted) + '\n')
        ns = 'http://www.accellera.org/XMLSchema/IPXACT/1685-2014'
        extension = 'http://www.altera.com/XMLSchema/IPXact2014/extensions'
        root = ET.Element('component')
        params = ET.SubElement(ET.SubElement(root, '{' + extension + '}altera_module_parameters'),
                               '{' + ns + '}parameters')
        for name, value in self.expected['pll_parameters'].items():
            param = ET.SubElement(params, '{' + ns + '}parameter', parameterId=name)
            ET.SubElement(param, '{' + ns + '}value').text = value
        (self.project / 'fes_combined/sys_pll.ip').write_bytes(ET.tostring(root))
        board = ('sys_pll video_clock (.refclk(CLOCK0_50), .outclk_0(pixel_clk));\n'
                 'assign HDMI_TX_CLK = pixel_clk;\n')
        for name in ('fes_de25_scanout_board.v', 'board.v'):
            (self.project / 'fes_mailbox' / name).write_text(board)
        lut = '\n'.join(f"{n}: LUT_DATA <= 16'h{value};" for n, value in
                        enumerate(self.expected['transmitter_lut']))
        (self.project / 'fes_mailbox/fes_de25_scanout_hdmi_config.v').write_text(lut)
        controller = self.project / 'fes_mailbox/fes_de25_scanout_i2c_controller.v'
        controller.write_text('synthetic controller fixture; never compiled\n')
        # Keep the public fixture metadata-only. Vendor source remains in the
        # resource ZIP; this unit fixture exercises drift checks independently.
        self.expected['derived_hdmi_sha256'] = {
            name: hashlib.sha256((self.project / 'fes_mailbox' / name).read_bytes()).hexdigest()
            for name in self.expected['derived_hdmi_sha256']}

    def validate(self, core='fes.menu'):
        return reference.validate_project(self.project, self.expected, core)

    def test_the_two_supported_cores_bind_the_complete_reference_profile(self):
        for core in ('fes.menu', 'fes.zx81'):
            result = self.validate(core)
            self.assertEqual(len(result['pins']), 37)
            self.assertEqual(len(result['transmitter_lut']), 33)
            self.assertFalse(result['independent_pcb_timing_qualified'])
            self.assertFalse(result['hardware_acceptance'])

    def test_source_and_actual_fitted_pin_drift_are_rejected_independently(self):
        for name, before, after in [
            ('golden_top.qsf', 'PIN_DJ24', 'PIN_DJ23'),
            ('golden_top.qsf', '"1.1-V" -to HDMI_TX_CLK', '"3.3-V LVCMOS" -to HDMI_TX_CLK'),
            ('output_files/golden_top.pin', 'DJ24', 'DJ23'),
            ('output_files/golden_top.pin', 'HDMI_TX_CLK', 'OTHER_CLOCK'),
        ]:
            path = self.project / name
            original = path.read_text()
            self.assertIn(before, original)
            try:
                path.write_text(original.replace(before, after))
                with self.assertRaises(reference.BuildError):
                    self.validate()
            finally:
                path.write_text(original)
        path = self.project / 'golden_top.qsf'
        path.write_text(path.read_text() + 'set_location_assignment PIN_DJ24 -to HDMI_TX_CLK\n')
        with self.assertRaises(ValueError):
            self.validate()

    def test_frequency_phase_duty_cycle_and_forwarding_are_not_approximated(self):
        path = self.project / 'fes_combined/sys_pll.ip'
        original = path.read_bytes()
        for parameter, value in [('gui_output_clock_frequency0', '65.0'),
                                 ('gui_phase_shift0', '180.0'), ('gui_duty_cycle0', '40.0')]:
            root = ET.fromstring(original)
            for element in root.iter():
                if element.get('parameterId') == parameter:
                    next(iter(element)).text = value
            path.write_bytes(ET.tostring(root))
            with self.assertRaises(reference.BuildError):
                self.validate()
        path.write_bytes(original)
        path = self.project / 'fes_mailbox/fes_de25_scanout_board.v'
        original = path.read_text()
        for before, after in [('= pixel_clk;', '= ~pixel_clk;'),
                              ('(CLOCK0_50)', '(CLOCK2_50)')]:
            path.write_text(original.replace(before, after))
            with self.assertRaises(reference.BuildError):
                self.validate()

    def test_every_transmitter_write_and_its_order_are_required(self):
        path = self.project / 'fes_mailbox/fes_de25_scanout_hdmi_config.v'
        original = path.read_text()
        for text in [original.replace("16'hba60", "16'hba20"),
                     '\n'.join(original.splitlines()[1:]),
                     original + '\n0: LUT_DATA <= 16\'h0000;']:
            self.assertNotEqual(text, original)
            path.write_text(text)
            with self.assertRaises(reference.BuildError):
                self.validate()

    def test_a_forged_or_partial_vendor_snapshot_is_not_a_reference(self):
        for members in ({}, {name: b'changed vendor source' for name in reference.MEMBERS}):
            with self.assertRaises(reference.BuildError):
                reference.profile(members)
        archive = self.project / 'wrong.zip'
        archive.write_bytes(b'not the locked vendor ZIP')
        with self.assertRaises(reference.BuildError):
            reference.collect(self.project, self.project / 'review', archive)


if __name__ == '__main__':
    unittest.main()
