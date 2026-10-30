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
    def test_combined_client_proof_receipt_matches_captured_sources(self):
        combined = build.BOARD / "zx81-computer"
        build.PROFILE = build.load_profile(combined / "profile.py")
        try:
            frozen = build.capture()
        finally:
            build.PROFILE = None

        root = frozen["repository"]
        receipt_path = root / "docs/validation/2026-10-30-de25-zx81-load-roundtrip/combined-proof.json"
        receipt = json.loads(receipt_path.read_text())
        self.assertEqual(receipt["status"], "passed")
        self.assertTrue(receipt["production_source_committed"])
        self.assertEqual(receipt["compiler_source_sha256"],
                         {name: build.sha(data) for name, data in frozen["files"].items()})

        runtime = root / "sources/libmister-runtime"
        paths = list(combined.glob("*.cpp")) + list(combined.glob("*.hpp"))
        paths += [combined / "sim_top.v", combined / "proof.py"]
        paths += list((runtime / "src/native").rglob("*.hpp"))
        paths += [runtime / "include/libmister-runtime/runtime.h",
                  runtime / "src/native/de25_computer.cpp",
                  runtime / "src/native/de25_pr_control.cpp",
                  build.BOARD.parent / "zx81-hps/cosim/wire.hpp",
                  build.BOARD.parent / "zx81-hps/client_proof.py"]
        runtime_hashes = {path.relative_to(root).as_posix(): build.sha(path.read_bytes()) for path in paths}
        self.assertTrue(receipt["test_source_committed"])
        self.assertEqual(receipt["test_runtime_source_sha256"], runtime_hashes)
        self.assertEqual(len(receipt["results"]), 14)
        self.assertTrue(all(result["result"] == "passed" for result in receipt["results"]))

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

    def test_pfg_zero_exit_text_must_not_hide_errors(self):
        with tempfile.TemporaryDirectory() as tmp:
            log = Path(tmp)/"info.log"
            valid = f"Device name: {build.DEVICE}\nPOF0 region ID: 0x01000000\nPOF0 hash: {'A'*64}\n"
            missing = "Error\nError info: The SOF file is incomplete.\nError detail: HPS is present but bootloader information is missing\n"
            for failure in (missing, 'Error (19192): incomplete\n', 'Error: bad image\n',
                            'Info: Programming File Generator was unsuccessful.\n'):
                log.write_text(valid + failure)
                with self.assertRaises(build.BuildError):
                    build.vendor_pof(log)
            log.write_text(valid + missing)
            self.assertEqual(build.vendor_pof(log, allow_missing_bootloader=True)["hash"], 'a'*64)
            for unexpected in (valid, valid + missing + 'Error: another error\n'):
                log.write_text(unexpected)
                with self.assertRaises(build.BuildError):
                    build.vendor_pof(log, allow_missing_bootloader=True)
            log.write_text(valid + 'Error Detection CRC\n')
            build.vendor_pof(log)

    def test_pfg_integrity_requires_explicit_success(self):
        with tempfile.TemporaryDirectory() as tmp:
            log = Path(tmp)/"integrity.log"
            log.write_text('   Integrity status: OK\n')
            build.vendor_integrity(log)
            for text in ('', 'Integrity status: FAILED\n', 'Integrity status: OK\nError: bad image\n',
                         'Integrity status: OK\nIntegrity status: OK\n'):
                log.write_text(text)
                with self.assertRaises(build.BuildError):
                    build.vendor_integrity(log)

    def test_hps_packaging_binds_vendor_bootloader_and_recipe(self):
        with tempfile.TemporaryDirectory() as tmp:
            project = Path(tmp)/'project'; project.mkdir()
            boot = project/'software/u-boot/spl/u-boot-spl-dtb.hex'
            boot.parent.mkdir(parents=True); boot.write_bytes(b'captured bootloader')
            recipe = project/'sof_with_hps.bat'
            recipe.write_text(r'%QUARTUS_ROOTDIR%\bin64\quartus_pfg -c output_files\golden_top.sof output_files\golden_top_hps.sof -o hps_path=software\u-boot\spl\u-boot-spl-dtb.hex' + '\n\npause\n')
            hashes = {p.relative_to(project).as_posix():build.sha(p.read_bytes()) for p in (boot,recipe)}
            metadata = project.parent/'vendor-inputs.json'
            metadata.write_text(json.dumps({'source_sha256':hashes}))
            self.assertEqual(build.hps_boot_packaging(project)['bootloader']['sha256'], hashes[boot.relative_to(project).as_posix()])
            boot.write_bytes(b'different bootloader')
            with self.assertRaises(build.BuildError):
                build.hps_boot_packaging(project)
            boot.write_bytes(b'captured bootloader')
            recipe.write_text(recipe.read_text().replace('golden_top.sof','different.sof'))
            hashes['sof_with_hps.bat'] = build.sha(recipe.read_bytes())
            metadata.write_text(json.dumps({'source_sha256':hashes}))
            with self.assertRaises(build.BuildError):
                build.hps_boot_packaging(project)

    def test_negative_timing_is_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            out = Path(tmp); (out/"output_files").mkdir()
            (out/"output_files/base.sta.summary").write_text("Type  : Setup 'board50'\nSlack : -0.001\nTNS   : -0.001\nCorner: Slow fix4 0C Model\n")
            (out/"output_files/base.sta.rpt").write_text("")
            with self.assertRaises(build.BuildError):
                build.timing_summary(out, "base")

    def test_source_closure_seal_recomputes_final_inputs(self):
        inputs = {"project_sha256": {"base.qsf": "a" * 64},
                  "qpf": {"sha256": "b" * 64, "version": "26.3"}}
        build.reseal_source_closure(inputs)
        build.verify_source_closure(inputs)
        self.assertEqual(inputs["source_closure_sha256"], build.source_closure_sha256(inputs))
        inputs["qpf"]["sha256"] = "c" * 64
        with self.assertRaises(build.BuildError):
            build.verify_source_closure(inputs)

    def test_persona_partition_reuse_requires_full_root_preservation(self):
        rows = [{"Statistic": "Total Preserved Core Logic", "root_partition": "99.17%"},
                {"Statistic": "Total Preserved Core Logic", "root_partition": "100.00%"}]
        self.assertEqual(build.verify_partition_reuse(rows, "persona_b")["status"], "passed")
        rows[-1]["root_partition"] = "99.99%"
        with self.assertRaises(build.BuildError):
            build.verify_partition_reuse(rows, "persona_b")

    def test_design_closure_failure_is_not_reported_as_internal_timing_failure(self):
        rows = [{"Panel Name": "Design Closure Summary", "Result Flag": "Fail"}]
        self.assertEqual(build.design_closure_status(rows), "failed")
        self.assertEqual(build.design_closure_status(
            [{"Panel Name": "Design Closure Summary", "Result Flag": "Pass"}]), "passed")
        with self.assertRaises(build.BuildError):
            build.design_closure_status([])

    def test_hps_archive_is_required_before_output_creation(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / "project"
            with self.assertRaises(build.BuildError):
                build.prepare(output, "X2 Y7 X26 Y12", "X1 Y6 X27 Y13")
            self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
