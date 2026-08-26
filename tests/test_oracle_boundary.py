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

    def _quartus_real(self, version, fit_report, timing_report, *, write_outputs=True, layout="quartus"):
        temp = tempfile.TemporaryDirectory()
        root = Path(temp.name) / "quartus-root"
        if layout == "quartus":
            binary = root / "quartus" / "bin" / "quartus_sh"
        else:
            binary = root / "bin" / "quartus_sh"
        binary.parent.mkdir(parents=True)
        marker = Path(temp.name) / "compile-marker"
        lines = [
            "#!/usr/bin/env bash",
            "set -eu",
            "if [[ ${1:-} == --version ]]; then",
            f"  printf '%s\\n' 'Quartus Prime Version {version}'",
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

    def _run_real(self, root, marker, *, seed=None):
        target = ROOT / "build" / "oracle" / "010_blinky"
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
                "010_blinky",
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
                "Total ALMs | 2 | 100 | 2%",
                "Total registers | 2 | 100 | 2%",
                "Total pins | 2 | 10 | 20%",
                "Total block memory bits | 0 | 524288 | 0%",
                "Total RAM Blocks 0 10 0%",
                "PLLs | 0 | 4 | 0%",
                "M10K blocks | 0 | 10 | 0%",
                "Total MLAB memory bits | 0 | 64000 | 0%",
                "Total MLABs | 0 | 8 | 0%",
                "MLAB blocks | 0 | 8 | 0%",
                "Total 9x9 multipliers | 0 | 2 | 0%",
                "Total DSP blocks | 0 | 2 | 0%",
                "DSP blocks | 0 | 2 | 0%",
                "HPS blocks | 0 | 1 | 0%",
            ]
        ) + "\n"

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
        self.assertEqual(
            set(summary["hard_blocks"]),
            {"PLL", "BRAM/M10K", "MLAB/LUTRAM", "DSP", "HPS"},
        )
        self.assertTrue(all(record["used"] == 0 for record in summary["hard_blocks"].values()))
        self.assertEqual(summary["source_hashes"]["boards/de10nano/clocks.sdc"], hashlib.sha256((ROOT / "boards/de10nano/clocks.sdc").read_bytes()).hexdigest())
        quartus_provenance = summary["authenticated_tools"]["quartus_sh"]
        self.assertTrue(quartus_provenance["executable"])
        self.assertRegex(quartus_provenance["executable_sha256"], r"^[0-9a-f]{64}$")
        self.assertRegex(quartus_provenance["version_output_sha256"], r"^[0-9a-f]{64}$")
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
