import re
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
EXPERIMENT = ROOT / "experiments" / "020_linux_mailbox"
RTL = EXPERIMENT / "rtl" / "top.v"
MODEL = EXPERIMENT / "sim" / "hps_gp_model.v"
TB = EXPERIMENT / "sim" / "tb.cpp"
EXPECTED = EXPERIMENT / "expected.md"
MAKEFILE = ROOT / "Makefile"


class MailboxSourcePolicyTests(unittest.TestCase):
    def test_mailbox_sources_exist(self):
        for path in (RTL, MODEL, TB, EXPECTED):
            self.assertTrue(path.is_file(), path)

    def test_production_top_has_exact_clock_only_port_list(self):
        self.assertTrue(RTL.is_file(), RTL)
        source = RTL.read_text(encoding="utf-8")
        match = re.search(
            r"(?ms)^\s*module\s+top\b(?:\s*#\s*\([^;]*\))?\s*\((.*?)\)\s*;",
            source,
        )
        self.assertIsNotNone(match, "top module declaration is missing")
        self.assertEqual(
            re.sub(r"\s+", " ", match.group(1).strip()),
            "input wire FPGA_CLK1_50",
        )

    def test_production_top_contains_exactly_one_hps_gp_primitive(self):
        self.assertTrue(RTL.is_file(), RTL)
        source = RTL.read_text(encoding="utf-8")
        self.assertEqual(
            len(re.findall(r"\bcyclonev_hps_interface_mpu_general_purpose\b", source)),
            1,
        )

    def test_production_top_contains_protocol_constants_and_exact_rom(self):
        self.assertTrue(RTL.is_file(), RTL)
        source = RTL.read_text(encoding="utf-8")
        self.assertRegex(source, r"localparam\s+\[31:0\]\s+HELLO\s*=\s*32'hD3100000")
        self.assertRegex(source, r"localparam\s+\[31:0\]\s+DONE\s*=\s*32'hD3130C00")
        self.assertRegex(
            source,
            r"localparam\s+\[95:0\]\s+MESSAGE\s*=\s*"
            r"\{8'h4f,8'h53,8'h53,8'h20,8'h46,8'h50,8'h47,8'h41,"
            r"8'h20,8'h4f,8'h4b,8'h0a\}",
        )
        self.assertRegex(source, r"32'hAC100000")

    def test_production_top_has_no_unrequested_resources_or_external_outputs(self):
        self.assertTrue(RTL.is_file(), RTL)
        source = RTL.read_text(encoding="utf-8")
        for forbidden in (
            "LED",
            "HDMI",
            "SDRAM",
            "PLL",
            "BRAM",
            "LUTRAM",
            "DSP",
            "external GPIO",
        ):
            self.assertNotRegex(
                source,
                re.compile(rf"\b{re.escape(forbidden)}\b", re.IGNORECASE),
                forbidden,
            )

    def test_simulation_model_exposes_gpo_and_gpi_and_is_not_a_production_input(self):
        self.assertTrue(MODEL.is_file(), MODEL)
        model = MODEL.read_text(encoding="utf-8")
        self.assertRegex(
            model,
            r"module\s+cyclonev_hps_interface_mpu_general_purpose\b",
        )
        self.assertRegex(model, r"\b(?:input\s+wire\s+)?\[31:0\]\s+gp_in\b")
        self.assertRegex(model, r"\b(?:output\s+wire\s+)?\[31:0\]\s+gp_out\b")
        self.assertRegex(model, r"\bgpo\b.*verilator\s+public_flat_rw", re.IGNORECASE | re.DOTALL)
        self.assertRegex(model, r"\bgpi\b.*verilator\s+public_flat_rd", re.IGNORECASE | re.DOTALL)

        for production in (
            ROOT / "scripts" / "build_oss.sh",
            ROOT / "scripts" / "build_oracle.sh",
        ):
            self.assertNotIn("hps_gp_model.v", production.read_text(encoding="utf-8"))

    def test_expected_document_binds_protocol_payload_and_terminal_hold(self):
        self.assertTrue(EXPECTED.is_file(), EXPECTED)
        document = EXPECTED.read_text(encoding="utf-8")
        for expected in (
            "OSS FPGA OK\\n",
            "D3100000",
            "AC100000",
            "D311",
            "D312",
            "D3130C00",
            "255",
            "0",
            "reconfiguration",
            "hold",
        ):
            self.assertIn(expected, document)

    def test_simulation_target_allows_only_known_experiments(self):
        makefile = MAKEFILE.read_text(encoding="utf-8")
        require_exp = makefile[
            makefile.index("define require_exp") : makefile.index("endef", makefile.index("define require_exp"))
        ]
        self.assertNotIn("$(EXP)", require_exp)
        self.assertIn('"$$EXP"', require_exp)
        sim = makefile[makefile.index("sim:") : makefile.index("oss:")]
        self.assertIn("scripts/run_sim.sh --experiment", sim)
        self.assertNotIn("$(EXP)", sim)

        for experiment in (
            "010_blinky",
            "020_linux_mailbox",
            "030_m10k_rom",
            "040_mlab_ram",
            "050_lut_mul",
            "060_dsp_mul",
            "070_mixed_mem",
            "080_dsp_mem",
            "090_pll_clock",
            "100_dsp_rom",
            "110_pll_reset",
            "120_pll_dsp",
            "130_pll_dsp_40",
            "140_pll_dsp_20",
            "150_pll_dsp_80",
            "160_pll_dsp_100",
            "170_pll_dual",
            "180_pll_frac",
            "190_pll_frac_441",
            "200_pll_frac_dual",
            "210_pll_duty",
            "220_pll_phase",
            "230_pll_phase_180",
            "240_pll_phase_270",
            "250_pll_triple",
            "260_pll_quad",
            "270_pll_multi_duty",
            "280_pll_quadrature",
            "290_pll_phase_select",
            "300_pll_ref25",
            "310_pll_ref100",
            "320_pll_phase50",
            "330_pll_phase100",
            "340_pll_phase45",
            "350_pll_two",
            "360_pll_clkena",
            "370_pll_clkena_low",
            "380_pll_clkena_branch",
            "390_pll_clkena_status",
            "400_pll_clkena_reg2",
            "410_dsp_triple",
        ):
            result = subprocess.run(
                ["make", "-n", "sim", f"EXP={experiment}"],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("run_sim.sh", result.stdout)

    def test_simulation_selector_rejects_shell_hostile_experiment_without_execution(self):
        with tempfile.TemporaryDirectory(prefix="mailbox-selector-", dir="/dev/shm") as directory:
            marker = Path(directory) / "selector-was-executed"
            hostile = f"020_linux_mailbox' ; touch {marker} ; echo '"
            result = subprocess.run(
                ["make", "sim", f"EXP={hostile}"],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(marker.exists(), result.stderr)

            # A selector that passes require_exp must still be rejected by the
            # simulation dispatch case without executing arbitrary shell text.
            unknown = subprocess.run(
                ["make", "sim", "EXP=999_hostile"],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(unknown.returncode, 0)
            self.assertIn("unknown simulation experiment", unknown.stderr)

    def test_simulation_sources_do_not_enter_oss_or_oracle_commands(self):
        for script_name in ("build_oss.sh", "build_oracle.sh"):
            source = (ROOT / "scripts" / script_name).read_text(encoding="utf-8")
            self.assertNotIn("/sim/", source)
            self.assertNotIn("hps_gp_model.v", source)


if __name__ == "__main__":
    unittest.main()
