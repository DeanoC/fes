from __future__ import annotations

import dataclasses
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import threading
import unittest
from pathlib import Path
from unittest import mock

from scripts.dev_bundle import (
    ArtifactManifestV1,
    ResourceEvidenceV2,
    encode_artifact_manifest,
    encode_resource_evidence,
)


SOURCE_COMMIT = "0123456789abcdef0123456789abcdef01234567"
ARTIFACT_SHA256 = "0123456789abcdef" * 4
REPORT_SHA256 = "fedcba9876543210" * 4
RUN_ID = "abcdef0123456789" * 2


def resource_evidence(**overrides: object) -> ResourceEvidenceV2:
    values: dict[str, object] = {
        "schema": 2,
        "experiment": "020_linux_mailbox",
        "board": "misterpi",
        "build_lane": "oss",
        "source_commit": SOURCE_COMMIT,
        "artifact_sha256": ARTIFACT_SHA256,
        "synthesis_report_sha256": REPORT_SHA256,
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
    values.update(overrides)
    return ResourceEvidenceV2(**values)


def artifact_manifest(**overrides: object) -> ArtifactManifestV1:
    values: dict[str, object] = {
        "schema": 1,
        "run_id": RUN_ID,
        "experiment": "020_linux_mailbox",
        "board": "misterpi",
        "build_lane": "oss",
        "artifact_filename": "top.rbf",
        "artifact_size": 123,
        "artifact_sha256": ARTIFACT_SHA256,
        "source_commit": SOURCE_COMMIT,
    }
    values.update(overrides)
    return ArtifactManifestV1(**values)


class ResourceEvidenceTests(unittest.TestCase):
    def test_resource_evidence_is_exact_compact_ordered_json(self) -> None:
        value = resource_evidence()

        encoded = encode_resource_evidence(value)

        expected = (
            b'{"schema":2,"experiment":"020_linux_mailbox","board":"misterpi",'
            b'"build_lane":"oss","source_commit":"0123456789abcdef0123456789abcdef01234567",'
            b'"artifact_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",'
            b'"synthesis_report_sha256":"fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",'
            b'"clock_inputs":1,"external_input_ports":0,"external_output_ports":0,'
            b'"bidirectional_ports":0,"hps_general_purpose_interfaces":1,"pll_blocks":0,'
            b'"dsp_blocks":0,"block_memory_bits":0,"lutram_bits":0,"sdram_interfaces":0}\n'
        )
        self.assertEqual(encoded, expected)
        self.assertEqual(encoded.count(b"\n"), 1)
        self.assertLessEqual(len(encoded), 2048)
        self.assertNotRegex(encoded.lower(), rb"signing|signature|key[_-]?sha256")

        parsed = json.loads(encoded)
        self.assertEqual(list(parsed), list(dataclasses.asdict(value)))
        self.assertIs(type(parsed["schema"]), int)
        self.assertEqual(parsed["schema"], 2)

    def test_resource_evidence_accepts_oracle_lane(self) -> None:
        encoded = encode_resource_evidence(resource_evidence(build_lane="oracle"))

        self.assertEqual(json.loads(encoded)["build_lane"], "oracle")

    def test_resource_evidence_requires_fixed_identity_and_schema(self) -> None:
        cases = {
            "schema": (True, 1, 2.0, "2", None),
            "experiment": ("010_blinky", "020_linux_mailbox ", "", None),
            "board": ("de10nano", "MISTERPI", "", None),
            "build_lane": ("quartus", "signed", "", None),
        }
        for field, values in cases.items():
            for invalid in values:
                with self.subTest(field=field, invalid=invalid):
                    with self.assertRaises(ValueError):
                        encode_resource_evidence(resource_evidence(**{field: invalid}))

    def test_resource_evidence_requires_lowercase_commit_and_hashes(self) -> None:
        cases = {
            "source_commit": (
                "A" + SOURCE_COMMIT[1:],
                SOURCE_COMMIT[:-1],
                SOURCE_COMMIT + "0",
                "g" * 40,
                0,
                None,
            ),
            "artifact_sha256": (
                "A" + ARTIFACT_SHA256[1:],
                ARTIFACT_SHA256[:-1],
                ARTIFACT_SHA256 + "0",
                "g" * 64,
                0,
                None,
            ),
            "synthesis_report_sha256": (
                "F" + REPORT_SHA256[1:],
                REPORT_SHA256[:-1],
                REPORT_SHA256 + "0",
                "g" * 64,
                0,
                None,
            ),
        }
        for field, values in cases.items():
            for invalid in values:
                with self.subTest(field=field, invalid=invalid):
                    with self.assertRaises(ValueError):
                        encode_resource_evidence(resource_evidence(**{field: invalid}))

    def test_resource_evidence_requires_exact_clock_hps_and_forbidden_counts(self) -> None:
        self.assertRaises(
            ValueError,
            encode_resource_evidence,
            resource_evidence(clock_inputs=0),
        )
        self.assertRaises(
            ValueError,
            encode_resource_evidence,
            resource_evidence(hps_general_purpose_interfaces=0),
        )
        for field in (
            "external_input_ports",
            "external_output_ports",
            "bidirectional_ports",
            "pll_blocks",
            "dsp_blocks",
            "block_memory_bits",
            "lutram_bits",
            "sdram_interfaces",
        ):
            with self.subTest(field=field):
                with self.assertRaises(ValueError):
                    encode_resource_evidence(resource_evidence(**{field: 1}))

    def test_resource_evidence_rejects_non_integer_or_out_of_range_counts(self) -> None:
        fields = (
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
        )
        invalid_values = (True, False, 1.0, "1", "1e0", None, -1, 4294967296)
        for field in fields:
            for invalid in invalid_values:
                with self.subTest(field=field, invalid=invalid):
                    with self.assertRaises(ValueError):
                        encode_resource_evidence(resource_evidence(**{field: invalid}))

        valid = resource_evidence(
            clock_inputs=1,
            hps_general_purpose_interfaces=1,
            external_input_ports=0,
            external_output_ports=0,
            bidirectional_ports=0,
            pll_blocks=0,
            dsp_blocks=0,
            block_memory_bits=0,
            lutram_bits=0,
            sdram_interfaces=0,
        )
        self.assertIsInstance(encode_resource_evidence(valid), bytes)

    def test_resource_evidence_rejects_wrong_input_shape(self) -> None:
        with self.assertRaises(ValueError):
            encode_resource_evidence({"schema": 2})  # type: ignore[arg-type]


class ArtifactManifestTests(unittest.TestCase):
    def test_artifact_manifest_is_exact_compact_ordered_json(self) -> None:
        value = artifact_manifest()

        encoded = encode_artifact_manifest(value)

        expected = (
            b'{"schema":1,"run_id":"abcdef0123456789abcdef0123456789",'
            b'"experiment":"020_linux_mailbox","board":"misterpi","build_lane":"oss",'
            b'"artifact_filename":"top.rbf","artifact_size":123,'
            b'"artifact_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",'
            b'"source_commit":"0123456789abcdef0123456789abcdef01234567"}\n'
        )
        self.assertEqual(encoded, expected)
        self.assertEqual(encoded.count(b"\n"), 1)
        self.assertNotRegex(encoded.lower(), rb"signing|signature|key[_-]?sha256")

        parsed = json.loads(encoded)
        self.assertEqual(list(parsed), list(dataclasses.asdict(value)))
        self.assertIs(type(parsed["schema"]), int)
        self.assertIs(type(parsed["artifact_size"]), int)

    def test_artifact_manifest_accepts_oracle_lane(self) -> None:
        encoded = encode_artifact_manifest(artifact_manifest(build_lane="oracle"))

        self.assertEqual(json.loads(encoded)["build_lane"], "oracle")

    def test_artifact_manifest_requires_fixed_identity_and_types(self) -> None:
        cases = {
            "schema": (True, 0, 1.0, "1", None),
            "run_id": ("A" + RUN_ID[1:], RUN_ID[:-1], RUN_ID + "0", "g" * 32, 0, None),
            "experiment": ("010_blinky", "", None, 1),
            "board": ("de10nano", "", None, 1),
            "build_lane": ("quartus", "", None, 1),
            "artifact_filename": ("top.bit", "", None, 1),
            "artifact_size": (True, 1.0, "123", "1e2", None, 0, 16777217),
            "artifact_sha256": ("A" + ARTIFACT_SHA256[1:], ARTIFACT_SHA256[:-1], "g" * 64, 0, None),
            "source_commit": ("A" + SOURCE_COMMIT[1:], SOURCE_COMMIT[:-1], "g" * 40, 0, None),
        }
        for field, values in cases.items():
            for invalid in values:
                with self.subTest(field=field, invalid=invalid):
                    with self.assertRaises(ValueError):
                        encode_artifact_manifest(artifact_manifest(**{field: invalid}))

    def test_artifact_manifest_rejects_wrong_input_shape(self) -> None:
        with self.assertRaises(ValueError):
            encode_artifact_manifest({"schema": 1})  # type: ignore[arg-type]


class InterfaceTests(unittest.TestCase):
    def test_build_bundle_is_exposed_as_a_path_returning_interface(self) -> None:
        from scripts.dev_bundle import build_bundle

        self.assertEqual(build_bundle.__annotations__["return"], Path)

        with self.assertRaises(ValueError):
            build_bundle("unsupported_experiment", "oss")

    def test_clean_real_fixture_subprocess_binds_current_producer(self) -> None:
        fixture_value = os.environ.get("DEV_BUNDLE_REAL_FIXTURE")
        if not fixture_value:
            self.skipTest("set DEV_BUNDLE_REAL_FIXTURE for the real clean-fixture gate")
        fixture = Path(fixture_value)
        self.assertTrue(fixture.is_dir())
        self.assertEqual(
            (fixture / "scripts" / "dev_bundle.py").read_bytes(),
            Path(__file__).resolve().parents[1].joinpath("scripts/dev_bundle.py").read_bytes(),
        )
        for lane, run_id in (
            ("oss", "0123456789abcdef0123456789abcdef"),
            ("oracle", "fedcba9876543210fedcba9876543210"),
        ):
            with self.subTest(lane=lane):
                result = subprocess.run(
                    [
                        sys.executable,
                        str(fixture / "scripts" / "dev_bundle.py"),
                        "--experiment",
                        "020_linux_mailbox",
                        "--lane",
                        lane,
                        "--run-id",
                        run_id,
                    ],
                    cwd=fixture,
                    text=True,
                    capture_output=True,
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(result.stderr, "")


def _sha256(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _canonical_policy() -> tuple[dict[str, object], str]:
    from scripts.experiment_policy import policy_for

    policy = policy_for("020_linux_mailbox").as_dict()
    encoded = json.dumps(policy, ensure_ascii=True, separators=(",", ":"), sort_keys=True)
    return policy, hashlib.sha256(encoded.encode("utf-8")).hexdigest()


def _parse_artifact_manifest_independently(
    payload: bytes,
    *,
    expected_commit: str,
    expected_lane: str,
    artifact_bytes: bytes,
) -> dict[str, object]:
    """Independent parser for the FogCast nine-field artifact contract."""

    if not payload.endswith(b"\n") or payload.count(b"\n") != 1:
        raise ValueError("artifact manifest must have exactly one terminal newline")
    if payload[:-1].rstrip(b" \t\r") != payload[:-1]:
        raise ValueError("artifact manifest has trailing whitespace")
    pairs: list[tuple[str, object]] = []

    def collect(items: list[tuple[str, object]]) -> dict[str, object]:
        pairs.extend(items)
        return dict(items)

    try:
        value = json.loads(payload[:-1].decode("utf-8"), object_pairs_hook=collect)
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise ValueError("artifact manifest is not one JSON object") from exc
    if not isinstance(value, dict):
        raise ValueError("artifact manifest is not an object")
    expected_keys = (
        "schema",
        "run_id",
        "experiment",
        "board",
        "build_lane",
        "artifact_filename",
        "artifact_size",
        "artifact_sha256",
        "source_commit",
    )
    if tuple(key for key, _ in pairs) != expected_keys:
        raise ValueError("artifact manifest keys are not exact and ordered")
    if type(value.get("schema")) is not int or value["schema"] != 1:
        raise ValueError("artifact manifest schema is invalid")
    if type(value.get("run_id")) is not str or re.fullmatch(r"[0-9a-f]{32}", value["run_id"]) is None:
        raise ValueError("artifact manifest run ID is invalid")
    if value.get("experiment") != "020_linux_mailbox":
        raise ValueError("artifact manifest experiment is invalid")
    if value.get("board") != "misterpi":
        raise ValueError("artifact manifest board is invalid")
    if value.get("build_lane") != expected_lane:
        raise ValueError("artifact manifest lane is invalid")
    if value.get("artifact_filename") != "top.rbf":
        raise ValueError("artifact manifest filename is invalid")
    expected_size = len(artifact_bytes)
    expected_hash = hashlib.sha256(artifact_bytes).hexdigest()
    if type(value.get("artifact_size")) is not int or value["artifact_size"] != expected_size:
        raise ValueError("artifact manifest size is invalid")
    if value.get("artifact_sha256") != expected_hash:
        raise ValueError("artifact manifest artifact hash is invalid")
    if value.get("source_commit") != expected_commit:
        raise ValueError("artifact manifest source commit is invalid")
    return value


def _independent_fixture_report_expectations(report_bytes: bytes, lane: str) -> dict[str, int]:
    expected_report = f"{lane} synthesis report\n".encode("ascii")
    if report_bytes != expected_report:
        raise ValueError("fixture report is not the independently recognized report")
    return {
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


def _parse_resource_evidence_independently(
    payload: bytes,
    *,
    expected_commit: str,
    expected_lane: str,
    artifact_bytes: bytes,
    report_bytes: bytes,
) -> dict[str, object]:
    """Parse evidence using opened fixture artifact/report, never manifest fields."""

    if not payload.endswith(b"\n") or payload.count(b"\n") != 1:
        raise ValueError("resource evidence must have exactly one terminal newline")
    pairs: list[tuple[str, object]] = []

    def collect(items: list[tuple[str, object]]) -> dict[str, object]:
        pairs.extend(items)
        return dict(items)

    value = json.loads(payload[:-1].decode("utf-8"), object_pairs_hook=collect)
    if not isinstance(value, dict) or tuple(key for key, _ in pairs) != (
        "schema", "experiment", "board", "build_lane", "source_commit",
        "artifact_sha256", "synthesis_report_sha256", "clock_inputs",
        "external_input_ports", "external_output_ports", "bidirectional_ports",
        "hps_general_purpose_interfaces", "pll_blocks", "dsp_blocks",
        "block_memory_bits", "lutram_bits", "sdram_interfaces",
    ):
        raise ValueError("resource evidence fields are not exact and ordered")
    expected_counts = _independent_fixture_report_expectations(report_bytes, expected_lane)
    expected = {
        "schema": 2,
        "experiment": "020_linux_mailbox",
        "board": "misterpi",
        "build_lane": expected_lane,
        "source_commit": expected_commit,
        "artifact_sha256": hashlib.sha256(artifact_bytes).hexdigest(),
        "synthesis_report_sha256": hashlib.sha256(report_bytes).hexdigest(),
        **expected_counts,
    }
    if set(value) != set(expected):
        raise ValueError("resource evidence keys are not exact")
    for field, expected_value in expected.items():
        if type(value[field]) is not type(expected_value) or value[field] != expected_value:
            raise ValueError(f"resource evidence field is not independently bound: {field}")
    return value


def _parse_checksums_independently(
    payload: bytes,
    members: dict[str, bytes],
) -> list[str]:
    """Independent parser for the exact three-line GNU checksum contract."""

    if not payload.endswith(b"\n"):
        raise ValueError("checksum record must have a terminal newline")
    lines = payload[:-1].split(b"\n")
    expected_names = ["manifest.json", "resource_evidence.json", "top.rbf"]
    if len(lines) != len(expected_names):
        raise ValueError("checksum record has the wrong number of lines")
    names: list[str] = []
    seen: set[str] = set()
    for line in lines:
        match = re.fullmatch(rb"([0-9a-f]{64})  ([^\n ]+)", line)
        if match is None:
            raise ValueError("checksum line is not GNU sha256sum format")
        digest = match.group(1).decode("ascii")
        name = match.group(2).decode("utf-8")
        if name in seen or name not in members:
            raise ValueError("checksum membership is not unique and exact")
        if hashlib.sha256(members[name]).hexdigest() != digest:
            raise ValueError("checksum does not match payload")
        seen.add(name)
        names.append(name)
    if names != expected_names:
        raise ValueError("checksum members are not in canonical order")
    return names


class BundleTests(unittest.TestCase):
    """Exercise the producer against clean, disposable git repositories."""

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix="dev-bundle-test-")
        self.root = Path(self.temp.name)
        self._build_fixture()

    def tearDown(self) -> None:
        self.temp.cleanup()

    def _run_git(self, *arguments: str) -> str:
        return subprocess.check_output(
            ["git", "-C", str(self.root), *arguments],
            text=True,
        ).strip()

    def _build_fixture(self) -> None:
        (self.root / ".gitignore").write_text("build/\n", encoding="utf-8")
        source_values = {
            "experiments/020_linux_mailbox/rtl/top.v": "module top (input wire FPGA_CLK1_50); endmodule\n",
            "boards/de10nano/pins.qsf": "set_global_assignment -name DEVICE 5CSEBA6U23I7\n",
            "boards/de10nano/clocks.sdc": "create_clock -period 20 [get_ports FPGA_CLK1_50]\n",
            "experiments/020_linux_mailbox/oracle/top.qpf": "PROJECT_REVISION = top\n",
            "experiments/020_linux_mailbox/oracle/top.qsf": "set_global_assignment -name TOP_LEVEL_ENTITY top\n",
        }
        for relative, contents in source_values.items():
            path = self.root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(contents, encoding="utf-8")
        self._run_git("init", "-q", "-b", "main")
        self._run_git("config", "user.email", "fixture@example.invalid")
        self._run_git("config", "user.name", "Fixture")
        self._run_git("add", ".")
        self._run_git("commit", "-qm", "fixture")
        self.commit = self._run_git("rev-parse", "HEAD")
        self.lane_paths: dict[str, dict[str, Path]] = {}
        for lane in ("oss", "oracle"):
            lane_dir = self.root / "build" / lane / "020_linux_mailbox"
            lane_dir.mkdir(parents=True, exist_ok=True)
            report_name = "timing.json" if lane == "oss" else "top.fit.rpt"
            report = lane_dir / report_name
            report.write_bytes((lane + " synthesis report\n").encode("ascii"))
            rbf = lane_dir / "top.rbf"
            rbf.write_bytes((lane + " rbf\n").encode("ascii"))
            self.lane_paths[lane] = {"dir": lane_dir, "report": report, "rbf": rbf}
            self._write_lane_manifest(lane)

    def _write_lane_manifest(self, lane: str, **overrides: object) -> Path:
        policy, policy_hash = _canonical_policy()
        paths = self.lane_paths[lane]
        report = paths["report"]
        rbf = paths["rbf"]
        report_relative = f"build/{lane}/020_linux_mailbox/{report.name}"
        rbf_relative = f"build/{lane}/020_linux_mailbox/top.rbf"
        source_relative = (
            "experiments/020_linux_mailbox/rtl/top.v",
            "boards/de10nano/pins.qsf",
            "boards/de10nano/clocks.sdc",
        )
        if lane == "oracle":
            source_relative += (
                "experiments/020_linux_mailbox/oracle/top.qpf",
                "experiments/020_linux_mailbox/oracle/top.qsf",
            )
        sources = [
            {"path": relative, "sha256": _sha256(self.root / relative)}
            for relative in source_relative
        ]
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
        report_record = {"path": report_relative, "sha256": _sha256(report)}
        artifact_record = {"path": rbf_relative, "sha256": _sha256(rbf)}
        build = {
            "status": "pass",
            "build_status": "pass",
            "route_status": "pass",
            "route": {"status": "pass", "unrouted": False},
            "timing": {
                "status": "pass",
                "clock": "protocol.FPGA_CLK1_50" if lane == "oss" else "FPGA_CLK1_50",
                "requested_mhz": 50.0,
                "achieved_mhz": 100.0,
            },
            "hard_block_status": "pass",
            "unknown_resources": {},
            "experiment": "020_linux_mailbox",
            "lane": lane,
            "target": "5CSEBA6U23I7",
            "top": "top",
            "clock_intent": "FPGA_CLK1_50",
            "clock_constraint_mhz": 50.0,
            "experiment_policy": policy,
            "experiment_policy_sha256": policy_hash,
            "policy_sha256": policy_hash,
            "protocol_source": "experiments/020_linux_mailbox/rtl/top.v",
            "protocol_source_sha256": sources[0]["sha256"],
            "resource_evidence": evidence,
            "source_hashes": {record["path"]: record["sha256"] for record in sources},
            "synthesis_report": report_record,
            "synthesis_report_path": report_relative,
            "synthesis_report_sha256": report_record["sha256"],
            "reproducibility": {
                "rbf_sha256": artifact_record["sha256"],
                "rbf_size_bytes": rbf.stat().st_size,
            },
        }
        manifest = {
            "schema": 2,
            "experiment": "020_linux_mailbox",
            "lane": lane,
            "target": "5CSEBA6U23I7",
            "git": {"commit": self.commit, "dirty": False, "state": "clean", "changes": []},
            "sources": sources,
            "artifacts": [report_record, artifact_record],
            "build": build,
            "experiment_policy": policy,
            "experiment_policy_sha256": policy_hash,
            "policy_sha256": policy_hash,
            "protocol_source_sha256": sources[0]["sha256"],
            "resource_evidence": evidence,
            "synthesis_report": report_record,
            "synthesis_report_path": report_relative,
            "synthesis_report_sha256": report_record["sha256"],
        }
        manifest.update(overrides)
        path = paths["dir"] / "manifest.json"
        path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
        return path

    def _bundle(self, lane: str, run_id: str = RUN_ID) -> Path:
        from scripts import dev_bundle

        with mock.patch.object(dev_bundle, "REPO_ROOT", self.root, create=True):
            return dev_bundle.build_bundle("020_linux_mailbox", lane, run_id)

    def _assert_manifest_rejected(self, lane: str, mutate: object) -> None:
        from scripts import dev_bundle

        manifest_path = self.lane_paths[lane]["dir"] / "manifest.json"
        original = manifest_path.read_bytes()
        manifest = json.loads(original)
        mutate(manifest)
        manifest_path.write_text(json.dumps(manifest) + "\n", encoding="utf-8")
        try:
            with mock.patch.object(dev_bundle, "REPO_ROOT", self.root, create=True):
                with self.assertRaises(ValueError):
                    dev_bundle.build_bundle("020_linux_mailbox", lane, RUN_ID)
        finally:
            manifest_path.write_bytes(original)

    def test_generates_exact_private_bundle_for_each_lane(self) -> None:
        expected_keys = [
            "schema",
            "run_id",
            "experiment",
            "board",
            "build_lane",
            "artifact_filename",
            "artifact_size",
            "artifact_sha256",
            "source_commit",
        ]
        evidence_keys = [
            "schema",
            "experiment",
            "board",
            "build_lane",
            "source_commit",
            "artifact_sha256",
            "synthesis_report_sha256",
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
        ]
        for lane in ("oss", "oracle"):
            with self.subTest(lane=lane):
                bundle = self._bundle(lane)
                self.assertEqual(
                    sorted(path.name for path in bundle.iterdir()),
                    ["bundle.sha256", "manifest.json", "resource_evidence.json", "top.rbf"],
                )
                self.assertEqual(stat.S_IMODE(bundle.stat().st_mode), 0o700)
                for member in bundle.iterdir():
                    self.assertEqual(stat.S_IMODE(member.stat().st_mode), 0o600)
                    self.assertFalse(member.is_symlink())
                manifest_bytes = (bundle / "manifest.json").read_bytes()
                self.assertEqual(manifest_bytes.count(b"\n"), 1)
                manifest = json.loads(manifest_bytes)
                self.assertEqual(list(manifest), expected_keys)
                self.assertEqual(manifest["schema"], 1)
                self.assertEqual(manifest["run_id"], RUN_ID)
                self.assertEqual(manifest["build_lane"], lane)
                self.assertEqual(manifest["artifact_filename"], "top.rbf")
                self.assertEqual(manifest["source_commit"], self.commit)
                evidence = json.loads((bundle / "resource_evidence.json").read_bytes())
                self.assertEqual(list(evidence), evidence_keys)
                self.assertEqual(evidence["schema"], 2)
                self.assertEqual(evidence["build_lane"], lane)
                self.assertEqual(evidence["source_commit"], self.commit)
                self.assertNotIn(b"signature", (bundle / "resource_evidence.json").read_bytes())
                self.assertNotIn(b"signing", (bundle / "manifest.json").read_bytes())
                lines = (bundle / "bundle.sha256").read_text(encoding="ascii").splitlines()
                self.assertEqual([line.split("  ", 1)[1] for line in lines], [
                    "manifest.json",
                    "resource_evidence.json",
                    "top.rbf",
                ])
                self.assertEqual(
                    lines,
                    [
                        f"{_sha256(bundle / name)}  {name}"
                        for name in ("manifest.json", "resource_evidence.json", "top.rbf")
                    ],
                )

    def test_repeated_generation_replaces_atomically_and_is_byte_identical(self) -> None:
        first = self._bundle("oss")
        first_bytes = {path.name: path.read_bytes() for path in first.iterdir()}
        (first / "unexpected").write_bytes(b"must be replaced")
        second = self._bundle("oss")
        self.assertEqual(first, second)
        self.assertEqual(sorted(path.name for path in second.iterdir()), sorted(first_bytes))
        self.assertEqual(
            {path.name: path.read_bytes() for path in second.iterdir()},
            first_bytes,
        )

    def test_manifest_binding_rejects_every_required_provenance_mismatch(self) -> None:
        def artifact_record(manifest: dict[str, object], suffix: str) -> dict[str, object]:
            records = manifest["artifacts"]
            for record in records:  # type: ignore[union-attr]
                if record["path"].endswith(suffix):  # type: ignore[index,union-attr]
                    return record  # type: ignore[return-value]
            raise AssertionError(f"missing artifact {suffix}")

        cases: list[tuple[str, object]] = [
            ("manifest schema", lambda m: m.__setitem__("schema", 1)),
            ("experiment", lambda m: m.__setitem__("experiment", "010_blinky")),
            ("lane", lambda m: m.__setitem__("lane", "mismatch")),
            ("target", lambda m: m.__setitem__("target", "de10nano")),
            ("source commit", lambda m: m["git"].__setitem__("commit", "0" * 40)),  # type: ignore[index]
            ("source dirty", lambda m: m["git"].__setitem__("dirty", True)),  # type: ignore[index]
            ("source state", lambda m: m["git"].__setitem__("state", "dirty")),  # type: ignore[index]
            ("source changes", lambda m: m["git"].__setitem__("changes", ["dirty"])),  # type: ignore[index]
            ("source record hash", lambda m: m["sources"][0].__setitem__("sha256", "0" * 64)),  # type: ignore[index]
            ("source record path", lambda m: m["sources"][0].__setitem__("path", "missing.v")),  # type: ignore[index]
            ("build source hash", lambda m: m["build"]["source_hashes"].__setitem__(  # type: ignore[index]
                "experiments/020_linux_mailbox/rtl/top.v", "0" * 64
            )),
            ("policy object", lambda m: m["experiment_policy"].__setitem__("top", "wrong")),  # type: ignore[index]
            ("policy hash", lambda m: m.__setitem__("experiment_policy_sha256", "0" * 64)),
            ("policy hash alias", lambda m: m.__setitem__("policy_sha256", "0" * 64)),
            ("build policy object", lambda m: m["build"]["experiment_policy"].__setitem__("top", "wrong")),  # type: ignore[index]
            ("build policy hash", lambda m: m["build"].__setitem__("experiment_policy_sha256", "0" * 64)),  # type: ignore[index]
            ("build policy hash alias", lambda m: m["build"].__setitem__("policy_sha256", "0" * 64)),  # type: ignore[index]
            ("protocol hash", lambda m: m.__setitem__("protocol_source_sha256", "0" * 64)),
            ("build protocol path", lambda m: m["build"].__setitem__("protocol_source", "wrong.v")),  # type: ignore[index]
            ("build protocol hash", lambda m: m["build"].__setitem__("protocol_source_sha256", "0" * 64)),  # type: ignore[index]
            ("report map path", lambda m: m["synthesis_report"].__setitem__("path", "wrong.rpt")),  # type: ignore[index]
            ("report map hash", lambda m: m["synthesis_report"].__setitem__("sha256", "0" * 64)),  # type: ignore[index]
            ("report path", lambda m: m.__setitem__("synthesis_report_path", "wrong.rpt")),
            ("report hash", lambda m: m.__setitem__("synthesis_report_sha256", "0" * 64)),
            ("build report map path", lambda m: m["build"]["synthesis_report"].__setitem__("path", "wrong.rpt")),  # type: ignore[index]
            ("build report map hash", lambda m: m["build"]["synthesis_report"].__setitem__("sha256", "0" * 64)),  # type: ignore[index]
            ("build report path", lambda m: m["build"].__setitem__("synthesis_report_path", "wrong.rpt")),  # type: ignore[index]
            ("build report hash", lambda m: m["build"].__setitem__("synthesis_report_sha256", "0" * 64)),  # type: ignore[index]
            ("artifact filename", lambda m: artifact_record(m, "top.rbf").__setitem__("path", "build/oss/020_linux_mailbox/top.bit")),
            ("artifact hash", lambda m: artifact_record(m, "top.rbf").__setitem__("sha256", "0" * 64)),
            ("artifact repro hash", lambda m: m["build"]["reproducibility"].__setitem__("rbf_sha256", "0" * 64)),  # type: ignore[index]
            ("artifact size", lambda m: m["build"]["reproducibility"].__setitem__("rbf_size_bytes", 999)),  # type: ignore[index]
            ("build status", lambda m: m["build"].__setitem__("status", "fail")),  # type: ignore[index]
            ("build status alias", lambda m: m["build"].__setitem__("build_status", "fail")),  # type: ignore[index]
            ("route status alias", lambda m: m["build"].__setitem__("route_status", "fail")),  # type: ignore[index]
            ("hard-block status", lambda m: m["build"].__setitem__("hard_block_status", "fail")),  # type: ignore[index]
            ("route nested status", lambda m: m["build"]["route"].__setitem__("status", "fail")),  # type: ignore[index]
            ("route unrouted", lambda m: m["build"]["route"].__setitem__("unrouted", True)),  # type: ignore[index]
            ("timing status", lambda m: m["build"]["timing"].__setitem__("status", "fail")),  # type: ignore[index]
            ("timing clock", lambda m: m["build"]["timing"].__setitem__("clock", "wrong")),  # type: ignore[index]
            ("timing requested", lambda m: m["build"]["timing"].__setitem__("requested_mhz", 25.0)),  # type: ignore[index]
            ("timing achieved", lambda m: m["build"]["timing"].__setitem__("achieved_mhz", 49.0)),  # type: ignore[index]
        ]
        for lane in ("oss", "oracle"):
            for name, mutate in cases:
                with self.subTest(lane=lane, mismatch=name):
                    if name == "artifact filename":
                        def lane_artifact_filename(manifest: dict[str, object], lane: str = lane) -> None:
                            artifact_record(manifest, "top.rbf")["path"] = f"build/{lane}/020_linux_mailbox/top.bit"

                        self._assert_manifest_rejected(lane, lane_artifact_filename)
                    else:
                        self._assert_manifest_rejected(lane, mutate)

            for location in ("top", "build"):
                for field, expected in (
                    ("clock_inputs", 1),
                    ("external_input_ports", 0),
                    ("external_output_ports", 0),
                    ("bidirectional_ports", 0),
                    ("hps_general_purpose_interfaces", 1),
                    ("pll_blocks", 0),
                    ("dsp_blocks", 0),
                    ("block_memory_bits", 0),
                    ("lutram_bits", 0),
                    ("sdram_interfaces", 0),
                ):
                    invalid = 0 if expected == 1 else 1

                    def mutate_resource(
                        manifest: dict[str, object],
                        location: str = location,
                        field: str = field,
                        invalid: int = invalid,
                    ) -> None:
                        target = manifest if location == "top" else manifest["build"]
                        target["resource_evidence"][field] = invalid  # type: ignore[index]

                    with self.subTest(lane=lane, location=location, resource=field):
                        self._assert_manifest_rejected(lane, mutate_resource)

    def test_manifest_binding_rejects_missing_build_contract_fields(self) -> None:
        fields = (
            "experiment",
            "lane",
            "target",
            "top",
            "clock_intent",
            "clock_constraint_mhz",
            "unknown_resources",
        )
        for lane in ("oss", "oracle"):
            for field in fields:
                with self.subTest(lane=lane, field=field):
                    self._assert_manifest_rejected(
                        lane,
                        lambda manifest, field=field: manifest["build"].pop(field),  # type: ignore[index]
                    )

    def test_policy_bindings_reject_nested_type_and_digest_tampering(self) -> None:
        cases = (
            ("allowed count bool", lambda policy: policy["allowed_hard_blocks"].__setitem__(
                "cyclonev_hps_interface_mpu_general_purpose", True
            )),
            ("allowed count float", lambda policy: policy["allowed_hard_blocks"].__setitem__(
                "cyclonev_hps_interface_mpu_general_purpose", 1.0
            )),
            ("allowed count exponent", lambda policy: policy["allowed_hard_blocks"].__setitem__(
                "cyclonev_hps_interface_mpu_general_purpose", 1e0
            )),
            ("allowed count string", lambda policy: policy["allowed_hard_blocks"].__setitem__(
                "cyclonev_hps_interface_mpu_general_purpose", "1"
            )),
            ("allowed count null", lambda policy: policy["allowed_hard_blocks"].__setitem__(
                "cyclonev_hps_interface_mpu_general_purpose", None
            )),
            ("extra field", lambda policy: policy.__setitem__("unexpected", 1)),
        )
        for lane in ("oss", "oracle"):
            for location in ("top", "build"):
                for name, mutate_policy in cases:
                    with self.subTest(lane=lane, location=location, case=name):
                        def mutate(manifest: dict[str, object], location: str = location) -> None:
                            target = manifest if location == "top" else manifest["build"]
                            mutate_policy(target["experiment_policy"])  # type: ignore[index]

                        self._assert_manifest_rejected(lane, mutate)

            def stale_policy_hash(manifest: dict[str, object]) -> None:
                manifest["experiment_policy_sha256"] = "0" * 64
                manifest["policy_sha256"] = "0" * 64

            self._assert_manifest_rejected(lane, stale_policy_hash)

    def test_independent_output_parser_binds_opened_artifact_report_and_counts(self) -> None:
        for lane in ("oss", "oracle"):
            with self.subTest(lane=lane):
                bundle = self._bundle(lane)
                artifact_bytes = self.lane_paths[lane]["rbf"].read_bytes()
                report_bytes = self.lane_paths[lane]["report"].read_bytes()
                _parse_artifact_manifest_independently(
                    (bundle / "manifest.json").read_bytes(),
                    expected_commit=self.commit,
                    expected_lane=lane,
                    artifact_bytes=artifact_bytes,
                )
                _parse_resource_evidence_independently(
                    (bundle / "resource_evidence.json").read_bytes(),
                    expected_commit=self.commit,
                    expected_lane=lane,
                    artifact_bytes=artifact_bytes,
                    report_bytes=report_bytes,
                )

                evidence = json.loads((bundle / "resource_evidence.json").read_bytes())
                for field, invalid in (
                    ("artifact_sha256", "0" * 64),
                    ("synthesis_report_sha256", "0" * 64),
                    ("clock_inputs", 0),
                    ("hps_general_purpose_interfaces", 0),
                ):
                    with self.subTest(lane=lane, evidence_field=field):
                        hostile = dict(evidence)
                        hostile[field] = invalid
                        with self.assertRaises(ValueError):
                            _parse_resource_evidence_independently(
                                (json.dumps(hostile, separators=(",", ":")) + "\n").encode(),
                                expected_commit=self.commit,
                                expected_lane=lane,
                                artifact_bytes=artifact_bytes,
                                report_bytes=report_bytes,
                            )

    def test_existing_bundle_uses_atomic_exchange_without_observable_gap(self) -> None:
        from scripts import dev_bundle

        final = self._bundle("oss")
        real_exchange = getattr(dev_bundle, "_rename_exchange", None)
        observations: list[tuple[str, bool, bool]] = []
        concurrent_visibility: list[bool] = []

        def observe_exchange(source: Path, destination: Path) -> None:
            observations.append(("before", destination.exists(), source.exists()))
            if real_exchange is None:
                return
            stop = threading.Event()
            started = threading.Event()

            def observe() -> None:
                started.set()
                while not stop.is_set():
                    concurrent_visibility.append(destination.exists())

            observer = threading.Thread(target=observe)
            observer.start()
            self.assertTrue(started.wait(timeout=2))
            real_exchange(source, destination)
            stop.set()
            observer.join()
            observations.append(("after", destination.exists(), source.exists()))

        with mock.patch.object(
            dev_bundle,
            "_rename_exchange",
            side_effect=observe_exchange,
            create=True,
        ) as exchange:
            self._bundle("oss")

        exchange.assert_called_once()
        self.assertEqual(
            observations,
            [("before", True, True), ("after", True, True)],
        )
        self.assertTrue(concurrent_visibility)
        self.assertTrue(all(concurrent_visibility))
        self.assertTrue(final.is_dir())

    def test_unsupported_existing_exchange_fails_closed_without_siblings(self) -> None:
        from scripts import dev_bundle

        final = self._bundle("oss")
        before = {path.name: path.read_bytes() for path in final.iterdir()}
        with mock.patch.object(
            dev_bundle,
            "_rename_exchange",
            side_effect=OSError("renameat2 unavailable"),
            create=True,
        ):
            with self.assertRaises(ValueError):
                self._bundle("oss")

        self.assertEqual(
            {path.name: path.read_bytes() for path in final.iterdir()},
            before,
        )
        self.assertEqual(
            [path.name for path in final.parent.iterdir() if path.name.startswith(".")],
            [],
        )

    def _assert_failed_publish_restores_old_tree(
        self,
        inject: object,
        *,
        expected_message: str,
    ) -> None:
        from scripts import dev_bundle

        final = self._bundle("oss")
        before = {path.name: path.read_bytes() for path in final.iterdir()}
        with inject:
            with self.assertRaises(ValueError) as failure:
                self._bundle("oss")
        self.assertIn(expected_message, str(failure.exception))
        self.assertEqual(
            {path.name: path.read_bytes() for path in final.iterdir()},
            before,
        )
        self.assertEqual(
            [
                path.name
                for path in final.parent.iterdir()
                if path.name.startswith(".020_linux_mailbox.tmp-")
                or path.name.startswith(".020_linux_mailbox.old-")
            ],
            [],
        )

    def test_parent_fsync_failure_rolls_back_exchange_and_cleans_staging(self) -> None:
        from scripts import dev_bundle

        calls = 0
        original = dev_bundle._fsync_directory

        def fail_once(path: Path, label: str) -> None:
            nonlocal calls
            if label == "bundle parent directory":
                calls += 1
                if calls == 1:
                    raise OSError("injected parent fsync failure")
            original(path, label)

        self._assert_failed_publish_restores_old_tree(
            mock.patch.object(dev_bundle, "_fsync_directory", side_effect=fail_once),
            expected_message="injected parent fsync failure",
        )

    def test_old_tree_delete_partial_failure_preserves_new_final_and_stale_tree(self) -> None:
        final = self._bundle("oss")
        new_bytes = {path.name: path.read_bytes() for path in final.iterdir()}
        (final / "old-only").write_bytes(b"diagnostic old tree member")
        original = shutil.rmtree

        def delete_one_member_then_fail(path: object, *args: object, **kwargs: object) -> None:
            tree = Path(path)
            members = sorted(tree.iterdir(), key=lambda child: child.name)
            members[0].unlink()
            raise OSError("injected old-tree delete after one member")

        with mock.patch.object(shutil, "rmtree", side_effect=delete_one_member_then_fail):
            with self.assertRaises(ValueError) as failure:
                self._bundle("oss")
        self.assertIn("injected old-tree delete after one member", str(failure.exception))
        self.assertIn("authoritative", str(failure.exception))
        self.assertEqual(
            {path.name: path.read_bytes() for path in final.iterdir()},
            new_bytes,
        )
        stale = [path for path in final.parent.iterdir() if path.name.startswith(".020_linux_mailbox.stale-")]
        self.assertEqual(len(stale), 1)
        self.assertTrue((stale[0] / "old-only").exists())
        self.assertLess(
            len(list(stale[0].iterdir())),
            len(new_bytes) + 1,
        )
        self.assertFalse(any(path.name.startswith(".020_linux_mailbox.tmp-") for path in final.parent.iterdir()))

    def test_old_tree_delete_complete_then_failure_fsyncs_parent_without_rollback(self) -> None:
        from scripts import dev_bundle

        final = self._bundle("oss")
        new_bytes = {path.name: path.read_bytes() for path in final.iterdir()}
        original_fsync = dev_bundle._fsync_directory
        fsync_labels: list[str] = []

        def delete_all_then_fail(path: object, *args: object, **kwargs: object) -> None:
            tree = Path(path)
            for member in tree.iterdir():
                member.unlink()
            tree.rmdir()
            raise OSError("injected complete old-tree delete")

        def record_fsync(path: Path, label: str) -> None:
            fsync_labels.append(label)
            original_fsync(path, label)

        with mock.patch.object(shutil, "rmtree", side_effect=delete_all_then_fail):
            with mock.patch.object(dev_bundle, "_fsync_directory", side_effect=record_fsync):
                with self.assertRaises(ValueError) as failure:
                    self._bundle("oss")
        self.assertIn("injected complete old-tree delete", str(failure.exception))
        self.assertIn("stale bundle parent directory", fsync_labels)
        self.assertEqual(
            {path.name: path.read_bytes() for path in final.iterdir()},
            new_bytes,
        )
        self.assertFalse(any(path.name.startswith(".020_linux_mailbox.tmp-") for path in final.parent.iterdir()))
        self.assertFalse(any(path.name.startswith(".020_linux_mailbox.stale-") for path in final.parent.iterdir()))

    def test_persistent_delete_failure_preserves_new_final_and_stale_tree(self) -> None:
        final = self._bundle("oss")
        before = {path.name: path.read_bytes() for path in final.iterdir()}

        def always_fail(path: object, *args: object, **kwargs: object) -> None:
            raise OSError("injected persistent delete failure")

        with mock.patch.object(shutil, "rmtree", side_effect=always_fail):
            with self.assertRaises(ValueError) as failure:
                self._bundle("oss")
        self.assertIn("injected persistent delete failure", str(failure.exception))
        self.assertIn("authoritative", str(failure.exception))
        self.assertEqual(
            {path.name: path.read_bytes() for path in final.iterdir()},
            before,
        )
        self.assertEqual(
            len([path for path in final.parent.iterdir() if path.name.startswith(".020_linux_mailbox.stale-")]),
            1,
        )
        self.assertEqual(
            [
                path.name
                for path in final.parent.iterdir()
                if path.name.startswith(".020_linux_mailbox.tmp-")
                or path.name.startswith(".020_linux_mailbox.old-")
            ],
            [],
        )

    def test_old_tree_delete_success_fsync_failure_keeps_new_final_authoritative(self) -> None:
        from scripts import dev_bundle

        final = self._bundle("oss")
        before = {path.name: path.read_bytes() for path in final.iterdir()}
        original = dev_bundle._fsync_directory

        def fail_after_old_delete(path: Path, label: str) -> None:
            if label == "bundle old-tree cleanup parent directory":
                raise OSError("injected post-delete parent fsync")
            original(path, label)

        with mock.patch.object(dev_bundle, "_fsync_directory", side_effect=fail_after_old_delete):
            with self.assertRaises(ValueError) as failure:
                self._bundle("oss")
        self.assertIn("authoritative", str(failure.exception))
        self.assertIn("injected post-delete parent fsync", str(failure.exception))
        self.assertEqual(
            {path.name: path.read_bytes() for path in final.iterdir()},
            before,
        )
        self.assertFalse(any(path.name.startswith(".020_linux_mailbox.tmp-") for path in final.parent.iterdir()))
        self.assertFalse(any(path.name.startswith(".020_linux_mailbox.stale-") for path in final.parent.iterdir()))

    def test_rollback_cleanup_fsync_failure_keeps_restored_old_final(self) -> None:
        from scripts import dev_bundle

        final = self._bundle("oss")
        before = {path.name: path.read_bytes() for path in final.iterdir()}
        original = dev_bundle._fsync_directory

        def fail_exchange_and_rollback_cleanup(path: Path, label: str) -> None:
            if label == "bundle parent directory":
                raise OSError("injected post-exchange parent fsync")
            if label == "bundle rollback cleanup parent directory":
                raise OSError("injected rollback deletion fsync")
            original(path, label)

        with mock.patch.object(
            dev_bundle,
            "_fsync_directory",
            side_effect=fail_exchange_and_rollback_cleanup,
        ):
            with self.assertRaises(ValueError) as failure:
                self._bundle("oss")
        self.assertIn("injected post-exchange parent fsync", str(failure.exception))
        self.assertIn("injected rollback deletion fsync", str(failure.exception))
        self.assertEqual(
            {path.name: path.read_bytes() for path in final.iterdir()},
            before,
        )
        self.assertFalse(any(path.name.startswith(".020_linux_mailbox.tmp-") for path in final.parent.iterdir()))

    def test_first_publication_replace_failure_cleans_temp_and_fsyncs_parent(self) -> None:
        from scripts import dev_bundle

        parent = self.root / "build" / "dev-bundle" / "oss"
        final = parent / "020_linux_mailbox"
        replace_calls: list[tuple[Path, Path]] = []
        fsync_labels: list[str] = []
        original_replace = dev_bundle.os.replace
        original_fsync = dev_bundle._fsync_directory

        def fail_replace(source: object, destination: object) -> None:
            source_path = Path(source)
            destination_path = Path(destination)
            replace_calls.append((source_path, destination_path))
            if destination_path == final:
                raise OSError("injected first-publication replace")
            original_replace(source, destination)

        def record_fsync(path: Path, label: str) -> None:
            fsync_labels.append(label)
            original_fsync(path, label)

        with mock.patch.object(dev_bundle.os, "replace", side_effect=fail_replace):
            with mock.patch.object(dev_bundle, "_fsync_directory", side_effect=record_fsync):
                with self.assertRaises(ValueError) as failure:
                    self._bundle("oss")
        self.assertIn("injected first-publication replace", str(failure.exception))
        self.assertEqual(len(replace_calls), 1)
        self.assertIn("bundle rollback parent directory", fsync_labels)
        self.assertFalse(final.exists())
        self.assertFalse(any(path.name.startswith(".020_linux_mailbox.tmp-") for path in parent.iterdir()))

    def test_first_publication_parent_fsync_failure_cleans_new_bundle(self) -> None:
        from scripts import dev_bundle

        parent = self.root / "build" / "dev-bundle" / "oss"
        final = parent / "020_linux_mailbox"
        original = dev_bundle._fsync_directory
        failed = False

        def fail_first_parent_fsync(path: Path, label: str) -> None:
            nonlocal failed
            if label == "bundle parent directory" and not failed:
                failed = True
                raise OSError("injected first-publication parent fsync")
            original(path, label)

        with mock.patch.object(dev_bundle, "_fsync_directory", side_effect=fail_first_parent_fsync):
            with self.assertRaises(ValueError) as failure:
                self._bundle("oss")
        self.assertIn("injected first-publication parent fsync", str(failure.exception))
        self.assertFalse(final.exists())
        self.assertEqual(
            [
                path.name
                for path in parent.iterdir()
                if path.name.startswith(".020_linux_mailbox.tmp-")
                or path.name.startswith(".020_linux_mailbox.stale-")
            ],
            [],
        )

    def test_rollback_errors_are_aggregated_and_do_not_leave_temp_siblings(self) -> None:
        from scripts import dev_bundle

        calls = 0
        original = dev_bundle._fsync_directory

        def fail_twice(path: Path, label: str) -> None:
            nonlocal calls
            if label.endswith("parent directory"):
                calls += 1
                if calls <= 2:
                    raise OSError(f"injected parent fsync failure {calls}")
            original(path, label)

        final = self._bundle("oss")
        before = {path.name: path.read_bytes() for path in final.iterdir()}
        with mock.patch.object(dev_bundle, "_fsync_directory", side_effect=fail_twice):
            with self.assertRaises(ValueError) as failure:
                self._bundle("oss")
        self.assertIn("injected parent fsync failure 1", str(failure.exception))
        self.assertIn("injected parent fsync failure 2", str(failure.exception))
        self.assertEqual(
            {path.name: path.read_bytes() for path in final.iterdir()},
            before,
        )
        self.assertEqual(
            [
                path.name
                for path in final.parent.iterdir()
                if path.name.startswith(".020_linux_mailbox.tmp-")
                or path.name.startswith(".020_linux_mailbox.old-")
            ],
            [],
        )

    def test_rejects_dirty_stale_and_mismatched_inputs_without_touching_output(self) -> None:
        from scripts import dev_bundle

        bundle = self._bundle("oss")
        original = (self.root / "boards/de10nano/pins.qsf").read_bytes()
        pins = self.root / "boards/de10nano/pins.qsf"
        pins.write_bytes(original + b"dirty\n")
        with mock.patch.object(dev_bundle, "REPO_ROOT", self.root, create=True):
            with self.assertRaises(ValueError):
                dev_bundle.build_bundle("020_linux_mailbox", "oss", RUN_ID)

        pins.write_bytes(original)
        before = {path.name: path.read_bytes() for path in bundle.iterdir()}

        manifest_path = self.lane_paths["oss"]["dir"] / "manifest.json"
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        manifest["git"]["commit"] = "0" * 40
        manifest_path.write_text(json.dumps(manifest) + "\n", encoding="utf-8")
        with mock.patch.object(dev_bundle, "REPO_ROOT", self.root, create=True):
            with self.assertRaises(ValueError):
                dev_bundle.build_bundle("020_linux_mailbox", "oss", RUN_ID)

        self.assertEqual({path.name: path.read_bytes() for path in bundle.iterdir()}, before)

        self._write_lane_manifest("oss")
        real_rbf = self.lane_paths["oss"]["dir"] / "top.real.rbf"
        real_rbf.write_bytes(self.lane_paths["oss"]["rbf"].read_bytes())
        self.lane_paths["oss"]["rbf"].unlink()
        self.lane_paths["oss"]["rbf"].symlink_to(real_rbf.name)
        with mock.patch.object(dev_bundle, "REPO_ROOT", self.root, create=True):
            with self.assertRaises(ValueError):
                dev_bundle.build_bundle("020_linux_mailbox", "oss", RUN_ID)

    def test_rejects_manifest_report_and_rbf_symlinks(self) -> None:
        from scripts import dev_bundle

        self._bundle("oss")
        manifest_path = self.lane_paths["oss"]["dir"] / "manifest.json"
        report_path = self.lane_paths["oss"]["report"]
        rbf_path = self.lane_paths["oss"]["rbf"]
        for path in (manifest_path, report_path, rbf_path):
            with self.subTest(path=path.name):
                real_path = path.with_name(path.name + ".real")
                path.rename(real_path)
                path.symlink_to(real_path.name)
                try:
                    with mock.patch.object(dev_bundle, "REPO_ROOT", self.root, create=True):
                        with self.assertRaises(ValueError):
                            dev_bundle.build_bundle("020_linux_mailbox", "oss", RUN_ID)
                finally:
                    path.unlink()
                    real_path.rename(path)

    def test_rejects_non_passing_status_and_run_id_before_output(self) -> None:
        from scripts import dev_bundle

        manifest_path = self.lane_paths["oracle"]["dir"] / "manifest.json"
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
        manifest["build"]["timing"]["status"] = "fail"
        manifest_path.write_text(json.dumps(manifest) + "\n", encoding="utf-8")
        with mock.patch.object(dev_bundle, "REPO_ROOT", self.root, create=True):
            with self.assertRaises(ValueError):
                dev_bundle.build_bundle("020_linux_mailbox", "oracle", RUN_ID)
            for invalid in ("", "A" * 32, "g" * 32, RUN_ID + "0", 1):
                with self.subTest(invalid=invalid):
                    with self.assertRaises(ValueError):
                        dev_bundle.build_bundle("020_linux_mailbox", "oracle", invalid)  # type: ignore[arg-type]

    def test_independent_artifact_manifest_parser_accepts_only_exact_contract(self) -> None:
        bundle = self._bundle("oracle")
        payload = (bundle / "manifest.json").read_bytes()

        def parse(raw: bytes) -> dict[str, object]:
            self.assertTrue(raw.endswith(b"\n"))
            self.assertEqual(raw.count(b"\n"), 1)
            pairs: list[tuple[str, object]] = []

            def collect(items: list[tuple[str, object]]) -> dict[str, object]:
                pairs.extend(items)
                return dict(items)

            value = json.loads(raw.decode("utf-8"), object_pairs_hook=collect)
            self.assertEqual([key for key, _ in pairs], [
                "schema", "run_id", "experiment", "board", "build_lane",
                "artifact_filename", "artifact_size", "artifact_sha256", "source_commit",
            ])
            self.assertIs(type(value["schema"]), int)
            self.assertIs(type(value["artifact_size"]), int)
            self.assertIs(type(value["run_id"]), str)
            self.assertIs(type(value["artifact_sha256"]), str)
            self.assertIs(type(value["source_commit"]), str)
            return value

        value = parse(payload)
        self.assertEqual(value["build_lane"], "oracle")
        self.assertEqual(value["artifact_filename"], "top.rbf")
        self.assertEqual(value["source_commit"], self.commit)

    def test_independent_artifact_parser_rejects_full_raw_manifest_matrix(self) -> None:
        bundle = self._bundle("oracle")
        payload = (bundle / "manifest.json").read_bytes()
        valid = json.loads(payload)
        artifact_bytes = self.lane_paths["oracle"]["rbf"].read_bytes()

        def parse(raw: bytes) -> None:
            _parse_artifact_manifest_independently(
                raw,
                expected_commit=self.commit,
                expected_lane="oracle",
                artifact_bytes=artifact_bytes,
            )

        parse(payload)

        def encode_pairs(pairs: list[tuple[str, object]]) -> bytes:
            fields = [
                json.dumps(key, ensure_ascii=True, separators=(",", ":"))
                + ":"
                + json.dumps(value, ensure_ascii=True, separators=(",", ":"))
                for key, value in pairs
            ]
            return ("{" + ",".join(fields) + "}\n").encode("utf-8")

        cases: list[tuple[str, bytes]] = []
        for field, invalid in (
            ("schema", True),
            ("run_id", "A" * 32),
            ("experiment", "010_blinky"),
            ("board", "de10nano"),
            ("build_lane", "oss"),
            ("artifact_filename", "top.bit"),
            ("artifact_size", True),
            ("artifact_sha256", "0" * 64),
            ("source_commit", "0" * 40),
        ):
            changed = dict(valid)
            changed[field] = invalid
            cases.append((f"wrong {field}", encode_pairs(list(changed.items()))))
        cases.extend(
            (
                ("unknown field", encode_pairs([*valid.items(), ("unknown", 1)])),
                ("duplicate field", encode_pairs([*valid.items(), ("schema", 1)])),
                ("reordered fields", encode_pairs(list(reversed(list(valid.items()))))),
                ("trailing whitespace", payload[:-1] + b" \n"),
                ("missing newline", payload[:-1]),
                ("extra newline", payload + b"\n"),
                ("trailing field", payload[:-2] + b',"extra":1}\n'),
            )
        )
        for name, malformed in cases:
            with self.subTest(manifest=name):
                with self.assertRaises(ValueError):
                    parse(malformed)

    def test_independent_checksum_parser_rejects_full_membership_matrix(self) -> None:
        bundle = self._bundle("oss")
        members = {
            name: (bundle / name).read_bytes()
            for name in ("manifest.json", "resource_evidence.json", "top.rbf")
        }
        valid = (bundle / "bundle.sha256").read_bytes()
        self.assertEqual(
            _parse_checksums_independently(valid, members),
            ["manifest.json", "resource_evidence.json", "top.rbf"],
        )
        lines = valid.splitlines()
        cases = {
            "missing": lines[:2],
            "extra": [*lines, b"0" * 64 + b"  unexpected"],
            "duplicate": [lines[0], lines[0], lines[2]],
            "wrong order": [lines[1], lines[0], lines[2]],
            "wrong hash": [b"0" * 64 + lines[0][64:], lines[1], lines[2]],
        }
        for name, malformed_lines in cases.items():
            with self.subTest(checksum=name):
                with self.assertRaises(ValueError):
                    _parse_checksums_independently(
                        b"\n".join(malformed_lines) + b"\n",
                        members,
                    )


class MakeBundleTests(unittest.TestCase):
    def test_make_passes_run_id_as_one_argument_and_omits_empty_option(self) -> None:
        root = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory(prefix="dev-bundle-make-") as temporary:
            directory = Path(temporary)
            recorder = directory / "record.py"
            recorder.write_text(
                "#!/usr/bin/env python3\n"
                "import json\n"
                "import pathlib\n"
                "import sys\n"
                "pathlib.Path(sys.argv[0]).with_suffix('.argv').write_text(json.dumps(sys.argv[1:]))\n",
                encoding="utf-8",
            )
            recorder.chmod(0o700)
            hostile = "0123456789abcdef0123456789abcdef;touch SHOULD_NOT_EXIST"
            result = subprocess.run(
                [
                    "make",
                    "--no-print-directory",
                    "dev-bundle",
                    "EXP=020_linux_mailbox",
                    "BUILD=oracle",
                    f"RUN_ID={hostile}",
                    f"PYTHON={recorder}",
                ],
                cwd=root,
                env=os.environ.copy(),
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(
                json.loads((directory / "record.argv").read_text(encoding="utf-8")),
                [
                    "scripts/dev_bundle.py",
                    "--experiment",
                    "020_linux_mailbox",
                    "--lane",
                    "oracle",
                    "--run-id",
                    hostile,
                ],
            )
            self.assertFalse((root / "SHOULD_NOT_EXIST").exists())

            (directory / "record.argv").unlink()
            result = subprocess.run(
                [
                    "make",
                    "--no-print-directory",
                    "dev-bundle",
                    "EXP=020_linux_mailbox",
                    "BUILD=oss",
                    "RUN_ID=",
                    f"PYTHON={recorder}",
                ],
                cwd=root,
                env=os.environ.copy(),
                text=True,
                capture_output=True,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(
                json.loads((directory / "record.argv").read_text(encoding="utf-8")),
                [
                    "scripts/dev_bundle.py",
                    "--experiment",
                    "020_linux_mailbox",
                    "--lane",
                    "oss",
                ],
            )

    def test_make_rejects_non_mailbox_experiment_and_unknown_lane(self) -> None:
        root = Path(__file__).resolve().parents[1]
        for values in (
            {"EXP": "010_blinky", "BUILD": "oss"},
            {"EXP": "020_linux_mailbox", "BUILD": "quartus"},
        ):
            with self.subTest(values=values):
                result = subprocess.run(
                    ["make", "--no-print-directory", "dev-bundle", *[f"{key}={value}" for key, value in values.items()]],
                    cwd=root,
                    text=True,
                    capture_output=True,
                )
                self.assertNotEqual(result.returncode, 0)


if __name__ == "__main__":
    unittest.main()
