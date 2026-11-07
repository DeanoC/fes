"""Exercise the actual ZX81 source, pin, memory and PLL preparation gates."""
import importlib.util
import io
import json
from contextlib import redirect_stderr
from pathlib import Path
import tempfile
import unittest
from unittest import mock
import zipfile

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("de25_zx81_builder", HERE / "build.py")
B = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(B)


class ZX81BuildTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="fes-de25-zx81-test-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.archive = self.root / "vendor.zip"
        self.files = {name: b"vendor input\n" for name in B.VENDOR_FILES}
        self.files["HDMI_I2C/I2C_HDMI_Config.v"] = b'''parameter CLK_Freq = 50000000;
parameter I2C_Freq = 20000;
if( mI2C_CLK_DIV < (CLK_Freq/I2C_Freq) ) mI2C_CLK_DIV <= mI2C_CLK_DIV+1;
else begin mI2C_CLK_DIV <= 0; mI2C_CTRL_CLK <= ~mI2C_CTRL_CLK; end
'''
        def pins(ports):
            return "\n".join(f'set_location_assignment PIN_TEST{n} -to {port}\n'
                f'set_instance_assignment -name IO_STANDARD "3.3-V LVCMOS" -to {port}'
                for n, port in enumerate(sorted(ports))) + "\n"
        display_pins = pins(B.DISPLAY.PORTS)
        self.files["golden_top.qsf"] = display_pins.encode()
        self.baseline = ("set_global_assignment -name DEVICE " + B.DEVICE + "\n" + display_pins + pins(B.KEY_PORTS)).encode()
        params = {"system_info_device_component": "A5EB013BB23BE4SR1", "gui_reference_clock_frequency": "50.0",
            "gui_output_clock_frequency0": "74.25", "gui_output_clock_frequency_ps0": "13468.013",
            "gui_number_of_clocks": "1", "gui_use_locked": "true", "gui_phase_shift0": "0.0", "gui_duty_cycle0": "50.0"}
        def xml(parameters, extra=""):
            return ('<ipxact:component xmlns:ipxact="' + B.DISPLAY.NS["i"] + '" xmlns:altera="' + B.DISPLAY.NS["a"] + '">'
                '<altera:altera_module_parameters><ipxact:parameters>' + "".join(
                    f'<ipxact:parameter parameterId="{key}"><ipxact:value>{value}</ipxact:value></ipxact:parameter>'
                    for key, value in parameters.items()) + '</ipxact:parameters></altera:altera_module_parameters>'
                '<ipxact:value>A5EB013BB23BE4SR1</ipxact:value>' + extra + '</ipxact:component>').encode()
        self.files["sys_pll.ip"] = xml(params, '<ipxact:library>sys_pll</ipxact:library>' * 2 +
            '<ipxact:value>74250000</ipxact:value>&lt;value&gt;74250000&lt;/value&gt;')
        self.files["agilex_reset_release.ip"] = xml({"DEVICE_FAMILY": "Agilex 5"})

    def prepare(self, name="run"):
        with zipfile.ZipFile(self.archive, "w") as package:
            for name_, data in self.files.items():
                package.writestr("Demonstration/FPGA/HDMI_ASx4/" + name_, data)
            package.writestr(B.SCS_GOLDEN, self.baseline)
        sources = B.capture_sources()
        lock = sources["vendor.lock.toml"].decode()
        old_hash = B.tomllib.loads(lock)["archive"]["sha256"]
        sources["vendor.lock.toml"] = lock.replace(old_hash, B.sha(self.archive.read_bytes())).encode()
        with mock.patch.object(B, "capture_sources", return_value=sources):
            return B.prepare(self.archive, self.root / name)

    def test_real_machine_project_preserves_sources_and_records_derivations(self):
        output = self.prepare()
        project = output / "project"
        qsf = (project / (B.PROJECT + ".qsf")).read_text()
        self.assertIn("NUM_PARALLEL_PROCESSORS 2", qsf)
        self.assertIn("VERILOG_MACRO QUARTUS=1", qsf)
        self.assertNotIn("machine_pll", qsf)
        self.assertIn("SYSTEMVERILOG_FILE zx81_machine.sv", qsf)
        self.assertIn("VERILOG_FILE zx81_dpram_de25.v", qsf)
        self.assertNotIn("VERILOG_FILE zx81_dpram.v\n", qsf)
        self.assertIn("KEY[0]", (project / "pins.qsf").read_text())
        self.assertEqual((output / "vendor/sys_pll.ip").read_bytes(), self.files["sys_pll.ip"])
        self.assertEqual(B.pll_parameters((project / "sys_pll.ip").read_bytes())["gui_output_clock_frequency0"], "74.25")
        self.assertEqual((project / "zx81_dpram.v").read_bytes(), B.SOURCE_PATHS["zx81_dpram.v"].read_bytes())
        self.assertEqual((project / "I2C_HDMI_Config.v").read_bytes(), self.files["HDMI_I2C/I2C_HDMI_Config.v"])
        inputs = json.loads((output / "inputs.json").read_text())
        self.assertFalse(inputs["hps"] or inputs["ddr"] or inputs["gp"] or inputs["runtime_package"])
        self.assertEqual(inputs["source_classification"], "working-copy-diagnostic")
        self.assertEqual(inputs["system_clock_hz"], 74250000)
        self.assertEqual(inputs["machine_clock"], {"ula_hz": 6500000, "cpu_hz": 3250000, "phase_numerator": 26, "phase_denominator": 297})
        for key, value in inputs["prepared_project_sha256"].items():
            self.assertEqual(value, B.sha((project / key).read_bytes()))

    def test_pll_targets_are_guarded(self):
        original = self.files["sys_pll.ip"]
        for before, after in ((b">74.25<", b">75.0<"), (b">true<", b">false<")):
            with self.subTest(before=before), self.assertRaisesRegex(ValueError, "PLL.*contract changed"):
                B.DISPLAY.retarget_pll(original.replace(before, after))

    def test_ram_adaptation_is_limited_to_family_and_block_type(self):
        original = B.SOURCE_PATHS["zx81_dpram.v"].read_bytes()
        derived, record = B.retarget_ram(original)
        expected = original.replace(b'intended_device_family = "Cyclone V"', b'intended_device_family = "Agilex 5"')
        expected = expected.replace(b'"M10K"', b'"M20K"')
        self.assertEqual(derived, expected)
        self.assertEqual(record["derived_sha256"], B.sha(expected))
        with self.assertRaisesRegex(ValueError, "adaptation contract changed"):
            B.retarget_ram(original.replace(b'ram_block_type = "M10K"', b'ram_block_type = "AUTO"'))
        video = B.SOURCE_PATHS["zx81_video_720p.v"].read_bytes()
        self.assertEqual(B.retarget_video(video)[0], video.replace(b'"M10K"', b'"M20K"'))
        with self.assertRaisesRegex(ValueError, "adaptation contract changed"):
            B.retarget_video(video.replace(b'270 * 512', b'270 * 1024'))

    def test_rejects_missing_key_or_disagreeing_hdmi_pin(self):
        original = self.baseline
        self.baseline = original.replace(b"-to KEY[0]", b"-to KEY_MISSING")
        with self.assertRaisesRegex(ValueError, "key pin/IO assignment"):
            self.prepare()
        self.baseline = original.replace(b"PIN_TEST0", b"PIN_OTHER", 1)
        with self.assertRaisesRegex(ValueError, "pins differ"):
            self.prepare()

    def test_rejects_changed_i2c_clock_divider(self):
        self.files["HDMI_I2C/I2C_HDMI_Config.v"] = self.files["HDMI_I2C/I2C_HDMI_Config.v"].replace(b"20000;", b"100000;")
        with self.assertRaisesRegex(ValueError, "I2C clock divider"):
            self.prepare()

    def test_refuses_unlocked_archive_and_existing_output(self):
        output = self.prepare()
        with self.assertRaises(FileExistsError):
            self.prepare()
        with self.assertRaisesRegex(ValueError, "must match"):
            B.prepare(self.archive, self.root / "unlocked")
        self.assertTrue((output / "inputs.json").is_file())

    def test_dirty_build_stops_before_preparation(self):
        identity = {"base_revision": "a" * 40, "source_classification": "working-copy-diagnostic"}
        with mock.patch.object(B, "source_identity", return_value=identity), mock.patch.object(B, "prepare") as prepare, redirect_stderr(io.StringIO()):
            with self.assertRaises(SystemExit):
                B.main(["build", "--vendor-archive", str(self.archive)])
            prepare.assert_not_called()

    def test_generated_ip_parameter_changes_are_retained_and_rejected(self):
        output = self.prepare()
        def mutate(command, project, log):
            path = project / "sys_pll.ip"
            path.write_bytes(path.read_bytes().replace(b">74.25<", b">74.0<"))
        with mock.patch.object(B.DISPLAY, "run", side_effect=mutate), self.assertRaisesRegex(ValueError, "generation changed"):
            B.compile_project(output, self.root / "quartus_sh", "test")
        self.assertFalse((output / "build.json").exists())
        review = json.loads((output / "ip-parameter-review.json").read_text())
        self.assertEqual(review["sys_pll.ip"]["gui_output_clock_frequency0"], {"before": "74.25", "after": "74.0"})

    def test_generated_clock_guard_rejects_silent_rounding_in_metadata_or_atom(self):
        project = self.root / "generated"
        synth = project / "sys_pll/altera_iopll_2110/synth"
        synth.mkdir(parents=True)
        xml = project / "sys_pll/sys_pll.xml"
        xml.write_text('<deploy><perimeter>'
            '<interface name="refclk"><property name="clockRate" value="50000000" /></interface>'
            '<interface name="outclk0"><property name="clockRate" value="74250000" /></interface>'
            '</perimeter><entity>'
            '<parameter name="gui_actual_output_clock_frequency0" value="74.25" />'
            '<parameter name="gui_actual_phase_shift0" value="0.0" />'
            '<parameter name="gui_actual_duty_cycle0" value="50.0" />'
            '</entity></deploy>')
        hdl = synth / "sys_pll_atom.v"
        hdl.write_text(".fb_clk_m_div(297), .ref_clk_n_div(5), .out_clk_0_c_div(40), "
            ".fb_clk_fractional_div_num(1), .fb_clk_fractional_div_den(1), .fb_clk_fractional_div_value(1), "
            ".out_clk_0_freq(36'd74250000)")
        self.assertEqual(B.generated_clock_review(project)["output_clock_hz"], 74250000)
        original_xml = xml.read_text()
        xml.write_text(original_xml.replace('value="74.25"', 'value="74.249999"'))
        with self.assertRaisesRegex(ValueError, "metadata differs"):
            B.generated_clock_review(project)
        xml.write_text(original_xml)
        hdl.write_text(hdl.read_text().replace(".fb_clk_m_div(297)", ".fb_clk_m_div(296)"))
        with self.assertRaisesRegex(ValueError, "does not realize exact"):
            B.generated_clock_review(project)


if __name__ == "__main__":
    unittest.main()
