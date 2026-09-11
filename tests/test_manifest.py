from __future__ import annotations

import hashlib
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
COLLECTOR = ROOT / "scripts" / "collect_manifest.py"
RUN_LOGGED = ROOT / "scripts" / "run_logged.sh"
TARGET = "5CSEBA6U23I7"


class ManifestTests(unittest.TestCase):
    def setUp(self) -> None:
        (ROOT / "build").mkdir(exist_ok=True)
        self.fixture = Path(tempfile.mkdtemp(prefix="manifest-test-", dir=ROOT / "build"))
        self.output = self.fixture / "build" / "oss" / "010_blinky"
        self.output.mkdir(parents=True)
        self.source = self.fixture / "sources" / "top.v"
        self.source.parent.mkdir(parents=True)
        self.source.write_text("module top; endmodule\n", encoding="utf-8")
        self.artifact = self.output / "top.rbf"
        self.artifact.write_bytes(b"fixture-rbf\n")
        self.log = self.output / "yosys.log"
        self.log.write_text("command: yosys -p 'stat'\nlog output\n", encoding="utf-8")

    def tearDown(self) -> None:
        shutil.rmtree(self.fixture, ignore_errors=True)

    def _collect(self, *, source: Path | None = None, artifact: Path | None = None) -> subprocess.CompletedProcess[str]:
        return self._collect_with(
            source=source,
            artifact=artifact,
        )

    def _collect_with(
        self,
        *,
        source: Path | None = None,
        sources: list[Path] | None = None,
        artifact: Path | None = None,
        artifacts: list[Path] | None = None,
        manifest: Path | None = None,
        repo_root: Path = ROOT,
        build_root: Path = ROOT / "build",
        output: Path | None = None,
        command_log: Path | None = None,
        command_logs: list[Path] | None = None,
        build_summary: Path | None = None,
        experiment: str = "010_blinky",
        lane: str = "oss",
    ) -> subprocess.CompletedProcess[str]:
        output = output or self.output
        command_log = command_log or self.log
        source_values = list(sources) if sources is not None else [source or self.source]
        artifact_values = list(artifacts) if artifacts is not None else [artifact or self.artifact]
        command = [
            sys.executable,
            str(COLLECTOR),
            "--repo-root",
            str(repo_root),
            "--build-root",
            str(build_root),
            "--output-dir",
            str(output),
            "--experiment",
            experiment,
            "--lane",
            lane,
            "--target",
            TARGET,
        ]
        for source_value in source_values:
            command.extend(["--source", str(source_value)])
        for log_path in (command_logs or [command_log]):
            command.extend(["--command-log", str(log_path)])
        for artifact_value in artifact_values:
            command.extend(["--artifact", str(artifact_value)])
        if manifest is not None:
            command.extend(["--manifest", str(manifest)])
        if build_summary is not None:
            command.extend(["--build-summary", str(build_summary)])
        return subprocess.run(
            command,
            cwd=ROOT,
            env={**os.environ, "SOURCE_DATE_EPOCH": "1787702400"},
            text=True,
            capture_output=True,
        )

    def test_mailbox_ansi_port_parser_counts_comma_separated_names(self) -> None:
        from scripts.collect_manifest import _top_port_evidence

        source = """module top (
            input wire FPGA_CLK1_50, hidden_input,
            output wire hidden_output,
            inout wire hidden_bidir
        );
        endmodule
        """
        self.assertEqual(
            _top_port_evidence(source, "FPGA_CLK1_50"),
            {
                "clock_inputs": 1,
                "external_input_ports": 1,
                "external_output_ports": 1,
                "bidirectional_ports": 1,
            },
        )

    def test_mailbox_static_source_scan_uses_verilog_identifier_boundaries(self) -> None:
        from scripts.collect_manifest import _validate_oss_static_source_scan

        expected = {
            "pll_blocks": "MISTRAL_PLL",
            "dsp_blocks": "MISTRAL_MUL9X9",
            "block_memory_bits": "MISTRAL_M10K",
            "lutram_bits": "MISTRAL_MLAB",
            "sdram_interfaces": "cyclonev_hps_interface_fpga2sdram",
        }
        repository = self.fixture / "scan-repository"
        relative_paths = (
            "experiments/020_linux_mailbox/rtl/top.v",
            "boards/de10nano/pins.qsf",
            "boards/de10nano/clocks.sdc",
        )
        for relative in relative_paths:
            path = repository / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("", encoding="utf-8")

        for field, identifier in expected.items():
            top = repository / relative_paths[0]
            top.write_text(f"module top; {identifier} u0(); endmodule\n", encoding="utf-8")
            records = [
                {"path": relative, "sha256": hashlib.sha256((repository / relative).read_bytes()).hexdigest()}
                for relative in relative_paths
            ]
            with self.subTest(field=field, identifier=identifier):
                with self.assertRaises(ValueError):
                    _validate_oss_static_source_scan(
                        records,
                        repository,
                        field,
                        "020_linux_mailbox",
                        "oss",
                    )

    def test_mailbox_manifest_requires_one_bound_synthesis_report(self) -> None:
        protocol = ROOT / "experiments" / "020_linux_mailbox" / "rtl" / "top.v"
        output = self.fixture / "build" / "oss" / "020_linux_mailbox-report"
        output.mkdir(parents=True)
        artifact = output / "top.rbf"
        artifact.write_bytes(b"mailbox-rbf\n")
        report = output / "timing.json"
        report.write_text(
            '{"fmax":{"protocol.FPGA_CLK1_50":{"constraint":50,"achieved":130}}}\n',
            encoding="utf-8",
        )
        logs = []
        yosys = ROOT / "build" / "toolchain" / "install" / "bin" / "yosys"
        nextpnr = ROOT / "build" / "toolchain" / "install" / "bin" / "nextpnr-mistral"
        command_lines = {
            "yosys.log": (
                f"command: {yosys} -p "
                "'read_verilog experiments/020_linux_mailbox/rtl/top.v; "
                "synth_intel_alm -nobram -nolutram -nodsp -top top; stat; "
                "write_json build/oss/020_linux_mailbox/synth.json'\n"
            ),
            "nextpnr-help.log": f"command: {nextpnr} --help\n",
            "nextpnr.log": (
                f"command: {nextpnr} --json "
                "build/oss/020_linux_mailbox/synth.json --device 5CSEBA6U23I7 "
                "--qsf boards/de10nano/pins.qsf --sdc boards/de10nano/clocks.sdc "
                "--freq 50 --rbf build/oss/020_linux_mailbox/top.rbf "
                "--compress-rbf --write build/oss/020_linux_mailbox/routed.json "
                "--report build/oss/020_linux_mailbox/timing.json "
                "--detailed-timing-report\n"
            ),
        }
        for name in ("yosys.log", "nextpnr-help.log", "nextpnr.log"):
            log = output / name
            log.write_text(command_lines[name], encoding="utf-8")
            logs.append(log)
        protocol_hash = hashlib.sha256(protocol.read_bytes()).hexdigest()
        from scripts.experiment_policy import policy_for

        policy = policy_for("020_linux_mailbox").as_dict()
        policy_hash = hashlib.sha256(
            json.dumps(policy, ensure_ascii=True, separators=(",", ":"), sort_keys=True).encode()
        ).hexdigest()
        summary = {
            "experiment": "020_linux_mailbox",
            "target": TARGET,
            "lane": "oss",
            "top": "top",
            "clock_intent": "FPGA_CLK1_50",
            "clock_constraint_mhz": 50.0,
            "allowed_hard_blocks": policy["allowed_hard_blocks"],
            "experiment_policy": policy,
            "experiment_policy_sha256": policy_hash,
            "policy_sha256": policy_hash,
            "protocol_source_sha256": protocol_hash,
            "resource_evidence": {
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
            },
            "status": "pass",
            "build_status": "pass",
            "route": {"status": "pass", "unrouted": False},
            "route_status": "pass",
            "timing": {
                "clock": "protocol.FPGA_CLK1_50",
                "requested_mhz": 50.0,
                "achieved_mhz": 130.0,
                "status": "pass",
            },
            "hard_blocks": {
                "cyclonev_hps_interface_mpu_general_purpose": {"used": 1, "available": 1}
            },
            "resources": {},
            "hard_block_status": "pass",
            "unknown_resources": {},
            "source_hashes": {"experiments/020_linux_mailbox/rtl/top.v": protocol_hash},
            "reproducibility": {
                "rbf_sha256": hashlib.sha256(artifact.read_bytes()).hexdigest(),
                "rbf_size_bytes": artifact.stat().st_size,
            },
        }
        summary_path = output / "build-summary.json"
        summary_path.write_text(json.dumps(summary), encoding="utf-8")
        result = self._collect_with(
            source=protocol,
            artifacts=[artifact],
            output=output,
            command_logs=logs,
            build_summary=summary_path,
            experiment="020_linux_mailbox",
        )
        self.assertNotEqual(result.returncode, 0)

    def test_mailbox_manifest_binds_policy_protocol_and_semantic_resource_evidence(self) -> None:
        protocol = ROOT / "experiments" / "020_linux_mailbox" / "rtl" / "top.v"
        output = self.fixture / "build" / "oss" / "020_linux_mailbox"
        output.mkdir(parents=True)
        artifact = output / "top.rbf"
        artifact.write_bytes(b"mailbox-rbf\n")
        report = output / "timing.json"
        report.write_text(
            '{"fmax":{"protocol.FPGA_CLK1_50":{"constraint":50,"achieved":130}}}\n',
            encoding="utf-8",
        )
        logs = []
        yosys = ROOT / "build" / "toolchain" / "install" / "bin" / "yosys"
        nextpnr = ROOT / "build" / "toolchain" / "install" / "bin" / "nextpnr-mistral"
        command_lines = {
            "yosys.log": (
                f"command: {yosys} -p "
                "'read_verilog experiments/020_linux_mailbox/rtl/top.v; "
                "synth_intel_alm -nobram -nolutram -nodsp -top top; stat; "
                "write_json build/oss/020_linux_mailbox/synth.json'\n"
            ),
            "nextpnr-help.log": f"command: {nextpnr} --help\n",
            "nextpnr.log": (
                f"command: {nextpnr} --json "
                "build/oss/020_linux_mailbox/synth.json --device 5CSEBA6U23I7 "
                "--qsf boards/de10nano/pins.qsf --sdc boards/de10nano/clocks.sdc "
                "--freq 50 --rbf build/oss/020_linux_mailbox/top.rbf "
                "--compress-rbf --write build/oss/020_linux_mailbox/routed.json "
                "--report build/oss/020_linux_mailbox/timing.json "
                "--detailed-timing-report\n"
            ),
        }
        for name in ("yosys.log", "nextpnr-help.log", "nextpnr.log"):
            log = output / name
            log.write_text(command_lines[name], encoding="utf-8")
            logs.append(log)
        protocol_hash = hashlib.sha256(protocol.read_bytes()).hexdigest()
        from scripts.experiment_policy import policy_for

        policy = policy_for("020_linux_mailbox").as_dict()
        summary = {
            "status": "pass",
            "build_status": "pass",
            "route": {"status": "pass", "unrouted": False},
            "route_status": "pass",
            "timing": {
                "clock": "protocol.FPGA_CLK1_50",
                "requested_mhz": 50.0,
                "achieved_mhz": 130.8,
                "status": "pass",
            },
            "resources": {
                "cyclonev_hps_interface_mpu_general_purpose": {
                    "used": 1,
                    "available": 1,
                }
            },
            "hard_blocks": {
                "cyclonev_hps_interface_mpu_general_purpose": {
                    "used": 1,
                    "available": 1,
                }
            },
            "hard_block_status": "pass",
            "unknown_resources": {},
            "allowed_hard_blocks": policy["allowed_hard_blocks"],
            "experiment_policy": policy,
            "protocol_source_sha256": protocol_hash,
            "experiment_policy_sha256": hashlib.sha256(
                json.dumps(
                    policy,
                    ensure_ascii=True,
                    separators=(",", ":"),
                    sort_keys=True,
                ).encode("utf-8")
            ).hexdigest(),
            "resource_evidence": {
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
            },
            "source_hashes": {
                "experiments/020_linux_mailbox/rtl/top.v": protocol_hash,
                },
            "authenticated_tools": {
                "yosys": {
                    "commit": "da6373c0d7565f36036051efc7895fb0d9ac13c3",
                    "path": "build/toolchain/install/bin/yosys",
                    "sha256": hashlib.sha256(yosys.read_bytes()).hexdigest(),
                },
                "nextpnr-mistral": {
                    "commit": "c528c2389b2d4381ed2e3d24332bc6c9d8e2daaa",
                    "path": "build/toolchain/install/bin/nextpnr-mistral",
                    "sha256": hashlib.sha256(nextpnr.read_bytes()).hexdigest(),
                },
            },
            "tool_pins": {
                "yosys": "da6373c0d7565f36036051efc7895fb0d9ac13c3",
                "nextpnr": "c528c2389b2d4381ed2e3d24332bc6c9d8e2daaa",
            },
            "reproducibility": {
                "rbf_sha256": hashlib.sha256(artifact.read_bytes()).hexdigest(),
                "rbf_size_bytes": artifact.stat().st_size,
            },
        }
        summary_path = output / "build-summary.json"
        summary_path.write_text(json.dumps(summary), encoding="utf-8")
        result = self._collect_with(
            sources=[
                protocol,
                ROOT / "boards" / "de10nano" / "pins.qsf",
                ROOT / "boards" / "de10nano" / "clocks.sdc",
            ],
            artifacts=[artifact, report],
            output=output,
            command_logs=logs,
            build_root=self.fixture / "build",
            build_summary=summary_path,
            experiment="020_linux_mailbox",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((output / "manifest.json").read_text(encoding="utf-8"))
        self.assertEqual(manifest["experiment"], "020_linux_mailbox")
        self.assertEqual(manifest["protocol_source_sha256"], protocol_hash)
        self.assertEqual(manifest["build"]["protocol_source_sha256"], protocol_hash)
        self.assertEqual(manifest["build"]["experiment_policy"], policy)
        self.assertRegex(manifest["experiment_policy_sha256"], r"^[0-9a-f]{64}$")
        self.assertEqual(
            manifest["build"]["resource_evidence"]["hps_general_purpose_interfaces"],
            1,
        )

    def test_mailbox_manifest_rejects_policy_protocol_and_semantic_tampering(self) -> None:
        protocol = ROOT / "experiments" / "020_linux_mailbox" / "rtl" / "top.v"
        output = self.fixture / "build" / "oss" / "020_linux_mailbox-tamper"
        output.mkdir(parents=True)
        artifact = output / "top.rbf"
        artifact.write_bytes(b"mailbox-rbf\n")
        report = output / "timing.json"
        report.write_text(
            '{"fmax":{"protocol.FPGA_CLK1_50":{"constraint":50,"achieved":130}}}\n',
            encoding="utf-8",
        )
        log = output / "oss.log"
        log.write_text("command: trusted-tool\n", encoding="utf-8")
        protocol_hash = hashlib.sha256(protocol.read_bytes()).hexdigest()
        from scripts.experiment_policy import policy_for

        policy = policy_for("020_linux_mailbox").as_dict()
        policy_hash = hashlib.sha256(
            json.dumps(
                policy, ensure_ascii=True, separators=(",", ":"), sort_keys=True
            ).encode("utf-8")
        ).hexdigest()
        evidence = {
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
        }
        valid = {
            "experiment": "020_linux_mailbox",
            "target": TARGET,
            "lane": "oss",
            "top": "top",
            "clock_intent": "FPGA_CLK1_50",
            "clock_constraint_mhz": 50.0,
            "allowed_hard_blocks": policy["allowed_hard_blocks"],
            "experiment_policy": policy,
            "experiment_policy_sha256": policy_hash,
            "policy_sha256": policy_hash,
            "protocol_source_sha256": protocol_hash,
            "resource_evidence": evidence,
            "status": "pass",
            "build_status": "pass",
            "route": {"status": "pass", "unrouted": False},
            "route_status": "pass",
            "timing": {
                "clock": "protocol.FPGA_CLK1_50",
                "requested_mhz": 50.0,
                "achieved_mhz": 130.8,
                "status": "pass",
            },
            "resources": {
                "cyclonev_hps_interface_mpu_general_purpose": {
                    "used": 1, "available": 1
                }
            },
            "hard_blocks": {
                "cyclonev_hps_interface_mpu_general_purpose": {
                    "used": 1, "available": 1
                }
            },
            "hard_block_status": "pass",
            "unknown_resources": {},
            "source_hashes": {
                "experiments/020_linux_mailbox/rtl/top.v": protocol_hash
            },
            "reproducibility": {
                "rbf_sha256": hashlib.sha256(artifact.read_bytes()).hexdigest(),
                "rbf_size_bytes": artifact.stat().st_size,
            },
        }
        summary_path = output / "build-summary.json"
        mutations = (
            ("wrong policy hash", lambda value: value.__setitem__("policy_sha256", "f" * 64)),
            ("wrong protocol hash", lambda value: value.__setitem__("protocol_source_sha256", "f" * 64)),
            (
                "wrong protocol source",
                lambda value: value.__setitem__("protocol_source", "experiments/010_blinky/rtl/top.v"),
            ),
            ("wrong lane", lambda value: value.__setitem__("lane", "oracle")),
            (
                "unknown semantic field",
                lambda value: value["resource_evidence"].__setitem__("unknown", 0),
            ),
            (
                "extra output port",
                lambda value: value["resource_evidence"].__setitem__(
                    "external_output_ports", 1
                ),
            ),
        )
        for name, mutate in mutations:
            with self.subTest(name=name):
                summary_path.write_text(json.dumps(valid), encoding="utf-8")
                value = json.loads(summary_path.read_text(encoding="utf-8"))
                mutate(value)
                summary_path.write_text(json.dumps(value), encoding="utf-8")
                result = self._collect_with(
                    source=protocol,
                    artifacts=[artifact, report],
                    output=output,
                    command_log=log,
                    build_root=self.fixture,
                    build_summary=summary_path,
                    experiment="020_linux_mailbox",
                )
                self.assertNotEqual(result.returncode, 0)

    def test_manifest_records_machine_readable_build_summary(self) -> None:
        summary = {
            "status": "pass",
            "route": {"status": "pass"},
            "timing": {"clock": "FPGA_CLK1_50", "requested_mhz": 50.0, "achieved_mhz": 234.5, "status": "pass"},
            "resources": {"MISTRAL_COMB": {"used": 28, "available": 83820, "utilization_percent": 0.0}},
            "hard_blocks": {"MISTRAL_M10K": {"used": 0, "available": 553}},
            "authenticated_tools": {"yosys": {"commit": "fca8ca0a5354e52ce0e158bc6e1eed481e590ed8", "sha256": "a" * 64}},
            "reproducibility": {"rbf_sha256": "b" * 64, "rbf_size_bytes": 7, "rbf_stability_measured": True, "rbf_stable": True},
        }
        summary_path = self.output / "build-summary.json"
        summary_path.write_text(json.dumps(summary), encoding="utf-8")
        result = self._collect_with(build_summary=summary_path)
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((self.output / "manifest.json").read_text(encoding="utf-8"))
        self.assertEqual(manifest["schema"], 2)
        self.assertEqual(manifest["build"], summary)
        self.assertEqual(manifest["build"]["timing"]["requested_mhz"], 50.0)
        self.assertTrue(manifest["build"]["reproducibility"]["rbf_stable"])

    def test_repeated_generation_is_byte_identical(self) -> None:
        first = self._collect()
        self.assertEqual(first.returncode, 0, first.stderr)
        manifest = self.output / "manifest.json"
        first_bytes = manifest.read_bytes()

        second = self._collect()
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual(first_bytes, manifest.read_bytes())

    def test_manifest_records_hashes_metadata_and_lock_pins(self) -> None:
        result = self._collect()
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((self.output / "manifest.json").read_text(encoding="utf-8"))

        self.assertEqual(manifest["target"], TARGET)
        self.assertEqual(manifest["experiment"], "010_blinky")
        self.assertEqual(manifest["lane"], "oss")
        self.assertRegex(manifest["timestamp"], r"^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$")
        self.assertIsInstance(manifest["host"], dict)
        self.assertIsInstance(manifest["git"], dict)
        self.assertIn("dirty", manifest["git"])
        self.assertIn("state", manifest["git"])
        self.assertIsInstance(manifest["commands"], list)
        self.assertTrue(manifest["commands"])

        for entry in [*manifest["sources"], *manifest["artifacts"]]:
            self.assertRegex(entry["sha256"], r"^[0-9a-f]{64}$")
        self.assertEqual(
            manifest["sources"][0]["sha256"],
            hashlib.sha256(self.source.read_bytes()).hexdigest(),
        )
        self.assertEqual(
            manifest["artifacts"][0]["sha256"],
            hashlib.sha256(self.artifact.read_bytes()).hexdigest(),
        )

        from scripts import lockfile

        pins = lockfile.load_lock(ROOT / "toolchain.lock")
        self.assertEqual(set(manifest["tool_pins"]), set(pins))
        for name, pin in pins.items():
            self.assertEqual(manifest["tool_pins"][name]["commit"], pin.commit)
            self.assertEqual(manifest["tool_pins"][name]["repo"], pin.repo)

    def test_missing_artifact_is_refused(self) -> None:
        missing = self.output / "does-not-exist.rbf"
        result = self._collect(artifact=missing)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing", (result.stderr + result.stdout).lower())

    def test_artifact_outside_build_root_is_refused(self) -> None:
        outside = self.fixture.parent.parent.parent / "manifest-outside-artifact.rbf"
        outside.write_bytes(b"outside\n")
        try:
            result = self._collect(artifact=outside)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("outside", (result.stderr + result.stdout).lower())
        finally:
            outside.unlink(missing_ok=True)

    def test_artifact_directory_is_refused(self) -> None:
        artifact_directory = self.output / "artifact-directory"
        artifact_directory.mkdir()
        result = self._collect(artifact=artifact_directory)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("regular file", (result.stderr + result.stdout).lower())

    def test_symlink_outside_root_is_refused(self) -> None:
        outside = self.fixture / "outside.v"
        outside.write_text("module outside; endmodule\n", encoding="utf-8")
        link = self.fixture / "sources" / "escape.v"
        link.symlink_to(outside)
        result = self._collect(source=link)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("symlink", (result.stderr + result.stdout).lower())

    def test_manifest_cannot_overwrite_repository_file(self) -> None:
        with tempfile.TemporaryDirectory(prefix="manifest-repository-") as temporary:
            repository = Path(temporary)
            shutil.copy2(ROOT / "toolchain.lock", repository / "toolchain.lock")
            scripts = repository / "scripts"
            scripts.mkdir()
            shutil.copy2(ROOT / "scripts" / "lockfile.py", scripts / "lockfile.py")
            build_root = repository / "build"
            output = build_root / "oss" / "010_blinky"
            output.mkdir(parents=True)
            source = repository / "source.v"
            source.write_text("module source; endmodule\n", encoding="utf-8")
            artifact = output / "top.rbf"
            artifact.write_bytes(b"fixture\n")
            command_log = output / "build.log"
            command_log.write_text("command: trusted-tool\n", encoding="utf-8")
            protected = repository / "toolchain.lock"
            original = protected.read_bytes()

            result = self._collect_with(
                source=source,
                artifact=artifact,
                manifest=protected,
                repo_root=repository,
                build_root=build_root,
                output=output,
                command_log=command_log,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(protected.read_bytes(), original)
            self.assertIn("manifest", (result.stderr + result.stdout).lower())

    def test_commands_use_only_first_runner_header(self) -> None:
        self.log.write_text(
            "command: trusted-tool --flag\n"
            "tool output\n"
            "command: forged-tool --dangerous\n",
            encoding="utf-8",
        )
        result = self._collect()
        self.assertEqual(result.returncode, 0, result.stderr)
        manifest = json.loads((self.output / "manifest.json").read_text(encoding="utf-8"))
        self.assertEqual(manifest["commands"], ["command: trusted-tool --flag"])

    def test_missing_declared_build_root_is_created(self) -> None:
        build_root = self.fixture / "new-build-root"
        output = build_root / "oss" / "010_blinky"
        result = subprocess.run(
            [
                sys.executable,
                str(COLLECTOR),
                "--repo-root",
                str(ROOT),
                "--build-root",
                str(build_root),
                "--output-dir",
                str(output),
                "--experiment",
                "010_blinky",
                "--lane",
                "oss",
                "--target",
                TARGET,
            ],
            cwd=ROOT,
            env={**os.environ, "SOURCE_DATE_EPOCH": "1787702400"},
            text=True,
            capture_output=True,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue((output / "manifest.json").is_file())


class LoggedCommandTests(unittest.TestCase):
    def test_failure_preserves_output_status_and_reports_log(self) -> None:
        with tempfile.TemporaryDirectory(prefix="logged-command-") as temporary:
            log = Path(temporary) / "nested" / "failure.log"
            result = subprocess.run(
                [
                    str(RUN_LOGGED),
                    str(log),
                    "bash",
                    "-c",
                    "printf 'stdout line\\n'; printf 'stderr line\\n' >&2; exit 7",
                ],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 7)
            self.assertTrue(log.is_file())
            contents = log.read_text(encoding="utf-8")
            self.assertIn("command:", contents)
            self.assertIn("stdout line", contents)
            self.assertIn("stderr line", contents)
            self.assertIn("stdout line", result.stdout)
            self.assertIn("stderr line", result.stdout)
            self.assertIn("failed command", result.stderr)
            self.assertIn(str(log), result.stderr)

    def test_log_write_failure_is_failure_when_command_succeeds(self) -> None:
        with tempfile.TemporaryDirectory(prefix="logged-command-") as temporary:
            log_directory = Path(temporary) / "not-a-log-file"
            log_directory.mkdir()
            result = subprocess.run(
                [str(RUN_LOGGED), str(log_directory), "bash", "-c", "printf 'success\\n'"],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("failed command", result.stderr)
            self.assertIn(str(log_directory), result.stderr)

    def test_command_failure_status_wins_over_log_write_failure(self) -> None:
        with tempfile.TemporaryDirectory(prefix="logged-command-") as temporary:
            log_directory = Path(temporary) / "not-a-log-file"
            log_directory.mkdir()
            result = subprocess.run(
                [str(RUN_LOGGED), str(log_directory), "bash", "-c", "exit 7"],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 7)

    def test_command_header_escapes_arguments_on_one_line(self) -> None:
        with tempfile.TemporaryDirectory(prefix="logged-command-") as temporary:
            log = Path(temporary) / "header.log"
            argument = "argument with spaces\nand a newline"
            result = subprocess.run(
                [
                    str(RUN_LOGGED),
                    str(log),
                    "bash",
                    "-c",
                    "printf '%s\\n' \"$1\"",
                    "bash",
                    argument,
                ],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            header = log.read_text(encoding="utf-8").splitlines()[0]
            self.assertTrue(header.startswith("command:"))
            self.assertEqual(header.count("\n"), 0)
            self.assertIn("$'argument with spaces\\nand a newline'", header)
            self.assertIn("argument with spaces\n", result.stdout)

    def test_failure_summary_escapes_log_path_on_one_line(self) -> None:
        with tempfile.TemporaryDirectory(prefix="logged-command-") as temporary:
            log = Path(temporary) / "log\nwith-newline.txt"
            result = subprocess.run(
                [str(RUN_LOGGED), str(log), "bash", "-c", "exit 9"],
                cwd=ROOT,
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 9)
            self.assertEqual(len(result.stderr.splitlines()), 1)
            self.assertIn("failed command", result.stderr)
            self.assertIn("$'", result.stderr)


if __name__ == "__main__":
    unittest.main()
