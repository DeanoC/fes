from __future__ import annotations

import hashlib
import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

import scripts.doctor as doctor
from scripts import toolchain_cache


ROOT = Path(__file__).resolve().parents[1]
ENV_SH = ROOT / "scripts" / "env.sh"
RUN_SIM = ROOT / "scripts" / "run_sim.sh"
PROGRAM = ROOT / "scripts" / "program.py"
MAKEFILE = ROOT / "Makefile"


class SharedEntrypointTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix="shared-entrypoints-")
        self.fixture = Path(self.temp.name)
        self.cache = self.fixture / "cache"

    def tearDown(self) -> None:
        self.temp.cleanup()

    def _env(self, **overrides: str) -> dict[str, str]:
        environment = os.environ.copy()
        environment["FES_TOOLCHAIN_CACHE_ROOT"] = str(self.cache)
        environment.update(overrides)
        return environment

    def _run(self, command: list[str], *, env: dict[str, str] | None = None) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            command,
            cwd=ROOT,
            env=env or self._env(),
            text=True,
            capture_output=True,
        )

    def test_sourced_env_rejects_shared_cache_before_local_exports(self) -> None:
        result = self._run(
            [
                "bash",
                "-c",
                'source "$1"',
                "shared-entrypoint-test",
                str(ENV_SH),
            ]
        )

        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("shared toolchain cache is unsupported", result.stderr)

    def test_sourced_env_keeps_local_lane_behavior_without_opt_in(self) -> None:
        environment = os.environ.copy()
        environment.pop("FES_TOOLCHAIN_CACHE_ROOT", None)
        result = self._run(
            [
                "bash",
                "-c",
                'source "$1"; printf "%s\\n" "$TOOLCHAIN_INSTALL"',
                "local-entrypoint-test",
                str(ENV_SH),
            ],
            env=environment,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), str(ROOT / "build" / "toolchain" / "install"))

    def test_run_sim_rejects_shared_cache_before_parsing_or_tools(self) -> None:
        result = self._run([str(RUN_SIM), "--help"])

        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("run_sim.sh: shared toolchain cache is unsupported", result.stderr)

    def test_program_rejects_shared_cache_before_loader_selection(self) -> None:
        result = self._run([str(PROGRAM), "--help"])

        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("program: shared toolchain cache is unsupported", result.stderr)

    def test_make_coleco_shared_command_omits_generated_local_root(self) -> None:
        result = self._run(
            ["make", "-n", "toolchain-fes-coleco"],
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("FES_TOOLCHAIN_LOCKFILE=", result.stdout)
        self.assertIn("FES_TOOLCHAIN_GPU_ROUTER=HIP", result.stdout)
        self.assertIn("FES_TOOLCHAIN_HIP_ARCHITECTURES=", result.stdout)
        self.assertNotIn("FES_TOOLCHAIN_ROOT=", result.stdout)

    def test_make_coleco_local_command_keeps_generated_local_root(self) -> None:
        environment = os.environ.copy()
        environment.pop("FES_TOOLCHAIN_CACHE_ROOT", None)
        result = self._run(
            ["make", "-n", "toolchain-fes-coleco"],
            env=environment,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("FES_TOOLCHAIN_ROOT=", result.stdout)

    def test_make_simulation_target_rejects_shared_cache(self) -> None:
        shell = self.fixture / "make-shell"
        shell.write_text(
            "#!/bin/sh\n"
            f"printf '%s\\n' invoked > {str(self.fixture / 'make-invoked')!r}\n"
            "exit 99\n",
            encoding="utf-8",
        )
        shell.chmod(shell.stat().st_mode | stat.S_IXUSR)
        result = self._run(
            ["make", "-s", "sim-fes-coleco-oss", f"SHELL={shell}"]
        )

        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("shared toolchain cache is unsupported for Make simulation targets", result.stderr)
        self.assertFalse((self.fixture / "make-invoked").exists())

    def test_make_shared_toolchain_clears_make_flags_and_does_not_export_default_python(self) -> None:
        shell = self.fixture / "make-shell"
        log = self.fixture / "make-shell.log"
        shell.write_text(
            "#!/bin/sh\n"
            f"printf 'PYTHON=%s\\nMAKEFLAGS=%s\\nMFLAGS=%s\\nCMD=%s\\n' "
            f"\"${{PYTHON-<unset>}}\" \"${{MAKEFLAGS-<unset>}}\" "
            f"\"${{MFLAGS-<unset>}}\" \"$2\" > {str(log)!r}\n"
            "exit 0\n",
            encoding="utf-8",
        )
        shell.chmod(shell.stat().st_mode | stat.S_IXUSR)
        environment = self._env()
        environment.pop("PYTHON", None)
        result = self._run(
            [
                "make",
                "-s",
                "-j8",
                "-C",
                str(ROOT),
                "toolchain-fes-coleco",
                f"SHELL={shell}",
            ],
            env=environment,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        record = log.read_text(encoding="utf-8")
        self.assertIn("PYTHON=<unset>", record)
        self.assertIn("CMD=MAKEFLAGS= MFLAGS=", record)

    def test_make_cache_root_clears_make_flags_and_forwards_cache_root(self) -> None:
        cache = "/x"
        environment = os.environ.copy()
        environment.pop("FES_TOOLCHAIN_CACHE_ROOT", None)
        environment.pop("CACHE_ROOT", None)
        environment.pop("PYTHON", None)
        for target, script in (
            ("build-fes-pong", "scripts/build_fes_pong.py"),
            ("build-fes-zx81", "scripts/build_fes_zx81_oss.py"),
            ("build-fes-coleco", "scripts/build_fes_coleco_oss.py"),
        ):
            with self.subTest(target=target):
                shell = self.fixture / f"make-shell-{target}"
                log = self.fixture / f"make-shell-{target}.log"
                shell.write_text(
                    "#!/bin/sh\n"
                    f"printf 'MAKEFLAGS=%s\\nMFLAGS=%s\\nCMD=%s\\n' "
                    f"\"${{MAKEFLAGS-<unset>}}\" \"${{MFLAGS-<unset>}}\" \"$2\" > {str(log)!r}\n"
                    "exit 0\n",
                    encoding="utf-8",
                )
                shell.chmod(shell.stat().st_mode | stat.S_IXUSR)
                result = self._run(
                    [
                        "make",
                        "-s",
                        "-j2",
                        "-C",
                        str(ROOT),
                        target,
                        f"CACHE_ROOT={cache}",
                        f"SHELL={shell}",
                    ],
                    env=environment,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                record = log.read_text(encoding="utf-8")
                self.assertIn(
                    f'CMD=MAKEFLAGS= MFLAGS= python3 {script} --root "{ROOT}" --cache-root "{cache}"',
                    record,
                )

    def test_make_rejects_explicit_python_override_before_bootstrap(self) -> None:
        environment = self._env(PYTHON="/opt/x")
        result = self._run(
            ["make", "-s", "toolchain-fes-coleco"],
            env=environment,
        )

        self.assertNotEqual(result.returncode, 0)
        self.assertIn("shared cache requires Python: /opt/x", result.stderr)

    def test_make_doctor_shared_mode_skips_legacy_env_source(self) -> None:
        fake_python = self.fixture / "python3"
        marker = self.fixture / "doctor-invoked"
        fake_python.write_text(
            "#!/bin/sh\n"
            f"touch {str(marker)!r}\n"
            "exit 0\n",
            encoding="utf-8",
        )
        fake_python.chmod(fake_python.stat().st_mode | stat.S_IXUSR)
        environment = self._env(PATH=f"{self.fixture}:{os.environ.get('PATH', '')}")
        environment.pop("PYTHON", None)
        result = self._run(["make", "-s", "doctor"], env=environment)

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(marker.exists())

    def test_doctor_check_tool_uses_verified_manifest_path_without_path_fallback(self) -> None:
        install = self.fixture / "install"
        binary = install / "bin" / "verilator"
        binary.parent.mkdir(parents=True)
        binary.write_text("#!/bin/sh\nprintf 'shared-verilator\\n'\n", encoding="utf-8")
        binary.chmod(binary.stat().st_mode | stat.S_IXUSR)
        digest = hashlib.sha256(binary.read_bytes()).hexdigest()
        pin = doctor._load_pins()["verilator"]
        manifest = SimpleNamespace(
            key="shared-key",
            install=install,
            evidence=self.fixture / "evidence",
            tools={
                "verilator": {
                    "binary": "bin/verilator",
                    "path": "install/bin/verilator",
                    "commit": pin.commit,
                    "identity": "verilator shared fixture",
                    "sha256": digest,
                }
            },
        )
        decoy = self.fixture / "decoy-verilator"
        marker = self.fixture / "decoy-used"
        decoy.write_text(f"#!/bin/sh\ntouch '{marker}'\n", encoding="utf-8")
        decoy.chmod(decoy.stat().st_mode | stat.S_IXUSR)
        environment = self._env(
            PATH=f"{self.fixture}:{os.environ.get('PATH', '')}",
            FES_TOOLCHAIN_LOCKFILE=str(ROOT / "toolchain.lock"),
        )
        with mock.patch.object(
            doctor.toolchain_cache,
            "request_from_environment",
            return_value=SimpleNamespace(),
        ) as request_from_environment, mock.patch.object(
            doctor.toolchain_cache, "verify_ready", return_value=manifest
        ) as verify_ready, mock.patch.dict(os.environ, environment, clear=True):
            check = doctor.check_tool("verilator")

        self.assertEqual(check["status"], "OK", check)
        self.assertIn("shared-key", check["detail"])
        self.assertIn(str(binary), check["detail"])
        self.assertFalse(marker.exists())
        request_from_environment.assert_called_once()
        verify_ready.assert_called_once()

    def test_doctor_shared_manifest_failure_does_not_fall_back_to_local(self) -> None:
        marker = self.fixture / "decoy-used"
        decoy = self.fixture / "verilator"
        decoy.write_text(f"#!/bin/sh\ntouch '{marker}'\n", encoding="utf-8")
        decoy.chmod(decoy.stat().st_mode | stat.S_IXUSR)
        environment = self._env(PATH=f"{self.fixture}:{os.environ.get('PATH', '')}")
        with mock.patch.object(
            doctor.toolchain_cache,
            "request_from_environment",
            side_effect=toolchain_cache.CacheError("ready manifest is missing"),
        ), mock.patch.dict(os.environ, environment, clear=True):
            check = doctor.check_tool("verilator")

        self.assertEqual(check["status"], "NOT READY", check)
        self.assertIn("shared toolchain cache", check["detail"])
        self.assertFalse(marker.exists())


if __name__ == "__main__":
    unittest.main()
