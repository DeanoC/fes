"""Focused fail-closed controls for the real Quartus proof recipe."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("de25_link_build", Path(__file__).with_name("build.py"))
build = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(build)


class RecipeTests(unittest.TestCase):
    def test_real_preparation_declares_both_personas_reconfigurable(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            (output / "golden_top.qsf").write_text(
                'set_global_assignment -name DEVICE A5EB013BB23BE4SCS\n'
                'set_global_assignment -name TOP_LEVEL_ENTITY golden_top\n'
                'set_global_assignment -name SDC_FILE ghrd_timing.sdc\n')
            build.write_revisions(output, {"placement": "X2 Y7 X26 Y12", "routing": "X1 Y6 X27 Y13"}, "1" * 32)
            for revision in ("base", "persona_a", "persona_b"):
                qsf = (output/f"{revision}.qsf").read_text()
                self.assertIn("PARTIAL_RECONFIGURATION_PARTITION ON -to slot_inst", qsf)
                self.assertIn("ENABLE_PR_POF_ID ON", qsf)
                self.assertIn("NUM_PARALLEL_PROCESSORS 2", qsf)
                self.assertIn(f"DEVICE {build.DEVICE}", qsf)
                self.assertIn('SDC_FILE ghrd_timing.sdc', qsf)
                self.assertIn('NOT_GATE_PUSH_BACK OFF -to "soc_inst|subsys_periph|fes_pr_control|fes_pr_control|slot_reset"', qsf)
                self.assertIn('VERILOG_MACRO "FES_DE25_PR_BUILD_ID=128\'h' + '1' * 32 + '"', qsf)
                if revision != "base":
                    self.assertIn("root_partition.qdb -to |", qsf)
            self.assertNotIn("VIRTUAL_PIN", (output / "base.qsf").read_text())

    def test_only_exact_duplicate_emif_clock_is_removed(self):
        duplicate = 'create_clock -name EMIF_REF_CLOCK -period "166.6666MHz" [get_ports LPDDR4A_REFCLK_p]'
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)
            path = output / "ghrd_timing.sdc"
            source = '# retained vendor timing\n' + duplicate + '\nsource ./jtag.sdc\n'
            path.write_text(source)
            receipt = build.remove_duplicate_emif_clock(output)
            self.assertEqual(receipt["owner"], build.EMIF_REFERENCE_CLOCK)
            self.assertEqual(receipt["period_ns"], "6.000")
            self.assertEqual(path.read_text(), source.replace(duplicate,
                '# Generated EMIF SDC owns the 6.000 ns LPDDR4A reference clock.'))
            for changed in ('', source + duplicate + '\n', source.replace('166.6666MHz', '150MHz'),
                            source.replace('LPDDR4A_REFCLK_p', 'OTHER_REFCLK_p')):
                path.write_text(changed)
                with self.assertRaises(build.BuildError):
                    build.remove_duplicate_emif_clock(output)

    def test_tool_qpf_metadata_may_change_but_revision_selection_may_not(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            qpf = root/f"{build.PROJECT}.qpf"
            valid = '# vendor header\nQUARTUS_VERSION = "26.3"\nDATE = "new date"\n' + ''.join(
                f'PROJECT_REVISION = "{r}"\n' for r in ("base", "persona_a", "persona_b"))
            qpf.write_text(valid)
            build.verify_qpf(root)
            qpf.write_text(valid.replace('PROJECT_REVISION = "base"\nPROJECT_REVISION = "persona_a"',
                                        'PROJECT_REVISION = "persona_a"\nPROJECT_REVISION = "base"'))
            build.verify_qpf(root)
            qpf.write_text(valid+'PROJECT_REVISION = "other"\n')
            with self.assertRaises(build.BuildError):
                build.verify_qpf(root)

    def test_region_command_injection_rejected_before_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp)/"project"
            with self.assertRaises(build.BuildError):
                build.prepare(output, 'X2 Y7 X26 Y12"\nputs BAD', "X1 Y6 X27 Y13")
            self.assertFalse(output.exists())

    def test_captured_input_mutation_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root/"rtl.v").write_bytes(b"first")
            expected = {"rtl.v": build.sha(b"first")}
            build.verify_files(root, expected)
            (root/"rtl.v").write_bytes(b"second")
            with self.assertRaises(build.BuildError):
                build.verify_files(root, expected)

    def test_fitted_region_checks_import_label_and_exact_coordinates(self):
        region = {"id": "slot0", "instance": "slot_inst", "placement": "X2 Y7 X26 Y12", "routing": "X1 Y6 X27 Y13"}
        row = {"Name": "slot_inst", "Place Region Constraint": "(2, 7) to (26, 12)",
               "Route Region Constraint": "(1, 6) to (27, 13)"}
        build.verify_regions([row], region, "persona_a")
        build.verify_regions([row], region, "base")
        with self.assertRaises(build.BuildError):
            build.verify_regions([{**row, "Name": "other_instance"}], region, "base")
        with self.assertRaises(build.BuildError):
            build.verify_regions([{**row, "Place Region Constraint": "(3, 7) to (26, 12)"}], region, "persona_a")

    def test_vendor_decoded_pof_device_and_uniqueness(self):
        with tempfile.TemporaryDirectory() as tmp:
            log = Path(tmp)/"info.log"
            text = f"Device name: {build.DEVICE}\nPOF0 region ID: 0x01000000\nPOF0 hash: {'A'*64}\n"
            log.write_text(text)
            self.assertEqual(build.vendor_pof(log)["hash"], "a"*64)
            for broken in (text.replace(build.DEVICE, "A5E_OTHER"), text+"POF0 hash: "+"B"*64+"\n", text.replace("POF0 region ID", "POF1 region ID")):
                log.write_text(broken)
                with self.assertRaises(build.BuildError):
                    build.vendor_pof(log)

    def test_negative_timing_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp); (out/"output_files").mkdir()
            (out/"output_files/base.sta.summary").write_text("Type  : Setup 'board50'\nSlack : -0.001\nTNS   : -0.001\nCorner: Slow fix4 0C Model\n")
            (out/"output_files/base.sta.rpt").write_text("")
            with self.assertRaises(build.BuildError):
                build.timing_summary(out, "base")

    def test_hps_archive_is_required_before_output_creation(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / "project"
            with self.assertRaises(build.BuildError):
                build.prepare(output, "X2 Y7 X26 Y12", "X1 Y6 X27 Y13")
            self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
