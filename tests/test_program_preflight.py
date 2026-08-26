from __future__ import annotations

import hashlib
import json
import os
import shutil
import stat
import subprocess
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
        self.temp.cleanup()

    def _write_manifest(self, **overrides: object) -> None:
        digest = _sha256(self.artifact)
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
            "hard_block_status": "pass",
            "hard_blocks": {
                "MISTRAL_M10K": {"used": 0, "available": 553},
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

    def _write_programmer(self, *, scan: str | None = None, detect: str | None = None, exit_code: int = 0) -> None:
        scan = scan if scan is not None else "Bus 1 0x09fb:0x6810 usb-blasterII de10nano\n"
        detect = detect if detect is not None else "IDCODE: 0x02d020dd\n"
        self._write_executable(
            self.programmer,
            ""
            f"printf '%s\\n' \"$*\" >> {str(self.actions)!r}\n"
            "case \" $* \" in\n"
            f"  *'--scan-usb'*) printf '%b' {scan!r} ;;\n"
            f"  *'--detect'*) printf '%b' {detect!r} ;;\n"
            f"  *'--write-sram'*) printf '%s\\n' PROGRAM >> {str(self.actions)!r}; exit {exit_code} ;;\n"
            "esac\n",
        )

    def _write_remote_tools(
        self,
        *,
        digest: str | None = None,
        architecture: str = "aarch64",
        fifo: bool = True,
        main: bool = True,
        scp_exit: int = 0,
        load_exit: int = 0,
        stage_exists: bool = False,
    ) -> None:
        digest = digest or _sha256(self.artifact)
        stage = f"/tmp/misteross-{EXPERIMENT}-{digest[:16]}.rbf"
        fifo_result = "" if fifo else "exit 1\n"
        main_output = "Main_MiSTer\n" if main else "other-process\n"
        stage_result = "exit 1" if stage_exists else "exit 0"
        self._write_executable(
            self.ssh,
            ""
            f"printf '%s\\n' \"$*\" >> {str(self.actions)!r}\n"
            "case \" $* \" in\n"
            f"  *'uname -m'*) printf '%s' {architecture!r} ;;\n"
            f"  *'test -p /dev/MiSTer_cmd'*) {fifo_result} ;;\n"
            "  *'test -d /tmp'*) exit 0 ;;\n"
            f"  *'ps -e -o comm='*) printf '%b' {main_output!r} ;;\n"
            f"  *'test ! -e {stage}'*) {stage_result} ;;\n"
            f"  *'sha256sum'*) printf '%s  %s\\n' {digest!r} {stage!r} ;;\n"
            "  *'load_core'*) printf '%s\\n' LOAD >> "
            f"{str(self.actions)!r}; exit {load_exit} ;;\n"
            "  *) exit 0 ;;\n"
            "esac\n",
        )
        self._write_executable(
            self.scp,
            f"printf '%s\\n' \"$*\" >> {str(self.actions)!r}\nprintf '%s\\n' SCP >> {str(self.actions)!r}\nexit {scp_exit}\n",
        )

    def _env(self, *, transport: str = "jtag", dry_run: bool = True, host: str | None = None, user: str | None = None) -> dict[str, str]:
        env = os.environ.copy()
        env.update(
            {
                "PROGRAMMER": str(self.programmer),
                "PROGRAM_TRANSPORT": transport,
                "PROGRAM_DRY_RUN": "1" if dry_run else "0",
                "PATH": os.pathsep.join((str(self.fixture), env.get("PATH", ""))),
            }
        )
        if host is not None:
            env["MISTER_HOST"] = host
        if user is not None:
            env["MISTER_USER"] = user
        return env

    def _run(self, *, transport: str = "jtag", dry_run: bool = True, args: tuple[str, ...] = (), host: str | None = None, user: str | None = None) -> subprocess.CompletedProcess[str]:
        command = [
            str(PROGRAM),
            "--repo-root",
            str(ROOT),
            "--experiment",
            EXPERIMENT,
            "--build",
            LANE,
            *args,
        ]
        return subprocess.run(
            command,
            cwd=ROOT,
            env=self._env(transport=transport, dry_run=dry_run, host=host, user=user),
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

    def test_two_jtag_boards_require_exact_cable_selection(self) -> None:
        self._write_programmer(
            scan=(
                "Bus 1 0x09fb:0x6810 usb-blasterII de10nano\n"
                "Bus 2 0x09fb:0x6810 usb-blasterII_2 de10nano\n"
            )
        )
        result = self._run()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("ambiguous", (result.stdout + result.stderr).lower())
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
        self.assertNotIn("sha256sum", actions)
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
        self._write_remote_tools(stage_exists=True)
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


if __name__ == "__main__":
    unittest.main()
