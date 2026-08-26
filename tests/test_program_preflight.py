from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PROGRAM = ROOT / "scripts" / "program.py"
TARGET = "5CSEBA6U23I7"
EXPERIMENT = "999_program_test"
LANE = "oss"


def _sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


class ProgramPreflightTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix="program-preflight-")
        self.fixture = Path(self.temp.name)
        self.output = ROOT / "build" / LANE / EXPERIMENT
        self.output.mkdir(parents=True, exist_ok=True)
        self.artifact = self.output / "top.rbf"
        self.artifact.write_bytes(b"program-test-rbf\n")
        self.manifest = self.output / "manifest.json"
        self._write_manifest()
        self.actions = self.fixture / "actions.log"
        self.programmer = self.fixture / "openFPGALoader"
        self._write_programmer()
        self.ssh = self.fixture / "ssh"
        self.scp = self.fixture / "scp"
        self._write_remote_tools()

    def tearDown(self) -> None:
        shutil.rmtree(self.output, ignore_errors=True)
        shutil.rmtree(ROOT / "build" / "oracle" / EXPERIMENT, ignore_errors=True)
        self.temp.cleanup()

    def _write_manifest(self, **overrides: object) -> None:
        digest = _sha256(self.artifact)
        resources = {
            "MISTRAL_BUF": {"used": 3, "available": 0},
            "MISTRAL_CLKENA": {"used": 1, "available": 2},
            "MISTRAL_COMB": {"used": 28, "available": 83820},
            "MISTRAL_FF": {"used": 25, "available": 167640},
            "MISTRAL_IO": {"used": 2, "available": 472},
            "MISTRAL_M10K": {"used": 0, "available": 553},
            "cyclonev_hps_interface_mpu_general_purpose": {"used": 0, "available": 1},
            "cyclonev_oscillator": {"used": 0, "available": 1},
        }
        build = {
            "status": "pass",
            "build_status": "pass",
            "route_status": "pass",
            "route": {"status": "pass", "unrouted": False},
            "timing": {
                "status": "pass",
                "requested_mhz": 50.0,
                "achieved_mhz": 200.0,
                "clock": "FPGA_CLK1_50",
            },
            "resources": resources,
            "resource_classes": {
                "MISTRAL_BUF": "ordinary",
                "MISTRAL_CLKENA": "ordinary",
                "MISTRAL_COMB": "ordinary",
                "MISTRAL_FF": "ordinary",
                "MISTRAL_IO": "ordinary",
                "MISTRAL_M10K": "forbidden",
                "cyclonev_hps_interface_mpu_general_purpose": "forbidden",
                "cyclonev_oscillator": "forbidden",
            },
            "hard_block_status": "pass",
            "hard_blocks": {
                "MISTRAL_M10K": {"used": 0, "available": 553},
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 0, "available": 1},
                "cyclonev_oscillator": {"used": 0, "available": 1},
            },
            "unknown_resources": {},
            "reproducibility": {
                "rbf_sha256": digest,
                "rbf_size_bytes": self.artifact.stat().st_size,
                "rbf_stability_measured": True,
                "rbf_stable": True,
            },
        }
        manifest = {
            "schema": 2,
            "experiment": EXPERIMENT,
            "lane": LANE,
            "target": TARGET,
            "artifacts": [
                {
                    "path": f"build/{LANE}/{EXPERIMENT}/top.rbf",
                    "sha256": digest,
                }
            ],
            "build": build,
        }
        for path, value in overrides.items():
            if path == "target":
                manifest["target"] = value
            elif path == "artifact_hash":
                manifest["artifacts"][0]["sha256"] = value
            elif path == "schema":
                manifest["schema"] = value
            elif path == "build":
                manifest["build"] = value
            elif path == "lane":
                manifest["lane"] = value
        self.manifest.write_text(json.dumps(manifest, indent=2), encoding="utf-8")

    def _write_executable(self, path: Path, body: str) -> None:
        path.write_text("#!/usr/bin/env bash\nset -eu\n" + body, encoding="utf-8")
        path.chmod(path.stat().st_mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)

    def _write_programmer(
        self,
        *,
        scan: str | None = None,
        detect: str | None = None,
        exit_code: int = 0,
        mutate_artifact_on_scan: bool = False,
        capture_programmed_bytes: Path | None = None,
    ) -> None:
        scan = scan if scan is not None else "Bus 1 0x09fb:0x6810 usb-blasterII de10nano\n"
        detect = detect if detect is not None else "IDCODE: 0x02d020dd\n"
        mutate = f"printf '%s' MUTATED > {str(self.artifact)!r}\n" if mutate_artifact_on_scan else ""
        capture = (
            f"snapshot=\"${{!#}}\"\ncp -- \"$snapshot\" {str(capture_programmed_bytes)!r}\n"
            if capture_programmed_bytes is not None
            else ""
        )
        self._write_executable(
            self.programmer,
            ""
            f"printf '%s\\n' \"$*\" >> {str(self.actions)!r}\n"
            "case \" $* \" in\n"
            f"  *'--scan-usb'*) {mutate}printf '%b' {scan!r} ;;\n"
            f"  *'--detect'*) printf '%b' {detect!r} ;;\n"
            f"  *'--write-sram'*) {capture}printf '%s\\n' PROGRAM >> {str(self.actions)!r}; exit {exit_code} ;;\n"
            "esac\n",
        )

    def _write_remote_tools(
        self,
        *,
        digest: str | None = None,
        architecture: str = "aarch64",
        fifo: bool = True,
        main: bool = True,
        main_sha256: str = "a" * 64,
        main_pid_count: int = 1,
        main_fifo_inode: bool = True,
        main_root_owned: bool = True,
        fifo_root_owned: bool = True,
        fifo_nlink: int = 1,
        stage_dir_mode: str = "700",
        stage_dir_uid: int = 0,
        stage_dir_type: str = "directory",
        stage_dir_nlink: int = 2,
        stage_file_mode: str = "400",
        stage_file_uid: int = 0,
        stage_file_type: str = "regular file",
        stage_file_nlink: int = 1,
        mkdir_exit: int = 0,
        verify_exit: int = 0,
        scp_exit: int = 0,
        load_exit: int = 0,
        control_exit: int = 0,
        control_stop_exit: int = 0,
        stage_exists: bool = False,
    ) -> None:
        digest = digest or _sha256(self.artifact)
        legacy_stage = f"/tmp/misteross-{EXPERIMENT}-{digest[:16]}.rbf"
        fifo_result = "" if fifo else "exit 1\n"
        main_output = "Main_MiSTer\n" if main else "other-process\n"
        stage_result = "exit 1" if stage_exists else "exit 0"
        if not main:
            main_pid_count = 0
        if main_pid_count != 1:
            main_pid_records = "".join(
                f"MAIN|{pid}|0|/usr/bin/Main_MiSTer|regular file|0|0|755|1\n"
                for pid in range(100, 100 + main_pid_count)
            )
        else:
            main_pid_records = (
                "MAIN|100|"
                + ("0" if main_root_owned else "1000")
                + "|/usr/bin/Main_MiSTer|regular file|"
                + ("0" if main_root_owned else "1000")
                + "|0|755|1\n"
            )
        fifo_record = (
            "FIFO|fifo|"
            + ("0" if fifo_root_owned else "1000")
            + "|0|666|4242|"
            + str(fifo_nlink)
            + "\n"
        )
        main_fd = "MAIN_FD|100|4242|1\n" if main_fifo_inode else "MAIN_FD|100|9999|1\n"
        preflight_output = (
            "PREFLIGHT_V1\n"
            + f"ARCH|{architecture}\n"
            + (fifo_record if fifo else "")
            + main_pid_records
            + (f"MAIN_SHA|100|{main_sha256}\n" if main_pid_count == 1 else "")
            + (main_fd if main_pid_count == 1 else "")
            + "TMP|directory|1\n"
        )
        verify_output = (
            "VERIFY_V1\n"
            + f"DIR|{stage_dir_type}|{stage_dir_uid}|0|{stage_dir_mode}|{stage_dir_nlink}\n"
            + f"FILE|{stage_file_type}|{stage_file_uid}|0|{stage_file_mode}|{stage_file_nlink}\n"
        )
        mkdir_branch = (
            f"  *'MISTEROSS_MKDIR_V1'*) exit {mkdir_exit} ;;\n"
            if mkdir_exit
            else f"  *'MISTEROSS_MKDIR_V1'*) printf '%s\\n' 'DIR|{stage_dir_type}|{stage_dir_uid}|0|{stage_dir_mode}|{stage_dir_nlink}' ; exit 0 ;;\n"
        )
        remote_body = (
            ""
            f"printf '%s\\n' \"$*\" >> {str(self.actions)!r}\n"
            "remote_command=\"${!#}\"\n"
            "case \" $* \" in\n"
            f"  *'MISTEROSS_PREFLIGHT_V1'*) printf '%b' {preflight_output!r} ; exit 0 ;;\n"
            + mkdir_branch
            + f"  *'-O exit'*) exit {control_exit} ;;\n"
            + f"  *'-O stop'*) exit {control_stop_exit} ;;\n"
            + f"  *'MISTEROSS_VERIFY_V1'*) verify_stage=\"${{remote_command#*MISTEROSS_VERIFY_V1 }}\"; verify_stage=\"${{verify_stage%%;*}}\"; printf '%b' {verify_output!r}; printf 'HASH|%s|%s\\n' {digest!r} \"$verify_stage\"; exit {verify_exit} ;;\n"
            + f"  *'MISTEROSS_RACE_V1'*) printf '%s\\n' RACE; exit 1 ;;\n"
            + f"  *'uname -m'*) printf '%s' {architecture!r} ;;\n"
            + f"  *'test -p /dev/MiSTer_cmd'*) {fifo_result} ;;\n"
            + "  *'test -d /tmp'*) exit 0 ;;\n"
            + f"  *'ps -e -o comm='*) printf '%b' {main_output!r} ;;\n"
            + f"  *'test ! -e {legacy_stage}'*) {stage_result} ;;\n"
            + "  *'load_core'*) printf '%s\\n' LOAD >> "
            + f"{str(self.actions)!r}; exit {load_exit} ;;\n"
            + f"  *'sha256sum'*) printf '%s  %s\\n' {digest!r} {legacy_stage!r} ;;\n"
            + "  *) exit 0 ;;\n"
            + "esac\n"
        )
        self._write_executable(self.ssh, remote_body)
        self._write_executable(
            self.scp,
            f"printf '%s\\n' \"$*\" >> {str(self.actions)!r}\nprintf '%s\\n' SCP >> {str(self.actions)!r}\nexit {scp_exit}\n",
        )

    def _env(
        self,
        *,
        transport: str = "jtag",
        dry_run: bool = True,
        host: str | None = None,
        user: str | None = None,
        expected_board: str | None = None,
        expected_main_sha256: str | None = "a" * 64,
    ) -> dict[str, str]:
        env = os.environ.copy()
        if expected_board is None:
            expected_board = "misterpi" if transport == "mister" else "de10nano"
        env.update(
            {
                "PROGRAMMER": str(self.programmer),
                "PROGRAM_TRANSPORT": transport,
                "PROGRAM_DRY_RUN": "1" if dry_run else "0",
                "PROGRAM_EXPECTED_BOARD": expected_board,
                "PATH": os.pathsep.join((str(self.fixture), env.get("PATH", ""))),
            }
        )
        if expected_main_sha256 is not None:
            env["MISTER_EXPECTED_MAIN_SHA256"] = expected_main_sha256
        else:
            env.pop("MISTER_EXPECTED_MAIN_SHA256", None)
        if host is not None:
            env["MISTER_HOST"] = host
        if user is not None:
            env["MISTER_USER"] = user
        return env

    def _run(
        self,
        *,
        transport: str = "jtag",
        dry_run: bool = True,
        args: tuple[str, ...] = (),
        host: str | None = None,
        user: str | None = None,
        expected_board: str | None = None,
        expected_main_sha256: str | None = "a" * 64,
        lane: str = LANE,
    ) -> subprocess.CompletedProcess[str]:
        command = [
            str(PROGRAM),
            "--repo-root",
            str(ROOT),
            "--experiment",
            EXPERIMENT,
            "--build",
            lane,
            *args,
        ]
        return subprocess.run(
            command,
            cwd=ROOT,
            env=self._env(
                transport=transport,
                dry_run=dry_run,
                host=host,
                user=user,
                expected_board=expected_board,
                expected_main_sha256=expected_main_sha256,
            ),
            text=True,
            capture_output=True,
        )

    def test_missing_rbf_is_rejected_before_programmer(self) -> None:
        self.artifact.unlink()
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing canonical RBF", result.stdout + result.stderr)
        self.assertFalse(self.actions.exists())

    def test_manifest_hash_mismatch_is_rejected(self) -> None:
        self._write_manifest(artifact_hash="0" * 64)
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("hash", (result.stdout + result.stderr).lower())
        self.assertFalse(self.actions.exists())

    def _mutate_build(self, path: tuple[str, ...], value: object) -> None:
        manifest = json.loads(self.manifest.read_text(encoding="utf-8"))
        current = manifest["build"]
        for component in path[:-1]:
            current = current[component]
        current[path[-1]] = value
        self.manifest.write_text(json.dumps(manifest, indent=2), encoding="utf-8")

    def _write_oracle_manifest(self, *, stability_measured: bool = False) -> Path:
        output = ROOT / "build" / "oracle" / EXPERIMENT
        output.mkdir(parents=True, exist_ok=True)
        artifact = output / "top.rbf"
        artifact.write_bytes(self.artifact.read_bytes())
        digest = _sha256(artifact)
        static = {
            "used": None,
            "available": None,
            "status": "excluded",
            "evidence_kind": "static_exclusion",
            "measured": False,
            "exclusion": {
                "basis": "static source/project exclusion",
                "patterns": ["forbidden"],
                "sources": [{"path": "source", "sha256": "b" * 64}],
            },
        }
        provenance = {
            "path": "/opt/quartus/17.0.2/quartus/bin/quartus_sh",
            "executable": "/opt/quartus/17.0.2/quartus/bin/quartus_sh",
            "sha256": "c" * 64,
            "executable_sha256": "c" * 64,
            "version": "Quartus Prime Version 17.0.2 Build 602",
            "required_version": "17.0.2",
            "version_output_sha256": "d" * 64,
        }
        manifest = {
            "schema": 2,
            "experiment": EXPERIMENT,
            "lane": "oracle",
            "target": TARGET,
            "artifacts": [{"path": f"build/oracle/{EXPERIMENT}/top.rbf", "sha256": digest}],
            "build": {
                "status": "pass",
                "build_status": "pass",
                "route_status": "pass",
                "route": {"status": "pass", "unrouted": False},
                "timing": {
                    "status": "pass",
                    "requested_mhz": 50.0,
                    "achieved_mhz": 100.0,
                    "clock": "FPGA_CLK1_50",
                },
                "resources": {
                    "ALM": {"used": 2, "available": 100},
                    "register": {"used": 2, "available": 100},
                    "IO": {"used": 2, "available": 10},
                },
                "resource_classes": {"ALM": "ordinary", "register": "ordinary", "IO": "ordinary"},
                "unknown_resources": {},
                "hard_block_status": "pass",
                "hard_blocks": {
                    "PLL": {"used": 0, "available": 4, "evidence_kind": "fitter_summary", "measured": True},
                    "BRAM/M10K": {"used": 0, "available": 10, "evidence_kind": "fitter_summary", "measured": True},
                    "DSP": {"used": 0, "available": 2, "evidence_kind": "fitter_summary", "measured": True},
                    "MLAB/LUTRAM": static,
                    "HPS": static,
                },
                "hard_block_evidence": {
                    "PLL": {"used": 0, "available": 4, "evidence_kind": "fitter_summary", "measured": True},
                    "BRAM/M10K": {"used": 0, "available": 10, "evidence_kind": "fitter_summary", "measured": True},
                    "DSP": {"used": 0, "available": 2, "evidence_kind": "fitter_summary", "measured": True},
                    "MLAB/LUTRAM": static,
                    "HPS": static,
                },
                "authenticated_tools": {"quartus_sh": provenance},
                "tool_pins": {"quartus": provenance},
                "reproducibility": {
                    "rbf_sha256": digest,
                    "rbf_size_bytes": artifact.stat().st_size,
                    "rbf_stability_measured": stability_measured,
                    "rbf_stable": True if stability_measured else None,
                    "rbf_stability_reason": "oracle stability is unmeasured by contract",
                },
            },
        }
        manifest_path = output / "manifest.json"
        manifest_path.write_text(json.dumps(manifest, indent=2), encoding="utf-8")
        return manifest_path

    def test_route_status_must_be_pass_and_routed(self) -> None:
        self._mutate_build(("route_status",), "fail")
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("route_status", result.stdout + result.stderr)

        self._write_manifest()
        self._mutate_build(("route", "unrouted"), True)
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unrouted", result.stdout + result.stderr)

    def test_timing_status_and_frequency_are_required(self) -> None:
        self._mutate_build(("timing", "status"), "fail")
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("timing.status", result.stdout + result.stderr)

        self._write_manifest()
        self._mutate_build(("timing", "achieved_mhz"), 12.0)
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("timing", (result.stdout + result.stderr).lower())

    def test_hard_block_and_unknown_resource_status_are_required(self) -> None:
        self._mutate_build(("hard_blocks", "MISTRAL_M10K", "used"), 1)
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("hard-block", (result.stdout + result.stderr).lower())

        self._write_manifest()
        self._mutate_build(("unknown_resources",), {"mystery": 1})
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unknown", (result.stdout + result.stderr).lower())

    def test_stability_status_schema_and_size_are_required(self) -> None:
        self._mutate_build(("reproducibility", "rbf_stability_measured"), False)
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("stability", (result.stdout + result.stderr).lower())

        self._write_manifest(schema=1)
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("schema", (result.stdout + result.stderr).lower())

        self._write_manifest()
        self._mutate_build(("reproducibility", "rbf_size_bytes"), 0)
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("size", (result.stdout + result.stderr).lower())

    def test_wrong_target_is_rejected(self) -> None:
        self._write_manifest(target="5CSXFC6D6F31C6G")
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(TARGET, result.stdout + result.stderr)
        self.assertFalse(self.actions.exists())

    def test_symlinked_artifact_is_rejected(self) -> None:
        outside = self.fixture / "outside.rbf"
        outside.write_bytes(self.artifact.read_bytes())
        self.artifact.unlink()
        self.artifact.symlink_to(outside)
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("symlink", (result.stdout + result.stderr).lower())
        self.assertFalse(self.actions.exists())

    def test_zero_jtag_boards_is_safe_stop(self) -> None:
        self._write_programmer(scan="No cable\n")
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no DE10-Nano detected", result.stdout + result.stderr)
        self.assertNotIn("PROGRAM", self.actions.read_text() if self.actions.exists() else "")

    def test_two_jtag_boards_stop_even_with_cable_index(self) -> None:
        self._write_programmer(
            scan=(
                "Bus 1 0x09fb:0x6810 usb-blasterII de10nano\n"
                "Bus 2 0x09fb:0x6810 usb-blasterII_2 de10nano\n"
            )
        )
        for args in ((), ("--cable-index", "1")):
            result = self._run(args=args)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("multiple", (result.stdout + result.stderr).lower())
            self.assertNotIn("PROGRAM", self.actions.read_text() if self.actions.exists() else "")

    def test_one_jtag_board_dry_run_checks_but_never_programs(self) -> None:
        result = self._run()
        self.assertEqual(result.returncode, 0, result.stderr)
        text = result.stdout + result.stderr
        self.assertIn("--write-sram", text)
        self.assertNotIn("--write-flash", text)
        self.assertNotIn("PROGRAM", self.actions.read_text() if self.actions.exists() else "")

    def test_programmer_failure_is_reported_without_persistent_mode(self) -> None:
        self._write_programmer(exit_code=7)
        result = self._run(dry_run=False)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("programmer", (result.stdout + result.stderr).lower())
        text = result.stdout + result.stderr
        self.assertNotIn("--write-flash", text)
        self.assertNotIn(" -f", text)

    def test_mister_requires_explicit_host_and_user(self) -> None:
        result = self._run(transport="mister", host=None, user=None)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("explicit", (result.stdout + result.stderr).lower())
        self.assertFalse(self.actions.exists())

    def test_mister_dry_run_performs_read_only_checks_without_scp_or_load(self) -> None:
        result = self._run(transport="mister", host="mister.test", user="root")
        self.assertEqual(result.returncode, 0, result.stderr)
        actions = self.actions.read_text()
        self.assertIn("uname -m", actions)
        self.assertNotIn("SCP", actions)
        self.assertNotIn("LOAD", actions)
        self.assertIn("load_core /tmp/", result.stdout)

    def test_mister_non_dry_run_verifies_hash_then_writes_fifo(self) -> None:
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertEqual(result.returncode, 0, result.stderr)
        actions = self.actions.read_text()
        self.assertIn("SCP", actions)
        self.assertIn("sha256sum", actions)
        self.assertIn("LOAD", actions)
        self.assertNotIn("/media/fat", result.stdout + result.stderr)
        self.assertIn("load_core /tmp/", result.stdout)

    def test_mister_remote_hash_mismatch_stops_before_fifo_write(self) -> None:
        self._write_remote_tools(digest="f" * 64)
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("hash", (result.stdout + result.stderr).lower())
        actions = self.actions.read_text()
        self.assertIn("SCP", actions)
        self.assertNotIn("LOAD", actions)

    def test_mister_scp_failure_stops_before_hash_or_fifo_write(self) -> None:
        self._write_remote_tools(scp_exit=7)
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("SCP", (result.stdout + result.stderr))
        actions = self.actions.read_text()
        self.assertIn("SCP", actions)
        self.assertNotIn("MISTEROSS_VERIFY_V1", actions)
        self.assertNotIn("LOAD", actions)

    def test_mister_fifo_failure_is_reported_after_verified_upload(self) -> None:
        self._write_remote_tools(load_exit=9)
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("FIFO", (result.stdout + result.stderr))
        actions = self.actions.read_text()
        self.assertIn("SCP", actions)
        self.assertIn("sha256sum", actions)
        self.assertIn("LOAD", actions)

    def test_mister_staging_collision_stops_before_upload(self) -> None:
        self._write_remote_tools(mkdir_exit=1)
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("staging", (result.stdout + result.stderr).lower())
        actions = self.actions.read_text()
        self.assertNotIn("SCP", actions)
        self.assertNotIn("LOAD", actions)

    def test_mister_rejects_command_injection_in_host_and_user(self) -> None:
        marker = self.fixture / "injected"
        for value, keyword in ((f"mister.test;touch {marker}", "host"), (f"root;touch {marker}", "user")):
            result = self._run(
                transport="mister",
                host=value if keyword == "host" else "mister.test",
                user=value if keyword == "user" else "root",
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(keyword, (result.stdout + result.stderr).lower())
        self.assertFalse(marker.exists())

    def test_mister_rejects_non_arm_or_missing_fifo(self) -> None:
        self._write_remote_tools(architecture="x86_64")
        result = self._run(transport="mister", host="mister.test", user="root")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("architecture", (result.stdout + result.stderr).lower())
        self._write_remote_tools(architecture="aarch64", fifo=False)
        result = self._run(transport="mister", host="mister.test", user="root")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("FIFO", result.stdout + result.stderr)

    def test_non_dry_action_requires_exact_operator_board_attestation(self) -> None:
        result = self._run(
            transport="mister",
            dry_run=False,
            host="mister.test",
            user="root",
            expected_board="",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("attestation", (result.stdout + result.stderr).lower())
        self.assertFalse(self.actions.exists())

        result = self._run(
            transport="jtag",
            dry_run=False,
            expected_board="misterpi",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("attestation", (result.stdout + result.stderr).lower())
        self.assertFalse(self.actions.exists())

    def test_mister_uses_unpredictable_private_stage_and_atomic_mkdir(self) -> None:
        first = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertEqual(first.returncode, 0, first.stderr)
        second = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertEqual(second.returncode, 0, second.stderr)
        stage_pattern = r"/tmp/misteross-[0-9a-f]{32}/artifact\.rbf"
        first_stage = self._extract_stage(first.stdout)
        second_stage = self._extract_stage(second.stdout)
        self.assertRegex(first_stage, stage_pattern)
        self.assertRegex(second_stage, stage_pattern)
        self.assertNotEqual(first_stage, second_stage)
        actions = self.actions.read_text()
        self.assertGreaterEqual(actions.count("MISTEROSS_MKDIR_V1"), 2)
        self.assertIn("sha256sum", actions)
        self.assertIn("SCP", actions)

    def test_mister_uses_one_ephemeral_control_path_and_cleans_it(self) -> None:
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertEqual(result.returncode, 0, result.stderr)
        output = result.stdout + result.stderr
        control_paths = set(re.findall(r"ControlPath=([^\s]+)", output))
        self.assertEqual(len(control_paths), 1, output)
        control_path = next(iter(control_paths))
        self.assertIn("%C", control_path)
        self.assertFalse(Path(control_path.split("%", 1)[0]).parent.exists())
        actions = self.actions.read_text()
        self.assertGreaterEqual(actions.count("ControlPath="), 5)
        self.assertIn("ControlMaster=auto", actions)
        self.assertIn("ControlPersist=", actions)

    def test_mister_control_path_is_cleaned_after_failure(self) -> None:
        self._write_remote_tools(mkdir_exit=1)
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertNotEqual(result.returncode, 0)
        output = result.stdout + result.stderr
        control_paths = set(re.findall(r"ControlPath=([^\s]+)", output))
        self.assertEqual(len(control_paths), 1, output)
        control_path = next(iter(control_paths))
        self.assertFalse(Path(control_path.split("%", 1)[0]).parent.exists())

    def test_mister_control_exit_failure_uses_bounded_stop_fallback(self) -> None:
        self._write_remote_tools(control_exit=7, control_stop_exit=0)
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertEqual(result.returncode, 0, result.stderr)
        actions = self.actions.read_text()
        self.assertIn("-O exit", actions)
        self.assertIn("-O stop", actions)
        output = result.stdout + result.stderr
        control_paths = set(re.findall(r"ControlPath=([^\s]+)", output))
        self.assertEqual(len(control_paths), 1, output)
        control_path = next(iter(control_paths))
        self.assertFalse(Path(control_path.split("%", 1)[0]).parent.exists())

    def test_mister_control_shutdown_failure_surfaces_and_preserves_path(self) -> None:
        self._write_remote_tools(control_exit=7, control_stop_exit=8)
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("control", (result.stdout + result.stderr).lower())
        output = result.stdout + result.stderr
        control_paths = set(re.findall(r"ControlPath=([^\s]+)", output))
        self.assertEqual(len(control_paths), 1, output)
        control_path = next(iter(control_paths))
        control_dir = Path(control_path.split("%", 1)[0]).parent
        self.assertTrue(control_dir.exists())
        shutil.rmtree(control_dir, ignore_errors=True)

    @staticmethod
    def _extract_stage(output: str) -> str:
        for line in output.splitlines():
            if line.startswith("remote_stage: "):
                return line.split(": ", 1)[1]
        return ""

    def test_mister_atomic_mkdir_failure_stops_for_dangling_symlink_or_race(self) -> None:
        self._write_remote_tools(mkdir_exit=1)
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("private", (result.stdout + result.stderr).lower())
        actions = self.actions.read_text()
        self.assertIn("MISTEROSS_MKDIR_V1", actions)
        self.assertNotIn("SCP", actions)
        self.assertNotIn("LOAD", actions)

    def test_mister_rejects_remote_stage_symlink_or_bad_metadata_before_fifo(self) -> None:
        self._write_remote_tools(stage_dir_type="symbolic link")
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("stage", (result.stdout + result.stderr).lower())
        self.assertNotIn("LOAD", self.actions.read_text())

    def test_remote_verify_script_hashes_without_newline_splitting_and_rejects_symlink(self) -> None:
        with tempfile.TemporaryDirectory(prefix="misteross-verify-") as directory:
            stage_dir = Path(directory)
            stage_file = stage_dir / "artifact.rbf"
            content = b"line one\nline two\n"
            stage_file.write_bytes(content)
            stage_file.chmod(0o664)
            script_source = (
                "from program import _remote_verify_script; "
                f"print(_remote_verify_script({str(stage_dir)!r}, {str(stage_file)!r}))"
            )
            env = os.environ.copy()
            env["PYTHONPATH"] = str(PROGRAM.parent)
            generated = subprocess.run(
                [sys.executable, "-c", script_source],
                env=env,
                text=True,
                capture_output=True,
                check=False,
            )
            self.assertEqual(generated.returncode, 0, generated.stderr)
            executed = subprocess.run(
                ["/bin/sh", "-c", generated.stdout],
                text=True,
                capture_output=True,
                check=False,
            )
            self.assertEqual(executed.returncode, 0, executed.stderr)
            lines = executed.stdout.splitlines()
            expected_hash = hashlib.sha256(content).hexdigest()
            self.assertEqual(
                [line for line in lines if line.startswith("HASH|")],
                [f"HASH|{expected_hash}|{stage_file}"],
            )
            self.assertEqual(
                len([line for line in lines if line.startswith("HASH|")]),
                1,
            )
            file_lines = [line for line in lines if line.startswith("FILE|")]
            self.assertEqual(len(file_lines), 1)
            self.assertIn("|400|", file_lines[0])
            self.assertEqual(stat.S_IMODE(stage_file.stat().st_mode), 0o400)

            outside = stage_dir / "outside.rbf"
            outside.write_bytes(content)
            stage_file.unlink()
            stage_file.symlink_to(outside)
            rejected = subprocess.run(
                ["/bin/sh", "-c", generated.stdout],
                text=True,
                capture_output=True,
                check=False,
            )
            self.assertNotEqual(rejected.returncode, 0)
            self.assertNotIn("HASH|", rejected.stdout)

    def test_mister_binds_fifo_to_one_root_main_pid_and_matching_inode(self) -> None:
        for options, expected in (
            ({"main_pid_count": 2}, "exactly one"),
            ({"main_root_owned": False}, "root"),
            ({"main_fifo_inode": False}, "FIFO"),
            ({"fifo_root_owned": False}, "FIFO"),
        ):
            self._write_remote_tools(**options)
            result = self._run(transport="mister", host="mister.test", user="root")
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(expected.lower(), (result.stdout + result.stderr).lower())
            self.assertNotIn("SCP", self.actions.read_text() if self.actions.exists() else "")

    def test_mister_requires_expected_main_digest_for_non_dry_and_reports_dry_hash(self) -> None:
        result = self._run(
            transport="mister",
            dry_run=False,
            host="mister.test",
            user="root",
            expected_main_sha256=None,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Main", result.stdout + result.stderr)
        self.assertFalse(self.actions.exists())

        result = self._run(
            transport="mister",
            host="mister.test",
            user="root",
            expected_main_sha256="b" * 64,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("discovered", (result.stdout + result.stderr).lower())
        self.assertIn("not authenticated", (result.stdout + result.stderr).lower())

    def test_mister_fifo_result_is_outcome_unverified_not_load_complete(self) -> None:
        result = self._run(transport="mister", dry_run=False, host="mister.test", user="root")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("load request dispatched; outcome unverified", result.stdout)
        self.assertNotIn("load complete", result.stdout.lower())

    def test_jtag_single_usb_blaster_uses_interface_without_cable_index(self) -> None:
        result = self._run(args=("--cable", "usb-blasterII"))
        self.assertEqual(result.returncode, 0, result.stderr)
        action_lines = self.actions.read_text().splitlines()
        self.assertTrue(any("--cable usb-blasterII" in line for line in action_lines))
        self.assertFalse(any("--cable-index" in line for line in action_lines))
        self.assertNotIn("physical index", result.stdout + result.stderr)

    def test_jtag_usb_blaster_rejects_cable_index_even_for_single_probe(self) -> None:
        result = self._run(args=("--cable-index", "0"))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("cable-index", (result.stdout + result.stderr).lower())
        self.assertNotIn("PROGRAM", self.actions.read_text() if self.actions.exists() else "")

    def test_jtag_scan_bus_and_device_columns_are_not_cable_index(self) -> None:
        # openFPGALoader's scan output starts with USB bus and device address;
        # those columns are not a selector for the pinned USB-Blaster II path.
        self._write_programmer(scan="001 002 0x09fb:0x6810 usb-blasterII\n")
        result = self._run()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("--cable usb-blasterII", self.actions.read_text())
        self.assertNotIn("--cable-index", self.actions.read_text())

    def test_jtag_snapshot_survives_source_replacement_and_is_cleaned(self) -> None:
        original = self.artifact.read_bytes()
        programmed = self.fixture / "programmed.rbf"
        self._write_programmer(
            mutate_artifact_on_scan=True,
            capture_programmed_bytes=programmed,
        )
        result = self._run(dry_run=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(programmed.read_bytes(), original)
        self.assertNotEqual(self.artifact.read_bytes(), original)
        snapshot_lines = [line for line in result.stdout.splitlines() if line.startswith("local_snapshot: ")]
        self.assertEqual(len(snapshot_lines), 1)
        snapshot = Path(snapshot_lines[0].split(": ", 1)[1])
        self.assertFalse(snapshot.exists())
        self.assertIn("outcome unverified", result.stdout)

    def test_oss_manifest_requires_exact_resource_classes_and_forbidden_entries(self) -> None:
        cases = (
            ("resource_classes", {"MISTRAL_BUF": "ordinary"}, "resource"),
            ("resources", {"MISTRAL_BUF": {"used": 0, "available": 1}, "arbitrary": {"used": 0, "available": 1}}, "resource"),
            ("hard_blocks", {"MISTRAL_M10K": {"used": 0, "available": 553}}, "hard"),
        )
        for key, value, marker in cases:
            manifest = json.loads(self.manifest.read_text(encoding="utf-8"))
            manifest["build"][key] = value
            self.manifest.write_text(json.dumps(manifest), encoding="utf-8")
            result = self._run()
            self.assertNotEqual(result.returncode, 0)
            self.assertIn(marker, (result.stdout + result.stderr).lower())
            self._write_manifest()

    def test_timing_rejects_nonfinite_values_and_requires_exact_request(self) -> None:
        for path, value in (("requested_mhz", float("inf")), ("achieved_mhz", float("nan"))):
            self._mutate_build(("timing", path), value)
            result = self._run()
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("finite", (result.stdout + result.stderr).lower())
            self._write_manifest()

        self._mutate_build(("timing", "requested_mhz"), 50.1)
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("50.0", result.stdout + result.stderr)

    def test_oracle_unmeasured_stability_requires_explicit_provenance_reason(self) -> None:
        self._write_oracle_manifest(stability_measured=False)
        result = self._run(lane="oracle")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("unmeasured", (result.stdout + result.stderr).lower())

        manifest_path = ROOT / "build" / "oracle" / EXPERIMENT / "manifest.json"
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        manifest["build"]["reproducibility"].pop("rbf_stability_reason")
        manifest_path.write_text(json.dumps(manifest), encoding="utf-8")
        result = self._run(lane="oracle")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("reason", (result.stdout + result.stderr).lower())

        self._write_manifest()
        self._mutate_build(("reproducibility", "rbf_stability_measured"), False)
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("stability", (result.stdout + result.stderr).lower())

    def test_make_program_does_not_execute_malicious_python_or_host_values(self) -> None:
        marker = self.fixture / "make-injected"
        env = self._env(transport="mister", dry_run=True, host=f"$(touch {marker})", user="root")
        env["PYTHON"] = f"python3; touch {marker}"
        result = subprocess.run(
            [
                "make",
                "--no-print-directory",
                "program",
                f"EXP={EXPERIMENT}",
                "BUILD=oss",
            ],
            cwd=ROOT,
            env=env,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("host", (result.stdout + result.stderr).lower())
        self.assertFalse(marker.exists())

        env = self._env(transport="mister", dry_run=True, host="mister.test", user=f"'; touch {marker}")
        env["PYTHON"] = f"python3; touch {marker}"
        result = subprocess.run(
            [
                "make",
                "--no-print-directory",
                "program",
                f"EXP={EXPERIMENT}",
                "BUILD=oss",
            ],
            cwd=ROOT,
            env=env,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("user", (result.stdout + result.stderr).lower())
        self.assertFalse(marker.exists())

    def test_make_plain_and_help_do_not_expand_operator_variables(self) -> None:
        for variable in ("EXP", "BUILD", "PYTHON", "PROGRAM_TRANSPORT"):
            with self.subTest(variable=variable):
                marker = self.fixture / f"make-{variable.lower()}-injected"
                env = os.environ.copy()
                env[variable] = f'x$(touch {marker})"; touch {marker}; echo "'
                for target in (None, "help"):
                    command = ["make", "--no-print-directory"]
                    if target is not None:
                        command.append(target)
                    result = subprocess.run(
                        command,
                        cwd=ROOT,
                        env=env,
                        text=True,
                        capture_output=True,
                        check=False,
                    )
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertFalse(marker.exists(), result.stdout + result.stderr)
                self.assertIn("Open MiSTer OSS Cyclone V toolchain", result.stdout)

    def test_program_dry_run_requires_exact_boolean_environment_values(self) -> None:
        invalid_values = ("tru", " true ", "maybe", "1 ", "yes\n")
        command = [
            str(PROGRAM),
            "--repo-root",
            str(ROOT),
            "--experiment",
            EXPERIMENT,
            "--build",
            LANE,
        ]
        for value in invalid_values:
            with self.subTest(value=repr(value)):
                env = self._env(transport="mister", dry_run=True, host="mister.test", user="root")
                env["PROGRAM_DRY_RUN"] = value
                result = subprocess.run(
                    command,
                    cwd=ROOT,
                    env=env,
                    text=True,
                    capture_output=True,
                    check=False,
                )
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(
                    len((result.stdout + result.stderr).splitlines()),
                    1,
                    result.stdout + result.stderr,
                )
                self.assertIn("PROGRAM_DRY_RUN", result.stdout + result.stderr)
                self.assertFalse(self.actions.exists())

    def test_program_dry_run_exact_false_allows_live_fake_transport(self) -> None:
        env = self._env(transport="mister", dry_run=True, host="mister.test", user="root")
        env["PROGRAM_DRY_RUN"] = "false"
        command = [
            str(PROGRAM),
            "--repo-root",
            str(ROOT),
            "--experiment",
            EXPERIMENT,
            "--build",
            LANE,
        ]
        result = subprocess.run(
            command,
            cwd=ROOT,
            env=env,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        actions = self.actions.read_text()
        self.assertIn("SCP", actions)
        self.assertIn("LOAD", actions)

    def test_program_dry_run_unset_defaults_to_safe_read_only_mode(self) -> None:
        env = self._env(transport="mister", dry_run=True, host="mister.test", user="root")
        env.pop("PROGRAM_DRY_RUN")
        command = [
            str(PROGRAM),
            "--repo-root",
            str(ROOT),
            "--experiment",
            EXPERIMENT,
            "--build",
            LANE,
        ]
        result = subprocess.run(
            command,
            cwd=ROOT,
            env=env,
            text=True,
            capture_output=True,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("DRY RUN", result.stdout)
        self.assertNotIn("SCP", self.actions.read_text() if self.actions.exists() else "")


if __name__ == "__main__":
    unittest.main()
