import hashlib
import json
import os
import shlex
import shutil
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
ORACLE = ROOT / "scripts" / "build_oracle.sh"


class OracleBoundaryTests(unittest.TestCase):
    def test_mailbox_oracle_project_is_minimal_and_uses_shared_clock_constraint(self):
        project = ROOT / "experiments" / "020_linux_mailbox" / "oracle"
        qpf = project / "top.qpf"
        qsf = project / "top.qsf"
        self.assertTrue(qpf.is_file(), qpf)
        self.assertTrue(qsf.is_file(), qsf)
        qpf_text = qpf.read_text(encoding="utf-8")
        qsf_text = qsf.read_text(encoding="utf-8")
        self.assertIn('QUARTUS_VERSION = "17.0"', qpf_text)
        for expected in (
            'set_global_assignment -name FAMILY "Cyclone V"',
            "set_global_assignment -name DEVICE 5CSEBA6U23I7",
            "set_global_assignment -name TOP_LEVEL_ENTITY top",
            'set_global_assignment -name VERILOG_FILE "../../../experiments/020_linux_mailbox/rtl/top.v"',
            'set_global_assignment -name SDC_FILE "../../../boards/de10nano/clocks.sdc"',
            "set_location_assignment PIN_V11 -to FPGA_CLK1_50",
            'set_instance_assignment -name IO_STANDARD "3.3-V LVTTL" -to FPGA_CLK1_50',
        ):
            self.assertIn(expected, qsf_text)
        for forbidden in (
            "LED",
            "hps_gp_model.v",
            "output pin",
            "PIN_W15",
            "QSYS",
            "qsys",
        ):
            self.assertNotIn(forbidden, qsf_text)

    def test_mailbox_print_commands_bind_shared_sources_without_simulation_model(self):
        temp, root, marker = self._quartus("17.0.2")
        with temp:
            result = self._run(
                "--experiment",
                "020_linux_mailbox",
                "--print-commands",
                env={"QUARTUS_ROOTDIR": str(root)},
            )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("experiments/020_linux_mailbox/rtl/top.v", result.stdout)
        self.assertIn("boards/de10nano/clocks.sdc", result.stdout)
        self.assertNotIn("hps_gp_model.v", result.stdout)
        self.assertFalse(marker.exists())

    def _run(self, *args, env=None):
        merged = os.environ.copy()
        merged.pop("QUARTUS_ROOTDIR", None)
        if env:
            merged.update(env)
        return subprocess.run(
            [str(ORACLE), *args],
            cwd=ROOT,
            env=merged,
            text=True,
            capture_output=True,
        )

    def _quartus(self, version):
        temp = tempfile.TemporaryDirectory()
        root = Path(temp.name) / "quartus-root"
        binary = root / "quartus" / "bin" / "quartus_sh"
        binary.parent.mkdir(parents=True)
        marker = Path(temp.name) / "compile-marker"
        binary.write_text(
            "#!/usr/bin/env bash\n"
            "if [[ ${1:-} == --version ]]; then\n"
            f"  printf '%s\\n' 'Quartus Prime Version {version}'\n"
            "else\n"
            f"  : > '{marker}'\n"
            "fi\n",
            encoding="utf-8",
        )
        binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
        return temp, root, marker

    def _quartus_real(
        self,
        version,
        fit_report,
        timing_report,
        *,
        write_outputs=True,
        layout="quartus",
        version_output=None,
    ):
        temp = tempfile.TemporaryDirectory()
        root = Path(temp.name) / "quartus-root"
        if layout == "quartus":
            binary = root / "quartus" / "bin" / "quartus_sh"
        else:
            binary = root / "bin" / "quartus_sh"
        binary.parent.mkdir(parents=True)
        marker = Path(temp.name) / "compile-marker"
        version_output = version_output or f"Quartus Prime Version {version}"
        version_lines = version_output.splitlines() or [""]
        lines = [
            "#!/usr/bin/env bash",
            "set -eu",
            "if [[ ${1:-} == --version ]]; then",
            *[
                f"  printf '%s\\n' {shlex.quote(line)}"
                for line in version_lines
            ],
            "  exit 0",
            "fi",
            f"printf '%s\\n' compiled > {shlex.quote(str(marker))}",
            "mkdir -p output_files",
        ]
        if write_outputs:
            lines.extend(
                [
                    f"printf '%s' {shlex.quote('synthetic-rbf')} > output_files/top.rbf",
                    f"printf '%s' {shlex.quote(fit_report)} > output_files/top.fit.rpt",
                    f"printf '%s' {shlex.quote(timing_report)} > output_files/top.sta.rpt",
                ]
            )
        binary.write_text("\n".join(lines) + "\n", encoding="utf-8")
        binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
        return temp, root, marker

    def _run_real(self, root, marker, *, seed=None, experiment="010_blinky"):
        target = ROOT / "build" / "oracle" / experiment
        target_parent = target.parent
        target_parent.mkdir(parents=True, exist_ok=True)
        preserve = tempfile.TemporaryDirectory()
        backup = Path(preserve.name) / "saved"
        had_target = os.path.lexists(target)
        if had_target:
            shutil.move(str(target), str(backup))
        try:
            if seed is not None:
                seed(target)
            result = self._run(
                "--experiment",
                experiment,
                env={"QUARTUS_ROOTDIR": str(root)},
            )
            summary = {}
            summary_path = target / "build-summary.json"
            if summary_path.is_file():
                summary = json.loads(summary_path.read_text(encoding="utf-8"))
            return result, summary, marker.exists()
        finally:
            if os.path.lexists(target):
                if target.is_dir() and not target.is_symlink():
                    shutil.rmtree(target)
                else:
                    target.unlink()
            if had_target:
                shutil.move(str(backup), str(target))
            preserve.cleanup()

    @staticmethod
    def _complete_fit_report():
        return "\n".join(
            [
                "; Fitter Summary ;",
                "Quartus Prime Version 17.0.2 Build 602",
                "Total ALMs | 2 | 100 | 2%",
                "Total registers | 2 | 100 | 2%",
                "Total pins | 2 | 10 | 20%",
                "Total block memory bits | 0 | 524288 | 0%",
                "Total RAM Blocks | 0 | 10 | 0%",
                "Total PLLs | 0 | 4 | 0%",
                "Total 9x9 multipliers | 0 | 2 | 0%",
                "Total DSP Blocks | 0 | 2 | 0%",
                "; Fitter Settings ;",
            ]
        ) + "\n"

    @classmethod
    def _mailbox_fit_report(cls):
        return cls._complete_fit_report().replace(
            "; Fitter Settings ;",
            "cyclonev_hps_interface_mpu_general_purpose | 1 | 1 | 100%\n"
            "Total MLAB memory bits | 0 | 524288 | 0%\n"
            "SDRAM | 0 | 1 | 0%\n"
            "; Fitter Settings ;",
        )

    @staticmethod
    def _fmax_report(*rows):
        body = [
            "Fmax Summary",
            "| Clock Name | Fmax | Restricted Fmax | Slack |",
        ]
        body.extend(
            f"| {clock} | {fmax} MHz | {restricted} MHz | 0.00 ns |"
            for clock, fmax, restricted in rows
        )
        return "\n".join(body) + "\n"

    def test_absent_root_is_friendly_and_does_not_create_oracle_output(self):
        target = ROOT / "build" / "oracle" / "010_blinky"

        def snapshot(path):
            if not os.path.lexists(path):
                return ("absent",)
            result = []
            def visit(current):
                # Use lstat/scandir so a pre-existing output symlink cannot
                # make this hermetic snapshot traverse or observe an
                # unrelated tree outside the oracle lane.
                info = current.lstat()
                result.append(
                    (
                        str(current.relative_to(path.parent)),
                        stat.S_IFMT(info.st_mode),
                        info.st_size,
                        info.st_mtime_ns,
                        info.st_ino,
                    )
                )
                if stat.S_ISDIR(info.st_mode):
                    with os.scandir(current) as entries:
                        children = sorted(
                            (Path(entry.path) for entry in entries),
                            key=lambda child: child.name,
                        )
                    for child in children:
                        visit(child)

            visit(path)
            return tuple(result)

        before = snapshot(target)
        result = self._run("--experiment", "010_blinky")

        self.assertNotEqual(result.returncode, 0)
        self.assertIn(
            "Quartus oracle unavailable; OSS and simulation remain usable",
            result.stdout + result.stderr,
        )
        self.assertEqual(before, snapshot(target))

    def test_wrong_quartus_version_is_rejected(self):
        temp, root, marker = self._quartus("17.0.0")
        with temp:
            result = self._run(
                "--experiment",
                "010_blinky",
                "--print-commands",
                env={"QUARTUS_ROOTDIR": str(root)},
            )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("17.0.2", result.stdout + result.stderr)
        self.assertFalse(marker.exists())

    def test_exact_version_reaches_print_commands_without_compile(self):
        temp, root, marker = self._quartus("17.0.2")
        with temp:
            result = self._run(
                "--experiment",
                "010_blinky",
                "--print-commands",
                env={"QUARTUS_ROOTDIR": str(root)},
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("quartus_sh", result.stdout)
        self.assertIn("--flow compile", result.stdout)
        self.assertIn("experiments/010_blinky/rtl/top.v", result.stdout)
        self.assertIn("boards/de10nano/clocks.sdc", result.stdout)
        self.assertFalse(marker.exists())

    def test_timequest_restricted_fmax_is_bound_to_intended_clock(self):
        temp, root, marker = self._quartus_real(
            "17.0.2",
            self._complete_fit_report(),
            self._fmax_report(("FPGA_CLK1_50", "999.00", "123.45"), ("other_clock", "777.00", "777.00")),
        )
        with temp:
            result, summary, marker_seen = self._run_real(root, marker)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(summary["timing"]["achieved_mhz"], 123.45)
        self.assertEqual(summary["resources"]["IO"]["used"], 2)
        self.assertEqual(summary["resources"]["IO"]["available"], 10)
        self.assertEqual(
            set(summary["hard_blocks"]),
            {"PLL", "BRAM/M10K", "MLAB/LUTRAM", "DSP", "HPS"},
        )
        self.assertEqual(
            [summary["hard_blocks"][name]["used"] for name in ("PLL", "BRAM/M10K", "DSP")],
            [0, 0, 0],
        )
        evidence = summary["hard_block_evidence"]
        self.assertEqual(set(evidence), set(summary["hard_blocks"]))
        for name in ("PLL", "BRAM/M10K", "DSP"):
            self.assertEqual(evidence[name]["evidence_kind"], "fitter_summary")
            self.assertTrue(evidence[name]["measured"])
        for name in ("MLAB/LUTRAM", "HPS"):
            self.assertEqual(evidence[name]["evidence_kind"], "static_exclusion")
            self.assertFalse(evidence[name]["measured"])
            self.assertEqual(evidence[name]["status"], "excluded")
            self.assertIsNone(evidence[name]["used"])
            self.assertIsNone(evidence[name]["available"])
            self.assertEqual(
                [source["path"] for source in evidence[name]["exclusion"]["sources"]],
                [
                    "experiments/010_blinky/rtl/top.v",
                    "boards/de10nano/pins.qsf",
                    "boards/de10nano/clocks.sdc",
                    "experiments/010_blinky/oracle/top.qsf",
                ],
            )
            self.assertTrue(evidence[name]["exclusion"]["sources"])
        self.assertEqual(summary["source_hashes"]["boards/de10nano/clocks.sdc"], hashlib.sha256((ROOT / "boards/de10nano/clocks.sdc").read_bytes()).hexdigest())
        quartus_provenance = summary["authenticated_tools"]["quartus_sh"]
        self.assertTrue(quartus_provenance["executable"])
        self.assertEqual(quartus_provenance["path"], quartus_provenance["executable"])
        self.assertRegex(quartus_provenance["executable_sha256"], r"^[0-9a-f]{64}$")
        self.assertEqual(quartus_provenance["sha256"], quartus_provenance["executable_sha256"])
        self.assertRegex(quartus_provenance["version_output_sha256"], r"^[0-9a-f]{64}$")
        self.assertTrue(marker_seen)

    def test_mailbox_report_parser_emits_exact_semantic_evidence(self):
        temp, root, marker = self._quartus_real(
            "17.0.2",
            self._mailbox_fit_report(),
            self._fmax_report(("FPGA_CLK1_50", "100", "100")),
        )
        with temp:
            result, summary, marker_seen = self._run_real(
                root, marker, experiment="020_linux_mailbox"
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(marker_seen)
        self.assertEqual(
            list(summary["resource_evidence"]),
            [
                "clock_inputs",
                "external_input_ports",
                "external_output_ports",
                "bidirectional_ports",
                "hps_general_purpose_interfaces",
                "pll_blocks",
                "dsp_blocks",
                "block_memory_bits",
                "lutram_bits",
                "sdram_interfaces",
            ],
        )
        self.assertEqual(summary["resource_evidence"], {
            "clock_inputs": 1,
            "external_input_ports": 0,
            "external_output_ports": 0,
            "bidirectional_ports": 0,
            "hps_general_purpose_interfaces": 1,
            "pll_blocks": 0,
            "dsp_blocks": 0,
            "block_memory_bits": 0,
            "lutram_bits": 0,
            "sdram_interfaces": 0,
        })
        self.assertEqual(
            summary["hard_blocks"]["cyclonev_hps_interface_mpu_general_purpose"]["used"],
            1,
        )

    def test_mailbox_report_parser_accepts_real_single_value_mlab_row(self):
        fit = self._mailbox_fit_report().replace(
            "Total MLAB memory bits | 0 | 524288 | 0%",
            "Total MLAB memory bits | 0",
        )
        temp, root, marker = self._quartus_real(
            "17.0.2",
            fit,
            self._fmax_report(("FPGA_CLK1_50", "100", "100")),
        )
        with temp:
            result, summary, marker_seen = self._run_real(
                root, marker, experiment="020_linux_mailbox"
            )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(marker_seen)
        self.assertEqual(summary["resource_evidence"]["lutram_bits"], 0)

    def test_mailbox_raw_forbidden_rows_must_be_present_once_and_zero(self):
        rows = (
            ("block memory bits", "Total block memory bits | 0 | 524288 | 0%"),
            ("MLAB memory bits", "Total MLAB memory bits | 0 | 524288 | 0%"),
            ("SDRAM interfaces", "SDRAM | 0 | 1 | 0%"),
            ("RAM blocks", "Total RAM Blocks | 0 | 10 | 0%"),
            ("PLLs", "Total PLLs | 0 | 4 | 0%"),
            ("DSP blocks", "Total DSP Blocks | 0 | 2 | 0%"),
        )
        for label, row in rows:
            with self.subTest(label=label):
                base = self._mailbox_fit_report()
                missing = base.replace(row + "\n", "")
                duplicate = base.replace(
                    "; Fitter Settings ;", row + "\n; Fitter Settings ;", 1
                )
                nonzero = base.replace(row, row.replace("| 0 |", "| 1 |", 1))
                for variant, fit in (
                    ("missing", missing),
                    ("duplicate", duplicate),
                    ("nonzero", nonzero),
                ):
                    with self.subTest(variant=variant):
                        temp, root, marker = self._quartus_real(
                            "17.0.2",
                            fit,
                            self._fmax_report(("FPGA_CLK1_50", "100", "100")),
                        )
                        with temp:
                            result, summary, marker_seen = self._run_real(
                                root, marker, experiment="020_linux_mailbox"
                            )
                        self.assertNotEqual(result.returncode, 0)
                        self.assertEqual(summary["hard_block_status"], "fail")
                        self.assertTrue(marker_seen)

    def test_mailbox_allowed_hps_row_must_be_present_once_with_exact_count(self):
        row = "cyclonev_hps_interface_mpu_general_purpose | 1 | 1 | 100%"
        base = self._mailbox_fit_report()
        variants = (
            ("missing", base.replace(row + "\n", "")),
            (
                "duplicate",
                base.replace("; Fitter Settings ;", row + "\n; Fitter Settings ;", 1),
            ),
            ("wrong count", base.replace(row, row.replace("| 1 |", "| 2 |", 1))),
        )
        for label, fit in variants:
            with self.subTest(label=label):
                temp, root, marker = self._quartus_real(
                    "17.0.2",
                    fit,
                    self._fmax_report(("FPGA_CLK1_50", "100", "100")),
                )
                with temp:
                    result, summary, marker_seen = self._run_real(
                        root, marker, experiment="020_linux_mailbox"
                    )
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(summary["hard_block_status"], "fail")
                self.assertTrue(marker_seen)

    def test_oracle_selector_rejects_repeated_and_mixed_experiment_selectors(self):
        temp, root, marker = self._quartus("17.0.2")
        with temp:
            repeated = self._run(
                "--experiment",
                "010_blinky",
                "--experiment",
                "020_linux_mailbox",
                "--print-commands",
                env={"QUARTUS_ROOTDIR": str(root)},
            )
            mixed = self._run(
                "--experiment",
                "010_blinky",
                "020_linux_mailbox",
                "--print-commands",
                env={"QUARTUS_ROOTDIR": str(root)},
            )
        self.assertNotEqual(repeated.returncode, 0)
        self.assertNotEqual(mixed.returncode, 0)
        self.assertFalse(marker.exists())

    def test_hard_resource_labels_without_fitted_summary_fail_closed(self):
        fit = "\n".join(
            [
                "Device capability table",
                "Total RAM Blocks | 0 | 10 | 0%",
                "Total PLLs | 0 | 4 | 0%",
                "Total DSP Blocks | 0 | 2 | 0%",
            ]
        ) + "\n"
        temp, root, marker = self._quartus_real(
            "17.0.2", fit, self._fmax_report(("FPGA_CLK1_50", "100", "100"))
        )
        with temp:
            result, summary, marker_seen = self._run_real(root, marker)

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(summary["hard_block_status"], "fail")
        self.assertTrue(all(name not in summary["hard_blocks"] for name in ("PLL", "BRAM/M10K", "DSP")))
        self.assertTrue(marker_seen)

    def test_timequest_multiple_operating_corners_use_conservative_minimum(self):
        timing = "\n".join(
            [
                "+------------------------------------------+",
                "; Clocks                                   ;",
                "+--------------+------+--------+-----------+",
                "; Clock Name   ; Type ; Period ; Frequency ;",
                "+--------------+------+--------+-----------+",
                "; FPGA_CLK1_50 ; Base ; 20.000 ; 50.0 MHz  ;",
                "+--------------+------+--------+-----------+",
                "+----------------------------------------------------+",
                "; Slow 1100mV 100C Model Fmax Summary                ;",
                "+------------+-----------------+--------------+------+",
                "; Fmax       ; Restricted Fmax ; Clock Name   ; Note ;",
                "+------------+-----------------+--------------+------+",
                "; 351.62 MHz ; 351.62 MHz      ; FPGA_CLK1_50 ;      ;",
                "+------------+-----------------+--------------+------+",
                "; Slow 1100mV -40C Model Fmax Summary                ;",
                "+------------+-----------------+--------------+------+",
                "; Fmax       ; Restricted Fmax ; Clock Name   ; Note ;",
                "+------------+-----------------+--------------+------+",
                "; 321.34 MHz ; 321.34 MHz      ; FPGA_CLK1_50 ;      ;",
            ]
        ) + "\n"
        temp, root, marker = self._quartus_real(
            "17.0.2", self._complete_fit_report(), timing
        )
        with temp:
            result, summary, marker_seen = self._run_real(root, marker)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(summary["timing"]["achieved_mhz"], 321.34)
        self.assertTrue(marker_seen)

    def test_timequest_malformed_exact_clock_restricted_fmax_fails_closed(self):
        timing = "\n".join(
            [
                "Fmax Summary",
                "; Fmax       ; Restricted Fmax ; Clock Name   ; Note ;",
                "; 351.62 MHz ; not-a-frequency ; FPGA_CLK1_50 ;      ;",
            ]
        ) + "\n"
        temp, root, marker = self._quartus_real(
            "17.0.2", self._complete_fit_report(), timing
        )
        with temp:
            result, summary, marker_seen = self._run_real(root, marker)

        self.assertNotEqual(result.returncode, 0)
        self.assertIsNone(summary["timing"]["achieved_mhz"])
        self.assertEqual(summary["timing"]["status"], "fail")
        self.assertTrue(marker_seen)

    def test_capability_prose_outside_resource_summary_is_ignored(self):
        fit = self._complete_fit_report() + "\n".join(
            [
                "; Parallel Compilation ;",
                "; Processors ; Number ;",
                "; Number detected on machine ; 24 ;",
                "; PLL capability ; 6 ;",
                "; Fitter Resource Utilization by Entity ;",
                "; mlab_entity_pin ; MLAB0 ;",
                "; Hard processor system peripheral utilization ; ; ;",
                ";     -- Boot from FPGA ; 0 / 1 ( 0 % ) ;",
                "; Total MLAB memory bits ; 0 ;",
                "; Pin Name ; FPGA_CLK1_50 ; MLAB0 ;",
            ]
        ) + "\n"
        temp, root, marker = self._quartus_real(
            "17.0.2", fit, self._fmax_report(("FPGA_CLK1_50", "100", "100"))
        )
        with temp:
            result, summary, marker_seen = self._run_real(root, marker)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(summary["hard_block_status"], "pass")
        self.assertEqual(summary["unknown_resources"], {})
        self.assertEqual(
            [summary["hard_blocks"][name]["used"] for name in ("PLL", "BRAM/M10K", "DSP")],
            [0, 0, 0],
        )
        self.assertIsNone(summary["hard_blocks"]["MLAB/LUTRAM"]["used"])
        self.assertIsNone(summary["hard_blocks"]["HPS"]["used"])
        self.assertTrue(marker_seen)

    def test_contradictory_physical_fitted_rows_fail_closed(self):
        fit = self._complete_fit_report() + "\n".join(
            [
                "; Fitter Resource Usage Summary ;",
                "; Total DSP Blocks ; 1 / 112 ; 1 % ;",
            ]
        ) + "\n"
        temp, root, marker = self._quartus_real(
            "17.0.2", fit, self._fmax_report(("FPGA_CLK1_50", "100", "100"))
        )
        with temp:
            result, summary, marker_seen = self._run_real(root, marker)

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(summary["hard_block_status"], "fail")
        self.assertTrue(marker_seen)

    def test_banner_plus_version_stores_exact_quartus_version_line(self):
        version_line = "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition"
        temp, root, marker = self._quartus_real(
            "17.0.2",
            self._complete_fit_report(),
            self._fmax_report(("FPGA_CLK1_50", "100", "100")),
            version_output=(
                "Quartus Prime Shell\n"
                f"{version_line}\n"
                "Copyright (C) 2017 Intel Corporation. All rights reserved."
            ),
        )
        with temp:
            result, summary, marker_seen = self._run_real(root, marker)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            summary["authenticated_tools"]["quartus_sh"]["version"], version_line
        )
        self.assertTrue(marker_seen)

    def test_oracle_qsf_ignores_partitions_without_ignored_incremental_assignment(self):
        qsf = (ROOT / "experiments" / "010_blinky" / "oracle" / "top.qsf").read_text(
            encoding="utf-8"
        )
        self.assertIn("set_global_assignment -name IGNORE_PARTITIONS ON", qsf)
        self.assertNotIn("INCREMENTAL_COMPILATION OFF", qsf)

    def test_fitter_forbidden_usage_fails_with_measured_evidence(self):
        fit = self._complete_fit_report().replace(
            "Total DSP Blocks | 0 | 2 | 0%", "Total DSP Blocks | 1 | 2 | 50%"
        )
        temp, root, marker = self._quartus_real(
            "17.0.2", fit, self._fmax_report(("FPGA_CLK1_50", "100", "100"))
        )
        with temp:
            result, summary, marker_seen = self._run_real(root, marker)

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(summary["hard_blocks"]["DSP"]["used"], 1)
        self.assertEqual(summary["hard_block_evidence"]["DSP"]["evidence_kind"], "fitter_summary")
        self.assertTrue(marker_seen)

    def test_static_exclusion_is_rejected_when_report_measures_mlab_or_hps(self):
        for row in (
            "Total MLABs | 1 | 8 | 12.5%",
            "Total HPS blocks | 1 | 1 | 100%",
        ):
            fit = self._complete_fit_report() + "; Fitter Resource Usage Summary ;\n" + row + "\n"
            temp, root, marker = self._quartus_real(
                "17.0.2", fit, self._fmax_report(("FPGA_CLK1_50", "100", "100"))
            )
            with temp:
                result, summary, marker_seen = self._run_real(root, marker)

            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(summary["hard_block_status"], "fail")
            self.assertTrue(marker_seen)

    def test_timequest_missing_or_ambiguous_restricted_fmax_fails(self):
        fit = self._complete_fit_report()
        reports = (
            self._fmax_report(("FPGA_CLK1_50", "123.45", "")),
            self._fmax_report(
                ("FPGA_CLK1_50", "123.45", "100.00"),
                ("FPGA_CLK1_50", "124.45", "101.00"),
            ),
        )
        for timing in reports:
            temp, root, _marker = self._quartus_real("17.0.2", fit, timing)
            with temp:
                result, _summary, _marker_seen = self._run_real(root, _marker)
            self.assertNotEqual(result.returncode, 0, result.stderr)
            self.assertIn("timing", (result.stdout + result.stderr).lower())

    def test_stale_project_outputs_cannot_attest_success(self):
        stale_fit = self._complete_fit_report()
        stale_timing = self._fmax_report(("FPGA_CLK1_50", "999.00", "999.00"))

        def seed(target):
            output = target / "project" / "output_files"
            output.mkdir(parents=True)
            (output / "top.rbf").write_bytes(b"stale-rbf")
            (output / "top.fit.rpt").write_text(stale_fit, encoding="utf-8")
            (output / "top.sta.rpt").write_text(stale_timing, encoding="utf-8")

        temp, root, marker = self._quartus_real(
            "17.0.2", stale_fit, stale_timing, write_outputs=False
        )
        with temp:
            result, _summary, marker_seen = self._run_real(root, marker, seed=seed)

        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(marker_seen)

    def test_stale_output_symlink_does_not_delete_outside_tree(self):
        external = tempfile.TemporaryDirectory()
        external_file = Path(external.name) / "sentinel"
        external_file.write_text("keep", encoding="utf-8")

        def seed(target):
            output = target / "project" / "output_files"
            output.mkdir(parents=True)
            (output / "redirect").symlink_to(Path(external.name), target_is_directory=True)

        temp, root, marker = self._quartus_real(
            "17.0.2", self._complete_fit_report(), self._fmax_report(("FPGA_CLK1_50", "100", "100")), write_outputs=False
        )
        with external, temp:
            result, _summary, marker_seen = self._run_real(root, marker, seed=seed)
            external_contents = external_file.read_text(encoding="utf-8")

        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(marker_seen)
        self.assertEqual(external_contents, "keep")

    def test_oss_and_sim_make_recipes_have_no_quartus_lane_reference(self):
        for target in ("oss", "sim"):
            result = subprocess.run(
                ["make", "-n", target, "EXP=010_blinky"],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotIn("quartus", result.stdout.lower())
            self.assertNotIn("quartus_rootdir", result.stdout.lower())


if __name__ == "__main__":
    unittest.main()
