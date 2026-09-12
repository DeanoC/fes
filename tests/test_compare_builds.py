import hashlib
import json
import os
import subprocess
import tempfile
import unittest
from copy import deepcopy
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
COMPARE = ROOT / "scripts" / "compare_builds.py"


def _sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


class CompareBuildsTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.root = Path(self.temp.name)
        self.output = self.root / "comparison"
        self.oss_rbf = self.root / "build" / "oss" / "010_blinky" / "top.rbf"
        self.oracle_rbf = self.root / "build" / "oracle" / "010_blinky" / "top.rbf"
        self.oss_rbf.parent.mkdir(parents=True)
        self.oracle_rbf.parent.mkdir(parents=True)
        self.oss_rbf.write_bytes(b"oss-rbf")
        self.oracle_rbf.write_bytes(b"oracle-rbf")

    def tearDown(self):
        self.temp.cleanup()

    def _manifest(self, lane, rbf, *, status="pass", alm=28, artifact=True, artifact_path=None):
        artifacts = []
        digest = _sha256(rbf) if rbf.exists() else "0" * 64
        if artifact:
            artifacts.append(
                {
                    "path": artifact_path or f"build/{lane}/010_blinky/top.rbf",
                    "sha256": digest,
                }
            )
        sources = [
            {"path": "experiments/010_blinky/rtl/top.v", "sha256": "a" * 64},
            {"path": "boards/de10nano/pins.qsf", "sha256": "b" * 64},
            {"path": "boards/de10nano/clocks.sdc", "sha256": "c" * 64},
            {"path": "experiments/010_blinky/oracle/top.qsf", "sha256": "d" * 64},
        ]
        direct_hard_blocks = {
            "PLL": {"used": 0, "available": 4, "evidence_kind": "fitter_summary", "measured": True},
            "BRAM/M10K": {"used": 0, "available": 10, "evidence_kind": "fitter_summary", "measured": True},
            "DSP": {"used": 0, "available": 2, "evidence_kind": "fitter_summary", "measured": True},
        }
        static_sources = [
            {"path": "experiments/010_blinky/rtl/top.v", "sha256": "a" * 64},
            {"path": "boards/de10nano/pins.qsf", "sha256": "b" * 64},
            {"path": "boards/de10nano/clocks.sdc", "sha256": "c" * 64},
            {"path": "experiments/010_blinky/oracle/top.qsf", "sha256": "d" * 64},
        ]
        oracle_static = {
            "MLAB/LUTRAM": {
                "used": None,
                "available": None,
                "status": "excluded",
                "evidence_kind": "static_exclusion",
                "measured": False,
                "exclusion": {
                    "basis": "static source/project exclusion",
                    "patterns": [
                        r"\bmlab(?:s)?\b",
                        r"\blutram\b",
                        r"\b(?:altsyncram|lpm_ram|mlab_cell)\b",
                        r"\b(?:reg|wire|logic)\s*\[[^\]]+\]\s+\w+\s*\[",
                    ],
                    "sources": static_sources,
                },
            },
            "HPS": {
                "used": None,
                "available": None,
                "status": "excluded",
                "evidence_kind": "static_exclusion",
                "measured": False,
                "exclusion": {
                    "basis": "static source/project exclusion",
                    "patterns": [
                        r"\bhps\b",
                        r"\bhard[_ ]processor",
                        r"\b(?:altera|cyclonev)[_ ]hps\b",
                        r"\b(?:hps_component|soc_system|soc_id|arm)\b",
                        r"\bsoc\b",
                    ],
                    "sources": static_sources,
                },
            },
        }
        hard_blocks = dict(direct_hard_blocks)
        if lane == "oracle":
            hard_blocks.update(oracle_static)
        else:
            hard_blocks.update(
                {
                    "MLAB/LUTRAM": {"used": 0, "available": 8},
                    "HPS": {"used": 0, "available": 1},
                }
            )
        hard_block_evidence = json.loads(json.dumps(hard_blocks))
        provenance = {
            "path": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "executable": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "sha256": "d" * 64,
            "executable_sha256": "d" * 64,
            "version": "Quartus Prime Version 17.0.2 Build 602",
            "required_version": "17.0.2",
            "version_output_sha256": "e" * 64,
        }
        return {
            "schema": 2,
            "experiment": "010_blinky",
            "lane": lane,
            "target": "5CSEBA6U23I7",
            "sources": sources,
            "artifacts": artifacts,
            "build": {
                "status": status,
                "build_status": status,
                "route_status": "pass",
                "route": {"status": "pass", "unrouted": False},
                "timing": {
                    "status": "pass",
                    "requested_mhz": 50.0,
                    "achieved_mhz": 100.0,
                    "clock": "FPGA_CLK1_50",
                },
                "resources": {
                    "ALM": {"used": alm, "available": 100, "utilization_percent": alm}
                },
                "source_hashes": {
                    source["path"]: source["sha256"] for source in sources
                },
                "hard_blocks": hard_blocks,
                "hard_block_evidence": hard_block_evidence,
                "hard_block_status": "pass",
                "unknown_resources": {},
                "simulation": {"status": "pass"},
                "authenticated_tools": {"quartus_sh": provenance},
                "tool_pins": {"quartus": provenance},
                "reproducibility": {
                    "rbf_sha256": digest,
                    "rbf_size_bytes": rbf.stat().st_size if rbf.exists() else 0,
                },
            },
        }

    def _write_manifest(self, name, value):
        path = self.root / name
        path.write_text(json.dumps(value), encoding="utf-8")
        return path

    def _mailbox_manifest(self, lane, rbf, *, timing_status="pass", hps=1, pll=0):
        from scripts.experiment_policy import policy_for
        from scripts.collect_manifest import _tool_pins

        digest = _sha256(rbf)
        report_name = "timing.json" if lane == "oss" else "top.fit.rpt"
        report = rbf.parent / report_name
        report.write_bytes(b"mailbox-synthesis-report\n")
        report_digest = _sha256(report)
        report_path = f"build/{lane}/020_linux_mailbox/{report_name}"
        command_names = (
            ("yosys.log", "nextpnr-help.log", "nextpnr.log")
            if lane == "oss"
            else ("quartus-version.log", "quartus.log")
        )
        command_records = []
        oss_yosys = ROOT / "build" / "toolchain" / "install" / "bin" / "yosys"
        oss_nextpnr = ROOT / "build" / "toolchain" / "install" / "bin" / "nextpnr-mistral"
        for index, command_name in enumerate(command_names):
            command_log = rbf.parent / command_name
            if lane == "oss":
                command_lines = {
                    "yosys.log": (
                        f"command: {oss_yosys} -p "
                        "'read_verilog experiments/020_linux_mailbox/rtl/top.v; "
                        "synth_intel_alm -nobram -nolutram -nodsp -top top; stat; "
                        "write_json build/oss/020_linux_mailbox/synth.json'\n"
                    ),
                    "nextpnr-help.log": f"command: {oss_nextpnr} --help\n",
                    "nextpnr.log": (
                        f"command: {oss_nextpnr} --json "
                        "build/oss/020_linux_mailbox/synth.json --device 5CSEBA6U23I7 "
                        "--qsf boards/de10nano/pins.qsf --sdc boards/de10nano/clocks.sdc "
                        "--freq 50 --rbf build/oss/020_linux_mailbox/top.rbf "
                        "--compress-rbf --write build/oss/020_linux_mailbox/routed.json "
                        "--report build/oss/020_linux_mailbox/timing.json "
                        "--detailed-timing-report\n"
                    ),
                }
                command_log.write_text(command_lines[command_name], encoding="utf-8")
            else:
                command_log.write_text(
                    f"command: trusted-build-{index}\n", encoding="utf-8"
                )
            command_path = f"build/{lane}/020_linux_mailbox/{command_name}"
            command_records.append({"path": command_path, "sha256": _sha256(command_log)})
        policy = policy_for("020_linux_mailbox").as_dict()
        policy_bytes = json.dumps(
            policy, ensure_ascii=True, separators=(",", ":"), sort_keys=True
        ).encode("utf-8")
        policy_hash = hashlib.sha256(policy_bytes).hexdigest()
        source_hashes = {
            path: _sha256(ROOT / path)
            for path in (
                "experiments/020_linux_mailbox/rtl/top.v",
                "boards/de10nano/pins.qsf",
                "boards/de10nano/clocks.sdc",
            )
        }
        if lane == "oracle":
            source_hashes.update({
                path: _sha256(ROOT / path)
                for path in (
                    "experiments/020_linux_mailbox/oracle/top.qpf",
                    "experiments/020_linux_mailbox/oracle/top.qsf",
                )
            })
        static_sources = [
            {"path": path, "sha256": value}
            for path, value in source_hashes.items()
        ]
        static_commands = list(command_records)
        static_mlab = {
            "used": None,
            "available": None,
            "status": "excluded",
            "evidence_kind": "static_exclusion",
            "measured": False,
            "exclusion": {
                "basis": "static source/project/command exclusion",
                "patterns": [
                    r"\bmlab(?:s)?\b",
                    r"\blutram\b",
                    r"\b(?:altsyncram|lpm_ram|mlab_cell)\b",
                    r"\b(?:reg|wire|logic)\s*\[[^\]]+\]\s+\w+\s*\[",
                ],
                "sources": static_sources,
                "commands": static_commands,
                "report": {"path": report_path, "sha256": report_digest},
            },
        }
        direct = {
            "PLL": {
                "used": pll,
                "available": 4,
                "evidence_kind": "fitter_summary",
                "measured": True,
            },
            "BRAM/M10K": {
                "used": 0,
                "available": 10,
                "evidence_kind": "fitter_summary",
                "measured": True,
            },
            "DSP": {
                "used": 0,
                "available": 2,
                "evidence_kind": "fitter_summary",
                "measured": True,
            },
            "cyclonev_hps_interface_mpu_general_purpose": {
                "used": hps,
                "available": 1,
                "evidence_kind": "fitter_summary",
                "measured": True,
            },
        }
        if lane == "oracle":
            hard_blocks = {**direct, "MLAB/LUTRAM": static_mlab}
        else:
            hard_blocks = {
                "cyclonev_hps_interface_mpu_general_purpose": direct[
                    "cyclonev_hps_interface_mpu_general_purpose"
                ],
                "MISTRAL_M10K": {
                    "used": 0,
                    "available": 553,
                },
            }
        semantic = {
            "clock_inputs": 1,
            "external_input_ports": 0,
            "external_output_ports": 0,
            "bidirectional_ports": 0,
            "hps_general_purpose_interfaces": hps,
            "pll_blocks": pll,
            "dsp_blocks": 0,
            "block_memory_bits": 0,
            "lutram_bits": 0,
            "sdram_interfaces": 0,
        }
        provenance = {
            "path": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "executable": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "sha256": "d" * 64,
            "executable_sha256": "d" * 64,
            "version": "Quartus Prime Version 17.0.2 Build 602",
            "required_version": "17.0.2",
            "version_output_sha256": "e" * 64,
        }
        timing = {
            "status": timing_status,
            "requested_mhz": 50.0,
            "achieved_mhz": 130.0 if timing_status == "pass" else 49.0,
            "clock": "protocol.FPGA_CLK1_50" if lane == "oss" else "FPGA_CLK1_50",
        }
        build = {
            "experiment": "020_linux_mailbox",
            "target": "5CSEBA6U23I7",
            "status": timing_status,
            "build_status": timing_status,
            "route_status": "pass",
            "route": {"status": "pass", "unrouted": False},
            "timing": timing,
            "clock_constraint_mhz": 50.0,
            "resources": {"ALM": {"used": 10, "available": 100}},
            "source_hashes": source_hashes,
            "hard_blocks": hard_blocks,
            "hard_block_evidence": deepcopy(hard_blocks),
            "hard_block_status": "pass" if hps == 1 and pll == 0 else "fail",
            "unknown_resources": {},
            "simulation": {"status": "pass"},
            "authenticated_tools": (
                {"quartus_sh": provenance}
                if lane == "oracle"
                else {
                    "yosys": {
                        "commit": "ec34fcf38986217af9b5558936044b7197d968a7",
                        "path": "build/toolchain/install/bin/yosys",
                        "sha256": _sha256(oss_yosys),
                    },
                    "nextpnr-mistral": {
                        "commit": "47c4251acc89eb9bf6742e32204af744a23446e0",
                        "path": "build/toolchain/install/bin/nextpnr-mistral",
                        "sha256": _sha256(oss_nextpnr),
                    },
                }
            ),
            "tool_pins": (
                {"quartus": provenance}
                if lane == "oracle"
                else {
                    "yosys": "ec34fcf38986217af9b5558936044b7197d968a7",
                    "nextpnr": "47c4251acc89eb9bf6742e32204af744a23446e0",
                }
            ),
            "reproducibility": {"rbf_sha256": digest, "rbf_size_bytes": rbf.stat().st_size},
            "experiment_policy": policy,
            "experiment_policy_sha256": policy_hash,
            "protocol_source_sha256": source_hashes[
                "experiments/020_linux_mailbox/rtl/top.v"
            ],
            "protocol_source": "experiments/020_linux_mailbox/rtl/top.v",
            "allowed_hard_blocks": policy["allowed_hard_blocks"],
            "clock_intent": "FPGA_CLK1_50",
            "resource_evidence": semantic,
            "synthesis_report_path": report_path,
            "synthesis_report_sha256": report_digest,
        }
        static_proof = {
            "kind": "static_exclusion",
            "basis": "static source/project/command exclusion",
            "patterns": [
                "PLL",
                "phase_locked",
                "BRAM",
                "M10K",
                "RAM",
                "ram_block",
                "MLAB",
                "LUTRAM",
                "DSP",
                "MAC",
                "MUL",
                "oscillator",
                "SDRAM",
                "video",
                "audio",
                "HPS",
                "MPU",
                "ARM",
            ],
            "sources": static_sources,
            "commands": static_commands,
            "report": {"path": report_path, "sha256": report_digest},
        }
        oss_static_patterns = {
            "pll_blocks": ["PLL", "MISTRAL_PLL", "phase_locked", "altpll"],
            "dsp_blocks": [
                "DSP",
                "MUL",
                "MISTRAL_MUL9X9",
                "MISTRAL_MUL18X18",
                "MISTRAL_MUL27X27",
                "MAC",
            ],
            "block_memory_bits": [
                "BRAM",
                "M10K",
                "MISTRAL_M10K",
                "M20K",
                "RAM",
                "ram_block",
                "altsyncram",
            ],
            "lutram_bits": [
                "MLAB",
                "MISTRAL_MLAB",
                "LUTRAM",
                "altsyncram",
                "lpm_ram",
                "mlab_cell",
                r"re:(?:reg|wire|logic)\s*\[[^\]]+\]\s+\w+\s*\[",
            ],
            "sdram_interfaces": [
                "SDRAM",
                "DDR",
                "cyclonev_hps_interface_fpga2sdram",
                "hps_sdram",
            ],
        }
        observed_resources = {}
        for key in ("resources", "hard_blocks"):
            values = build.get(key)
            if isinstance(values, dict):
                observed_resources.update(values)
        labels = {
            "hps_general_purpose_interfaces": "cyclonev_hps_interface_mpu_general_purpose",
            "pll_blocks": "PLL",
            "dsp_blocks": "DSP",
            "block_memory_bits": "block_memory_bits",
            "lutram_bits": "lutram_bits",
            "sdram_interfaces": "sdram_interfaces",
        }
        fields = {
            field: {
                "kind": "source_port_declaration",
                "source": {
                    "path": "experiments/020_linux_mailbox/rtl/top.v",
                    "sha256": source_hashes["experiments/020_linux_mailbox/rtl/top.v"],
                },
            }
            for field in (
                "clock_inputs",
                "external_input_ports",
                "external_output_ports",
                "bidirectional_ports",
            )
        }
        for field, label in labels.items():
            if label in observed_resources:
                fields[field] = {
                    "kind": "measured_report_row",
                    "label": label,
                    "report": {"path": report_path, "sha256": report_digest},
                }
            else:
                field_static_proof = dict(static_proof)
                if lane == "oss" and field in oss_static_patterns:
                    field_static_proof["patterns"] = oss_static_patterns[field]
                fields[field] = field_static_proof
        resource_provenance = {
            "report": {"path": report_path, "sha256": report_digest},
            "fields": fields,
        }
        tool_pins = _tool_pins(ROOT)
        build["synthesis_report"] = {
            "path": report_path,
            "sha256": report_digest,
        }
        build["resource_evidence_provenance"] = resource_provenance
        return {
            "schema": 2,
            "experiment": "020_linux_mailbox",
            "lane": lane,
            "target": "5CSEBA6U23I7",
            "sources": [
                {"path": path, "sha256": value}
                for path, value in source_hashes.items()
            ],
            "command_logs": command_records,
            "artifacts": [
                {
                    "path": f"build/{lane}/020_linux_mailbox/top.rbf",
                    "sha256": digest,
                },
                {
                    "path": report_path,
                    "sha256": report_digest,
                },
            ],
            "build": build,
            "tool_pins": tool_pins,
            "experiment_policy": policy,
            "experiment_policy_sha256": policy_hash,
            "protocol_source_sha256": build["protocol_source_sha256"],
            "resource_evidence": semantic,
            "synthesis_report_path": report_path,
            "synthesis_report_sha256": report_digest,
            "synthesis_report": {
                "path": report_path,
                "sha256": report_digest,
            },
            "resource_evidence_provenance": resource_provenance,
        }

    def _mlab_manifest(self, lane, rbf, *, lutram_bits=256, hps=1):
        from scripts.experiment_policy import policy_for

        digest = _sha256(rbf)
        policy = policy_for("040_mlab_ram").as_dict()
        source_hashes = {
            path: _sha256(ROOT / path)
            for path in (
                "experiments/040_mlab_ram/rtl/top.v",
                "boards/de10nano/pins.qsf",
                "boards/de10nano/clocks.sdc",
            )
        }
        hps_record = {
            "used": hps,
            "available": 1,
            "evidence_kind": "fitter_summary",
            "measured": True,
        }
        if lane == "oracle":
            hard_blocks = {
                "PLL": {
                    "used": 0,
                    "available": 6,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "BRAM/M10K": {
                    "used": 0,
                    "available": 553,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "DSP": {
                    "used": 0,
                    "available": 112,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "cyclonev_hps_interface_mpu_general_purpose": hps_record,
                "MLAB/LUTRAM": {
                    "used": lutram_bits,
                    "available": None,
                    "utilization_percent": None,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
            }
            resource_evidence = {
                "clock_inputs": 1,
                "external_input_ports": 0,
                "external_output_ports": 0,
                "bidirectional_ports": 0,
                "hps_general_purpose_interfaces": hps,
                "pll_blocks": 0,
                "dsp_blocks": 0,
                "block_memory_bits": 0,
                "lutram_bits": lutram_bits,
                "sdram_interfaces": 0,
            }
            clock = "FPGA_CLK1_50"
            clock_intent = "FPGA_CLK1_50"
            experiment_policy = None
        else:
            hard_blocks = {
                "cyclonev_hps_interface_mpu_general_purpose": {
                    "used": hps,
                    "available": 1,
                    "utilization_percent": 100.0,
                },
                "MISTRAL_M10K": {"used": 0, "available": 553, "utilization_percent": 0.0},
                "cyclonev_oscillator": {
                    "used": 0,
                    "available": 1,
                    "utilization_percent": 0.0,
                },
            }
            resource_evidence = None
            clock = "storage.FPGA_CLK1_50"
            clock_intent = None
            experiment_policy = policy
        provenance = {
            "path": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "executable": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "sha256": "d" * 64,
            "executable_sha256": "d" * 64,
            "version": "Quartus Prime Version 17.0.2 Build 602",
            "required_version": "17.0.2",
            "version_output_sha256": "e" * 64,
        }
        build = {
            "experiment": "040_mlab_ram",
            "target": "5CSEBA6U23I7",
            "status": "pass",
            "build_status": "pass",
            "route_status": "pass",
            "route": {"status": "pass", "unrouted": False},
            "timing": {
                "status": "pass",
                "requested_mhz": 50.0,
                "achieved_mhz": 240.0,
                "clock": clock,
            },
            "clock_constraint_mhz": 50.0 if lane == "oss" else None,
            "clock_intent": clock_intent,
            "resources": {"ALM": {"used": 12, "available": 41910}},
            "source_hashes": source_hashes,
            "hard_blocks": hard_blocks,
            "hard_block_evidence": deepcopy(hard_blocks),
            "hard_block_status": "pass",
            "unknown_resources": {},
            "simulation": {"status": "pass"},
            "allowed_hard_blocks": {
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            "authenticated_tools": {"quartus_sh": provenance} if lane == "oracle" else {},
            "tool_pins": {"quartus": provenance} if lane == "oracle" else {},
            "reproducibility": {
                "rbf_sha256": digest,
                "rbf_size_bytes": rbf.stat().st_size,
            },
        }
        if experiment_policy is not None:
            build["experiment_policy"] = experiment_policy
        if resource_evidence is not None:
            build["resource_evidence"] = resource_evidence
        if clock_intent is None:
            build.pop("clock_intent")
        if build["clock_constraint_mhz"] is None:
            build.pop("clock_constraint_mhz")
        return {
            "schema": 2,
            "experiment": "040_mlab_ram",
            "lane": lane,
            "target": "5CSEBA6U23I7",
            "sources": [
                {"path": path, "sha256": value}
                for path, value in source_hashes.items()
            ],
            "artifacts": [
                {
                    "path": f"build/{lane}/040_mlab_ram/top.rbf",
                    "sha256": digest,
                }
            ],
            "build": build,
        }

    def _lut_mul_manifest(self, lane, rbf, *, dsp_blocks=0, hps=1):
        from scripts.experiment_policy import policy_for

        digest = _sha256(rbf)
        policy = policy_for("050_lut_mul").as_dict()
        source_hashes = {
            path: _sha256(ROOT / path)
            for path in (
                "experiments/050_lut_mul/rtl/top.v",
                "boards/de10nano/pins.qsf",
                "boards/de10nano/clocks.sdc",
            )
        }
        if lane == "oracle":
            source_hashes["experiments/050_lut_mul/oracle/top.qsf"] = _sha256(
                ROOT / "experiments/050_lut_mul/oracle/top.qsf"
            )
        hps_record = {
            "used": hps,
            "available": 1,
            "evidence_kind": "fitter_summary",
            "measured": True,
        }
        static_mlab = {
            "used": None,
            "available": None,
            "status": "excluded",
            "evidence_kind": "static_exclusion",
            "measured": False,
            "exclusion": {
                "basis": "static source/project exclusion",
                "patterns": [
                    r"\bmlab(?:s)?\b",
                    r"\blutram\b",
                    r"\b(?:altsyncram|lpm_ram|mlab_cell)\b",
                    r"\b(?:reg|wire|logic)\s*\[[^\]]+\]\s+\w+\s*\[",
                ],
                "sources": [
                    {"path": path, "sha256": value}
                    for path, value in source_hashes.items()
                ],
            },
        }
        if lane == "oracle":
            hard_blocks = {
                "PLL": {
                    "used": 0,
                    "available": 6,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "BRAM/M10K": {
                    "used": 0,
                    "available": 553,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "DSP": {
                    "used": dsp_blocks,
                    "available": 112,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "cyclonev_hps_interface_mpu_general_purpose": hps_record,
                "MLAB/LUTRAM": static_mlab,
            }
            resource_evidence = {
                "clock_inputs": 1,
                "external_input_ports": 0,
                "external_output_ports": 0,
                "bidirectional_ports": 0,
                "hps_general_purpose_interfaces": hps,
                "pll_blocks": 0,
                "dsp_blocks": dsp_blocks,
                "block_memory_bits": 0,
                "lutram_bits": 0,
                "sdram_interfaces": 0,
            }
            clock = "FPGA_CLK1_50"
            clock_intent = "FPGA_CLK1_50"
            experiment_policy = None
        else:
            hard_blocks = {
                "cyclonev_hps_interface_mpu_general_purpose": {
                    "used": hps,
                    "available": 1,
                    "utilization_percent": 100.0,
                },
                "MISTRAL_M10K": {"used": 0, "available": 553, "utilization_percent": 0.0},
                "cyclonev_oscillator": {
                    "used": 0,
                    "available": 1,
                    "utilization_percent": 0.0,
                },
            }
            resource_evidence = None
            clock = "product.FPGA_CLK1_50"
            clock_intent = None
            experiment_policy = policy
        provenance = {
            "path": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "executable": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "sha256": "d" * 64,
            "executable_sha256": "d" * 64,
            "version": "Quartus Prime Version 17.0.2 Build 602",
            "required_version": "17.0.2",
            "version_output_sha256": "e" * 64,
        }
        build = {
            "experiment": "050_lut_mul",
            "target": "5CSEBA6U23I7",
            "status": "pass",
            "build_status": "pass",
            "route_status": "pass",
            "route": {"status": "pass", "unrouted": False},
            "timing": {
                "status": "pass",
                "requested_mhz": 50.0,
                "achieved_mhz": 180.0,
                "clock": clock,
            },
            "clock_constraint_mhz": 50.0 if lane == "oss" else None,
            "clock_intent": clock_intent,
            "resources": {"ALM": {"used": 40, "available": 41910}},
            "source_hashes": source_hashes,
            "hard_blocks": hard_blocks,
            "hard_block_evidence": deepcopy(hard_blocks),
            "hard_block_status": "pass" if dsp_blocks == 0 and hps == 1 else "fail",
            "unknown_resources": {},
            "simulation": {"status": "pass"},
            "allowed_hard_blocks": {
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            "authenticated_tools": {"quartus_sh": provenance} if lane == "oracle" else {},
            "tool_pins": {"quartus": provenance} if lane == "oracle" else {},
            "reproducibility": {
                "rbf_sha256": digest,
                "rbf_size_bytes": rbf.stat().st_size,
            },
        }
        if experiment_policy is not None:
            build["experiment_policy"] = experiment_policy
        if resource_evidence is not None:
            build["resource_evidence"] = resource_evidence
        if clock_intent is None:
            build.pop("clock_intent")
        if build["clock_constraint_mhz"] is None:
            build.pop("clock_constraint_mhz")
        return {
            "schema": 2,
            "experiment": "050_lut_mul",
            "lane": lane,
            "target": "5CSEBA6U23I7",
            "sources": [
                {"path": path, "sha256": value}
                for path, value in source_hashes.items()
            ],
            "artifacts": [
                {
                    "path": f"build/{lane}/050_lut_mul/top.rbf",
                    "sha256": digest,
                }
            ],
            "build": build,
        }

    def _dsp_mul_manifest(self, lane, rbf, *, dsp_blocks=1, hps=1):
        from scripts.experiment_policy import policy_for

        digest = _sha256(rbf)
        policy = policy_for("060_dsp_mul").as_dict()
        source_hashes = {
            path: _sha256(ROOT / path)
            for path in (
                "experiments/060_dsp_mul/rtl/top.v",
                "boards/de10nano/pins.qsf",
                "boards/de10nano/clocks.sdc",
            )
        }
        if lane == "oracle":
            source_hashes["experiments/060_dsp_mul/oracle/top.qsf"] = _sha256(
                ROOT / "experiments/060_dsp_mul/oracle/top.qsf"
            )
        hps_record = {
            "used": hps,
            "available": 1,
            "evidence_kind": "fitter_summary",
            "measured": True,
        }
        static_mlab = {
            "used": None,
            "available": None,
            "status": "excluded",
            "evidence_kind": "static_exclusion",
            "measured": False,
            "exclusion": {
                "basis": "static source/project exclusion",
                "patterns": [
                    r"\bmlab(?:s)?\b",
                    r"\blutram\b",
                    r"\b(?:altsyncram|lpm_ram|mlab_cell)\b",
                    r"\b(?:reg|wire|logic)\s*\[[^\]]+\]\s+\w+\s*\[",
                ],
                "sources": [
                    {"path": path, "sha256": value}
                    for path, value in source_hashes.items()
                ],
            },
        }
        if lane == "oracle":
            hard_blocks = {
                "PLL": {
                    "used": 0,
                    "available": 6,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "BRAM/M10K": {
                    "used": 0,
                    "available": 553,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "DSP": {
                    "used": dsp_blocks,
                    "available": 112,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "cyclonev_hps_interface_mpu_general_purpose": hps_record,
                "MLAB/LUTRAM": static_mlab,
            }
            resource_evidence = {
                "clock_inputs": 1,
                "external_input_ports": 0,
                "external_output_ports": 0,
                "bidirectional_ports": 0,
                "hps_general_purpose_interfaces": hps,
                "pll_blocks": 0,
                "dsp_blocks": dsp_blocks,
                "block_memory_bits": 0,
                "lutram_bits": 0,
                "sdram_interfaces": 0,
            }
            clock = "FPGA_CLK1_50"
            clock_intent = "FPGA_CLK1_50"
            experiment_policy = None
        else:
            hard_blocks = {
                "cyclonev_hps_interface_mpu_general_purpose": {
                    "used": hps,
                    "available": 1,
                    "utilization_percent": 100.0,
                },
                "MISTRAL_MUL9X9": {"used": 1, "available": 112, "utilization_percent": 0.89},
                "MISTRAL_M10K": {"used": 0, "available": 553, "utilization_percent": 0.0},
                "cyclonev_oscillator": {
                    "used": 0,
                    "available": 1,
                    "utilization_percent": 0.0,
                },
            }
            resource_evidence = None
            clock = "product.FPGA_CLK1_50"
            clock_intent = None
            experiment_policy = policy
        provenance = {
            "path": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "executable": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "sha256": "d" * 64,
            "executable_sha256": "d" * 64,
            "version": "Quartus Prime Version 17.0.2 Build 602",
            "required_version": "17.0.2",
            "version_output_sha256": "e" * 64,
        }
        build = {
            "experiment": "060_dsp_mul",
            "target": "5CSEBA6U23I7",
            "status": "pass",
            "build_status": "pass",
            "route_status": "pass",
            "route": {"status": "pass", "unrouted": False},
            "timing": {
                "status": "pass",
                "requested_mhz": 50.0,
                "achieved_mhz": 180.0,
                "clock": clock,
            },
            "clock_constraint_mhz": 50.0 if lane == "oss" else None,
            "clock_intent": clock_intent,
            "resources": {"ALM": {"used": 20, "available": 41910}},
            "source_hashes": source_hashes,
            "hard_blocks": hard_blocks,
            "hard_block_evidence": deepcopy(hard_blocks),
            "hard_block_status": "pass" if dsp_blocks == 1 and hps == 1 else "fail",
            "unknown_resources": {},
            "simulation": {"status": "pass"},
            "allowed_hard_blocks": {
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_MUL9X9": 1,
            },
            "authenticated_tools": {"quartus_sh": provenance} if lane == "oracle" else {},
            "tool_pins": {"quartus": provenance} if lane == "oracle" else {},
            "reproducibility": {
                "rbf_sha256": digest,
                "rbf_size_bytes": rbf.stat().st_size,
            },
        }
        if experiment_policy is not None:
            build["experiment_policy"] = experiment_policy
        if resource_evidence is not None:
            build["resource_evidence"] = resource_evidence
        if clock_intent is None:
            build.pop("clock_intent")
        if build["clock_constraint_mhz"] is None:
            build.pop("clock_constraint_mhz")
        return {
            "schema": 2,
            "experiment": "060_dsp_mul",
            "lane": lane,
            "target": "5CSEBA6U23I7",
            "sources": [
                {"path": path, "sha256": value}
                for path, value in source_hashes.items()
            ],
            "artifacts": [
                {
                    "path": f"build/{lane}/060_dsp_mul/top.rbf",
                    "sha256": digest,
                }
            ],
            "build": build,
        }

    def _mixed_mem_manifest(self, lane, rbf, *, lutram_bits=256, block_memory_bits=2048, ram_blocks=1, hps=1):
        from scripts.experiment_policy import policy_for

        digest = _sha256(rbf)
        policy = policy_for("070_mixed_mem").as_dict()
        source_hashes = {
            path: _sha256(ROOT / path)
            for path in (
                "experiments/070_mixed_mem/rtl/top.v",
                "boards/de10nano/pins.qsf",
                "boards/de10nano/clocks.sdc",
            )
        }
        if lane == "oracle":
            source_hashes["experiments/070_mixed_mem/oracle/top.qsf"] = _sha256(
                ROOT / "experiments/070_mixed_mem/oracle/top.qsf"
            )
        hps_record = {
            "used": hps,
            "available": 1,
            "evidence_kind": "fitter_summary",
            "measured": True,
        }
        if lane == "oracle":
            hard_blocks = {
                "PLL": {
                    "used": 0,
                    "available": 6,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "BRAM/M10K": {
                    "used": ram_blocks,
                    "available": 553,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "DSP": {
                    "used": 0,
                    "available": 112,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "cyclonev_hps_interface_mpu_general_purpose": hps_record,
                "MLAB/LUTRAM": {
                    "used": lutram_bits,
                    "available": None,
                    "utilization_percent": None,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
            }
            resource_evidence = {
                "clock_inputs": 1,
                "external_input_ports": 0,
                "external_output_ports": 0,
                "bidirectional_ports": 0,
                "hps_general_purpose_interfaces": hps,
                "pll_blocks": 0,
                "dsp_blocks": 0,
                "block_memory_bits": block_memory_bits,
                "lutram_bits": lutram_bits,
                "sdram_interfaces": 0,
            }
            clock = "FPGA_CLK1_50"
            experiment_policy = None
        else:
            hard_blocks = {
                "cyclonev_hps_interface_mpu_general_purpose": {
                    "used": hps,
                    "available": 1,
                    "utilization_percent": 100.0,
                },
                "MISTRAL_M10K": {"used": ram_blocks, "available": 553, "utilization_percent": 0.18},
                "cyclonev_oscillator": {
                    "used": 0,
                    "available": 1,
                    "utilization_percent": 0.0,
                },
            }
            resource_evidence = None
            clock = "storage.FPGA_CLK1_50"
            experiment_policy = policy
        provenance = {
            "path": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "executable": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "sha256": "d" * 64,
            "executable_sha256": "d" * 64,
            "version": "Quartus Prime Version 17.0.2 Build 602",
            "required_version": "17.0.2",
            "version_output_sha256": "e" * 64,
        }
        build = {
            "experiment": "070_mixed_mem",
            "target": "5CSEBA6U23I7",
            "status": "pass",
            "build_status": "pass",
            "route_status": "pass",
            "route": {"status": "pass", "unrouted": False},
            "timing": {
                "status": "pass",
                "requested_mhz": 50.0,
                "achieved_mhz": 180.0,
                "clock": clock,
            },
            "clock_constraint_mhz": 50.0 if lane == "oss" else None,
            "clock_intent": "FPGA_CLK1_50" if lane == "oracle" else None,
            "resources": {"ALM": {"used": 20, "available": 41910}},
            "source_hashes": source_hashes,
            "hard_blocks": hard_blocks,
            "hard_block_evidence": deepcopy(hard_blocks),
            "hard_block_status": "pass" if ram_blocks == 1 and lutram_bits == 256 and hps == 1 else "fail",
            "unknown_resources": {},
            "simulation": {"status": "pass"},
            "allowed_hard_blocks": {
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K": 1,
            },
            "authenticated_tools": {"quartus_sh": provenance} if lane == "oracle" else {},
            "tool_pins": {"quartus": provenance} if lane == "oracle" else {},
            "reproducibility": {
                "rbf_sha256": digest,
                "rbf_size_bytes": rbf.stat().st_size,
            },
        }
        if experiment_policy is not None:
            build["experiment_policy"] = experiment_policy
        if resource_evidence is not None:
            build["resource_evidence"] = resource_evidence
        if build["clock_intent"] is None:
            build.pop("clock_intent")
        if build["clock_constraint_mhz"] is None:
            build.pop("clock_constraint_mhz")
        return {
            "schema": 2,
            "experiment": "070_mixed_mem",
            "lane": lane,
            "target": "5CSEBA6U23I7",
            "sources": [
                {"path": path, "sha256": value}
                for path, value in source_hashes.items()
            ],
            "artifacts": [
                {
                    "path": f"build/{lane}/070_mixed_mem/top.rbf",
                    "sha256": digest,
                }
            ],
            "build": build,
        }

    def test_oss_authenticated_tool_path_must_be_canonical_repo_toolchain_path(self):
        canonical_paths = {
            "yosys": ROOT / "build" / "toolchain" / "install" / "bin" / "yosys",
            "nextpnr-mistral": ROOT / "build" / "toolchain" / "install" / "bin" / "nextpnr-mistral",
        }
        command_names = {
            "yosys": ("yosys.log",),
            "nextpnr-mistral": ("nextpnr-help.log", "nextpnr.log"),
        }
        for tool, canonical in canonical_paths.items():
            with self.subTest(tool=tool):
                oss_rbf = self.root / "build" / "oss" / "020_linux_mailbox" / "top.rbf"
                oracle_rbf = self.root / "build" / "oracle" / "020_linux_mailbox" / "top.rbf"
                oss_rbf.parent.mkdir(parents=True, exist_ok=True)
                oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
                oss_rbf.write_bytes(b"mailbox-oss")
                oracle_rbf.write_bytes(b"mailbox-oracle")
                oss_value = self._mailbox_manifest("oss", oss_rbf)
                oracle_value = self._mailbox_manifest("oracle", oracle_rbf)

                copy_dir = ROOT / "build" / f"task3-round4-same-basename-{os.getpid()}-{tool}"
                copy_dir.mkdir(parents=True, exist_ok=False)
                copy_path = copy_dir / tool
                try:
                    # A distinct executable with the same basename is enough to
                    # exercise path provenance without copying a 360 MiB binary.
                    copy_path.write_text("#!/bin/sh\nexit 0\n", encoding="utf-8")
                    copy_path.chmod(0o755)
                    relative_copy = copy_path.relative_to(ROOT).as_posix()
                    auth = oss_value["build"]["authenticated_tools"][tool]
                    auth["path"] = relative_copy
                    auth["sha256"] = _sha256(copy_path)
                    for command_name in command_names[tool]:
                        command_path = oss_rbf.parent / command_name
                        command_path.write_text(
                            command_path.read_text(encoding="utf-8").replace(
                                str(canonical), str(copy_path), 1
                            ),
                            encoding="utf-8",
                        )
                        command_record_path = f"build/oss/020_linux_mailbox/{command_name}"
                        command_digest = _sha256(command_path)

                        def refresh(node):
                            if isinstance(node, dict):
                                if node.get("path") == command_record_path and "sha256" in node:
                                    node["sha256"] = command_digest
                                for child in node.values():
                                    refresh(child)
                            elif isinstance(node, list):
                                for child in node:
                                    refresh(child)

                        refresh(oss_value)
                    oss = self._write_manifest("mailbox-oss.json", oss_value)
                    oracle = self._write_manifest("mailbox-oracle.json", oracle_value)
                    result = self._run(oss, oracle, experiment="020_linux_mailbox")
                    self.assertNotEqual(result.returncode, 0)
                    comparison = json.loads((self.output / "comparison.json").read_text())
                    self.assertTrue(
                        any(
                            "canonical" in item.lower() or "path" in item.lower()
                            for item in comparison["failures"]
                        ),
                        comparison["failures"],
                    )
                finally:
                    copy_path.unlink(missing_ok=True)
                    copy_dir.rmdir()

    def test_oss_tool_authentication_must_bind_canonical_manifest_and_lock_pins(self):
        for tool, lock_name in (("yosys", "yosys"), ("nextpnr-mistral", "nextpnr")):
            with self.subTest(tool=tool):
                oss_rbf = self.root / "build" / "oss" / "020_linux_mailbox" / "top.rbf"
                oracle_rbf = self.root / "build" / "oracle" / "020_linux_mailbox" / "top.rbf"
                oss_rbf.parent.mkdir(parents=True, exist_ok=True)
                oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
                oss_rbf.write_bytes(b"mailbox-oss")
                oracle_rbf.write_bytes(b"mailbox-oracle")
                oss_value = self._mailbox_manifest("oss", oss_rbf)
                oracle_value = self._mailbox_manifest("oracle", oracle_rbf)
                forged_commit = "f" * 40
                oss_value["build"]["authenticated_tools"][tool]["commit"] = forged_commit
                oss_value["build"]["tool_pins"][lock_name] = forged_commit
                oss_value["tool_pins"][lock_name]["commit"] = forged_commit
                oss = self._write_manifest("mailbox-oss.json", oss_value)
                oracle = self._write_manifest("mailbox-oracle.json", oracle_value)
                result = self._run(oss, oracle, experiment="020_linux_mailbox")
                self.assertNotEqual(result.returncode, 0)
                comparison = json.loads((self.output / "comparison.json").read_text())
                self.assertTrue(
                    any("lock" in item.lower() or "canonical" in item.lower() for item in comparison["failures"]),
                    comparison["failures"],
                )

    def test_mailbox_semantic_comparison_accepts_policy_protocol_and_distinct_rbf_hashes(self):
        oss_rbf = self.root / "build" / "oss" / "020_linux_mailbox" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "020_linux_mailbox" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"mailbox-oss")
        oracle_rbf.write_bytes(b"mailbox-oracle")
        oss = self._write_manifest("mailbox-oss.json", self._mailbox_manifest("oss", oss_rbf))
        oracle = self._write_manifest(
            "mailbox-oracle.json", self._mailbox_manifest("oracle", oracle_rbf)
        )
        result = self._run(oss, oracle, experiment="020_linux_mailbox")
        self.assertEqual(result.returncode, 0, result.stderr)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertEqual(comparison["status"], "pass")
        self.assertNotEqual(
            comparison["lanes"]["oss"]["rbf_sha256"],
            comparison["lanes"]["oracle"]["rbf_sha256"],
        )

    def test_mlab_comparison_accepts_measured_lutram_bits_and_hps(self):
        oss_rbf = self.root / "build" / "oss" / "040_mlab_ram" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "040_mlab_ram" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"mlab-oss")
        oracle_rbf.write_bytes(b"mlab-oracle")
        oss = self._write_manifest("mlab-oss.json", self._mlab_manifest("oss", oss_rbf))
        oracle = self._write_manifest(
            "mlab-oracle.json", self._mlab_manifest("oracle", oracle_rbf)
        )
        result = self._run(oss, oracle, experiment="040_mlab_ram")
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertEqual(comparison["status"], "pass", comparison.get("failures"))
        self.assertEqual(
            comparison["lanes"]["oracle"]["hard_blocks"]["MLAB/LUTRAM"]["used"],
            256,
        )

    def test_mlab_comparison_rejects_m10k_or_missing_lutram(self):
        oss_rbf = self.root / "build" / "oss" / "040_mlab_ram" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "040_mlab_ram" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"mlab-oss-fail")
        oracle_rbf.write_bytes(b"mlab-oracle-fail")
        oss_value = self._mlab_manifest("oss", oss_rbf)
        oracle_value = self._mlab_manifest("oracle", oracle_rbf, lutram_bits=0)
        oss = self._write_manifest("mlab-oss.json", oss_value)
        oracle = self._write_manifest("mlab-oracle.json", oracle_value)
        result = self._run(oss, oracle, experiment="040_mlab_ram")
        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(
            any("lutram" in item.lower() or "mlab" in item.lower() for item in comparison["failures"]),
            comparison["failures"],
        )

    def test_lut_mul_comparison_accepts_zero_dsp_and_hps(self):
        oss_rbf = self.root / "build" / "oss" / "050_lut_mul" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "050_lut_mul" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"lut-mul-oss")
        oracle_rbf.write_bytes(b"lut-mul-oracle")
        oss = self._write_manifest("lut-mul-oss.json", self._lut_mul_manifest("oss", oss_rbf))
        oracle = self._write_manifest(
            "lut-mul-oracle.json", self._lut_mul_manifest("oracle", oracle_rbf)
        )
        result = self._run(oss, oracle, experiment="050_lut_mul")
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertEqual(comparison["status"], "pass", comparison.get("failures"))
        self.assertEqual(comparison["lanes"]["oracle"]["hard_blocks"]["DSP"]["used"], 0)
        self.assertEqual(
            comparison["lanes"]["oracle"]["hard_blocks"]["MLAB/LUTRAM"]["status"],
            "excluded",
        )

    def test_lut_mul_comparison_rejects_measured_dsp(self):
        oss_rbf = self.root / "build" / "oss" / "050_lut_mul" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "050_lut_mul" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"lut-mul-oss-fail")
        oracle_rbf.write_bytes(b"lut-mul-oracle-fail")
        oss_value = self._lut_mul_manifest("oss", oss_rbf)
        oracle_value = self._lut_mul_manifest("oracle", oracle_rbf, dsp_blocks=1)
        oss = self._write_manifest("lut-mul-oss.json", oss_value)
        oracle = self._write_manifest("lut-mul-oracle.json", oracle_value)
        result = self._run(oss, oracle, experiment="050_lut_mul")
        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(
            any("dsp" in item.lower() for item in comparison["failures"]),
            comparison["failures"],
        )

    def test_dsp_mul_comparison_accepts_one_dsp_and_hps(self):
        oss_rbf = self.root / "build" / "oss" / "060_dsp_mul" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "060_dsp_mul" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"dsp-mul-oss")
        oracle_rbf.write_bytes(b"dsp-mul-oracle")
        oss = self._write_manifest("dsp-mul-oss.json", self._dsp_mul_manifest("oss", oss_rbf))
        oracle = self._write_manifest(
            "dsp-mul-oracle.json", self._dsp_mul_manifest("oracle", oracle_rbf)
        )
        result = self._run(oss, oracle, experiment="060_dsp_mul")
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertEqual(comparison["status"], "pass", comparison.get("failures"))
        self.assertEqual(comparison["lanes"]["oracle"]["hard_blocks"]["DSP"]["used"], 1)

    def test_dsp_mul_comparison_rejects_zero_dsp(self):
        oss_rbf = self.root / "build" / "oss" / "060_dsp_mul" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "060_dsp_mul" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"dsp-mul-oss-fail")
        oracle_rbf.write_bytes(b"dsp-mul-oracle-fail")
        oss_value = self._dsp_mul_manifest("oss", oss_rbf)
        oracle_value = self._dsp_mul_manifest("oracle", oracle_rbf, dsp_blocks=0)
        oss = self._write_manifest("dsp-mul-oss.json", oss_value)
        oracle = self._write_manifest("dsp-mul-oracle.json", oracle_value)
        result = self._run(oss, oracle, experiment="060_dsp_mul")
        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(
            any("dsp" in item.lower() for item in comparison["failures"]),
            comparison["failures"],
        )

    def test_mixed_mem_comparison_accepts_block_lab_and_hps(self):
        oss_rbf = self.root / "build" / "oss" / "070_mixed_mem" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "070_mixed_mem" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"mixed-oss")
        oracle_rbf.write_bytes(b"mixed-oracle")
        oss = self._write_manifest("mixed-oss.json", self._mixed_mem_manifest("oss", oss_rbf))
        oracle = self._write_manifest(
            "mixed-oracle.json", self._mixed_mem_manifest("oracle", oracle_rbf)
        )
        result = self._run(oss, oracle, experiment="070_mixed_mem")
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertEqual(comparison["status"], "pass", comparison.get("failures"))
        self.assertEqual(comparison["lanes"]["oracle"]["hard_blocks"]["BRAM/M10K"]["used"], 1)
        self.assertEqual(comparison["lanes"]["oracle"]["hard_blocks"]["MLAB/LUTRAM"]["used"], 256)

    def test_mixed_mem_comparison_rejects_missing_block(self):
        oss_rbf = self.root / "build" / "oss" / "070_mixed_mem" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "070_mixed_mem" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"mixed-oss-fail")
        oracle_rbf.write_bytes(b"mixed-oracle-fail")
        oss = self._write_manifest("mixed-oss.json", self._mixed_mem_manifest("oss", oss_rbf))
        oracle = self._write_manifest(
            "mixed-oracle.json",
            self._mixed_mem_manifest("oracle", oracle_rbf, ram_blocks=0, block_memory_bits=0),
        )
        result = self._run(oss, oracle, experiment="070_mixed_mem")
        self.assertNotEqual(result.returncode, 0)

    def _dsp_mem_manifest(self, lane, rbf, *, lutram_bits=256, block_memory_bits=2048, ram_blocks=1, dsp_blocks=1, hps=1):
        from copy import deepcopy

        from scripts.experiment_policy import policy_for

        value = self._mixed_mem_manifest(
            lane,
            rbf,
            lutram_bits=lutram_bits,
            block_memory_bits=block_memory_bits,
            ram_blocks=ram_blocks,
            hps=hps,
        )
        value["experiment"] = "080_dsp_mem"
        value["build"]["experiment"] = "080_dsp_mem"
        for artifact in value["artifacts"]:
            artifact["path"] = artifact["path"].replace("070_mixed_mem", "080_dsp_mem")
        hashes = {
            path.replace("070_mixed_mem", "080_dsp_mem"): _sha256(
                ROOT / path.replace("070_mixed_mem", "080_dsp_mem")
            )
            if (ROOT / path.replace("070_mixed_mem", "080_dsp_mem")).is_file()
            else digest
            for path, digest in value["build"]["source_hashes"].items()
        }
        value["build"]["source_hashes"] = hashes
        value["sources"] = [{"path": path, "sha256": digest} for path, digest in hashes.items()]
        value["build"]["allowed_hard_blocks"] = {
            "cyclonev_hps_interface_mpu_general_purpose": 1,
            "MISTRAL_M10K": 1,
            "MISTRAL_MUL9X9": 1,
        }
        if lane == "oracle":
            value["build"]["hard_blocks"]["DSP"]["used"] = dsp_blocks
            value["build"]["hard_block_evidence"] = deepcopy(value["build"]["hard_blocks"])
            value["build"]["resource_evidence"]["dsp_blocks"] = dsp_blocks
        else:
            value["build"]["hard_blocks"]["MISTRAL_MUL9X9"] = {
                "used": dsp_blocks,
                "available": 112,
                "utilization_percent": 0.89,
            }
            value["build"]["hard_block_evidence"] = deepcopy(value["build"]["hard_blocks"])
            value["build"]["experiment_policy"] = policy_for("080_dsp_mem").as_dict()
        return value

    def test_dsp_mem_comparison_accepts_product_block_lab_and_hps(self):
        oss_rbf = self.root / "build" / "oss" / "080_dsp_mem" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "080_dsp_mem" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"dsp-mem-oss")
        oracle_rbf.write_bytes(b"dsp-mem-oracle")
        oss = self._write_manifest("dsp-mem-oss.json", self._dsp_mem_manifest("oss", oss_rbf))
        oracle = self._write_manifest(
            "dsp-mem-oracle.json", self._dsp_mem_manifest("oracle", oracle_rbf)
        )
        result = self._run(oss, oracle, experiment="080_dsp_mem")
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertEqual(comparison["status"], "pass", comparison.get("failures"))
        self.assertEqual(comparison["lanes"]["oracle"]["hard_blocks"]["DSP"]["used"], 1)
        self.assertEqual(comparison["lanes"]["oracle"]["hard_blocks"]["BRAM/M10K"]["used"], 1)

    def test_dsp_mem_comparison_rejects_zero_dsp(self):
        oss_rbf = self.root / "build" / "oss" / "080_dsp_mem" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "080_dsp_mem" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"dsp-mem-oss-fail")
        oracle_rbf.write_bytes(b"dsp-mem-oracle-fail")
        oss = self._write_manifest("dsp-mem-oss-fail.json", self._dsp_mem_manifest("oss", oss_rbf))
        oracle = self._write_manifest(
            "dsp-mem-oracle-fail.json",
            self._dsp_mem_manifest("oracle", oracle_rbf, dsp_blocks=0),
        )
        result = self._run(oss, oracle, experiment="080_dsp_mem")
        self.assertNotEqual(result.returncode, 0)

    def _dsp_rom_manifest(self, lane, rbf, *, block_memory_bits=2048, ram_blocks=1, dsp_blocks=1, hps=1):
        from copy import deepcopy

        from scripts.experiment_policy import policy_for

        digest = _sha256(rbf)
        policy = policy_for("100_dsp_rom").as_dict()
        source_hashes = {
            path: _sha256(ROOT / path)
            for path in (
                "experiments/100_dsp_rom/rtl/top.v",
                "boards/de10nano/pins.qsf",
                "boards/de10nano/clocks.sdc",
            )
        }
        if lane == "oracle":
            source_hashes["experiments/100_dsp_rom/oracle/top.qsf"] = _sha256(
                ROOT / "experiments/100_dsp_rom/oracle/top.qsf"
            )
        hps_record = {
            "used": hps,
            "available": 1,
            "evidence_kind": "fitter_summary",
            "measured": True,
        }
        static_mlab = {
            "used": None,
            "available": None,
            "status": "excluded",
            "evidence_kind": "static_exclusion",
            "measured": False,
            "exclusion": {
                "basis": "static source/project exclusion",
                "patterns": [
                    r"\bmlab(?:s)?\b",
                    r"\blutram\b",
                    r"\b(?:altsyncram|lpm_ram|mlab_cell)\b",
                ],
                "sources": [
                    {"path": path, "sha256": value}
                    for path, value in source_hashes.items()
                ],
            },
        }
        if lane == "oracle":
            hard_blocks = {
                "PLL": {
                    "used": 0,
                    "available": 6,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "BRAM/M10K": {
                    "used": ram_blocks,
                    "available": 553,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "DSP": {
                    "used": dsp_blocks,
                    "available": 112,
                    "evidence_kind": "fitter_summary",
                    "measured": True,
                },
                "cyclonev_hps_interface_mpu_general_purpose": hps_record,
                "MLAB/LUTRAM": static_mlab,
            }
            resource_evidence = {
                "clock_inputs": 1,
                "external_input_ports": 0,
                "external_output_ports": 0,
                "bidirectional_ports": 0,
                "hps_general_purpose_interfaces": hps,
                "pll_blocks": 0,
                "dsp_blocks": dsp_blocks,
                "block_memory_bits": block_memory_bits,
                "lutram_bits": 0,
                "sdram_interfaces": 0,
            }
            clock = "FPGA_CLK1_50"
            clock_intent = "FPGA_CLK1_50"
            experiment_policy = None
        else:
            hard_blocks = {
                "cyclonev_hps_interface_mpu_general_purpose": {
                    "used": hps,
                    "available": 1,
                    "utilization_percent": 100.0,
                },
                "MISTRAL_MUL9X9": {"used": 1, "available": 112, "utilization_percent": 0.89},
                "MISTRAL_M10K": {"used": ram_blocks, "available": 553, "utilization_percent": 0.18},
                "cyclonev_oscillator": {
                    "used": 0,
                    "available": 1,
                    "utilization_percent": 0.0,
                },
            }
            resource_evidence = None
            clock = "product.FPGA_CLK1_50"
            clock_intent = None
            experiment_policy = policy
        provenance = {
            "path": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "executable": "/opt/quartus/17.0/quartus/bin/quartus_sh",
            "sha256": "d" * 64,
            "executable_sha256": "d" * 64,
            "version": "Quartus Prime Version 17.0.2 Build 602",
            "required_version": "17.0.2",
            "version_output_sha256": "e" * 64,
        }
        build = {
            "experiment": "100_dsp_rom",
            "target": "5CSEBA6U23I7",
            "status": "pass",
            "build_status": "pass",
            "route_status": "pass",
            "route": {"status": "pass", "unrouted": False},
            "timing": {
                "status": "pass",
                "requested_mhz": 50.0,
                "achieved_mhz": 180.0,
                "clock": clock,
            },
            "clock_constraint_mhz": 50.0 if lane == "oss" else None,
            "clock_intent": clock_intent,
            "resources": {"ALM": {"used": 20, "available": 41910}},
            "source_hashes": source_hashes,
            "hard_blocks": hard_blocks,
            "hard_block_evidence": deepcopy(hard_blocks),
            "hard_block_status": "pass" if dsp_blocks == 1 and ram_blocks == 1 and hps == 1 else "fail",
            "unknown_resources": {},
            "simulation": {"status": "pass"},
            "allowed_hard_blocks": {
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K": 1,
                "MISTRAL_MUL9X9": 1,
            },
            "authenticated_tools": {"quartus_sh": provenance} if lane == "oracle" else {},
            "tool_pins": {"quartus": provenance} if lane == "oracle" else {},
            "reproducibility": {
                "rbf_sha256": digest,
                "rbf_size_bytes": rbf.stat().st_size,
            },
        }
        if experiment_policy is not None:
            build["experiment_policy"] = experiment_policy
        if resource_evidence is not None:
            build["resource_evidence"] = resource_evidence
        if clock_intent is None:
            build.pop("clock_intent")
        if build["clock_constraint_mhz"] is None:
            build.pop("clock_constraint_mhz")
        return {
            "schema": 2,
            "experiment": "100_dsp_rom",
            "lane": lane,
            "target": "5CSEBA6U23I7",
            "sources": [
                {"path": path, "sha256": value}
                for path, value in source_hashes.items()
            ],
            "artifacts": [
                {
                    "path": f"build/{lane}/100_dsp_rom/top.rbf",
                    "sha256": digest,
                }
            ],
            "build": build,
        }

    def test_dsp_rom_comparison_accepts_product_block_and_hps(self):
        oss_rbf = self.root / "build" / "oss" / "100_dsp_rom" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "100_dsp_rom" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"dsp-rom-oss")
        oracle_rbf.write_bytes(b"dsp-rom-oracle")
        oss = self._write_manifest("dsp-rom-oss.json", self._dsp_rom_manifest("oss", oss_rbf))
        oracle = self._write_manifest(
            "dsp-rom-oracle.json", self._dsp_rom_manifest("oracle", oracle_rbf)
        )
        result = self._run(oss, oracle, experiment="100_dsp_rom")
        self.assertEqual(result.returncode, 0, result.stderr + result.stdout)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertEqual(comparison["status"], "pass", comparison.get("failures"))
        self.assertEqual(comparison["lanes"]["oracle"]["hard_blocks"]["DSP"]["used"], 1)
        self.assertEqual(comparison["lanes"]["oracle"]["hard_blocks"]["BRAM/M10K"]["used"], 1)
        self.assertEqual(
            comparison["lanes"]["oracle"]["hard_blocks"]["MLAB/LUTRAM"]["evidence_kind"],
            "static_exclusion",
        )

    def test_dsp_rom_comparison_rejects_zero_dsp(self):
        oss_rbf = self.root / "build" / "oss" / "100_dsp_rom" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "100_dsp_rom" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"dsp-rom-oss-fail")
        oracle_rbf.write_bytes(b"dsp-rom-oracle-fail")
        oss = self._write_manifest("dsp-rom-oss-fail.json", self._dsp_rom_manifest("oss", oss_rbf))
        oracle = self._write_manifest(
            "dsp-rom-oracle-fail.json",
            self._dsp_rom_manifest("oracle", oracle_rbf, dsp_blocks=0),
        )
        result = self._run(oss, oracle, experiment="100_dsp_rom")
        self.assertNotEqual(result.returncode, 0)

    def test_mailbox_policy_protocol_target_clock_and_resource_mismatches_fail_closed(self):
        cases = (
            ("missing policy hash", lambda value: value["build"].pop("experiment_policy_sha256")),
            ("policy hash mismatch", lambda value: value["build"].__setitem__("experiment_policy_sha256", "f" * 64)),
            ("protocol hash mismatch", lambda value: value["build"].__setitem__("protocol_source_sha256", "f" * 64)),
            ("protocol source mismatch", lambda value: value["build"].__setitem__("protocol_source", "experiments/010_blinky/rtl/top.v")),
            ("target mismatch", lambda value: value.__setitem__("target", "5CSEMA4U23C7")),
            ("clock mismatch", lambda value: value["build"]["timing"].__setitem__("clock", "wrong-clock")),
            ("primitive count mismatch", lambda value: value["build"]["resource_evidence"].__setitem__("hps_general_purpose_interfaces", 0)),
            ("unexpected resource", lambda value: value["build"]["resource_evidence"].__setitem__("pll_blocks", 1)),
            ("timing failure", lambda value: value["build"].__setitem__("timing", {**value["build"]["timing"], "status": "fail"})),
        )
        for name, mutate in cases:
            with self.subTest(name=name):
                oss_rbf = self.root / "build" / "oss" / "020_linux_mailbox" / "top.rbf"
                oracle_rbf = self.root / "build" / "oracle" / "020_linux_mailbox" / "top.rbf"
                oss_rbf.parent.mkdir(parents=True, exist_ok=True)
                oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
                oss_rbf.write_bytes(b"mailbox-oss")
                oracle_rbf.write_bytes(b"mailbox-oracle")
                oss_value = self._mailbox_manifest("oss", oss_rbf)
                mutate(oss_value)
                oss = self._write_manifest("mailbox-oss.json", oss_value)
                oracle = self._write_manifest(
                    "mailbox-oracle.json", self._mailbox_manifest("oracle", oracle_rbf)
                )
                result = self._run(oss, oracle, experiment="020_linux_mailbox")
                self.assertNotEqual(result.returncode, 0)

    def test_mailbox_resource_evidence_rejects_order_unknown_ports_forbidden_and_bindings(self):
        forbidden_fields = (
            "pll_blocks",
            "dsp_blocks",
            "block_memory_bits",
            "lutram_bits",
            "sdram_interfaces",
        )
        mutations = []
        unknown = lambda value: value["build"]["resource_evidence"].update({"unknown": 0})
        mutations.append(("unknown semantic field", unknown))

        def reorder(value):
            evidence = value["build"]["resource_evidence"]
            value["build"]["resource_evidence"] = dict(reversed(list(evidence.items())))

        mutations.append(("reordered semantic fields", reorder))
        mutations.append(
            (
                "extra external output",
                lambda value: value["build"]["resource_evidence"].__setitem__(
                    "external_output_ports", 1
                ),
            )
        )
        mutations.append(
            (
                "extra external input",
                lambda value: value["build"]["resource_evidence"].__setitem__(
                    "external_input_ports", 1
                ),
            )
        )
        mutations.append(
            (
                "extra bidirectional port",
                lambda value: value["build"]["resource_evidence"].__setitem__(
                    "bidirectional_ports", 1
                ),
            )
        )
        for field in forbidden_fields:
            mutations.append(
                (
                    f"nonzero {field}",
                    lambda value, field=field: value["build"]["resource_evidence"].__setitem__(
                        field, 1
                    ),
                )
            )
        mutations.extend(
            (
                (
                    "unexpected hard block",
                    lambda value: value["build"]["hard_blocks"].update(
                        {"unknown_hard_block": {"used": 0, "available": 1}}
                    ),
                ),
                (
                    "wrong lane binding",
                    lambda value: value.__setitem__("lane", "oracle"),
                ),
                (
                    "wrong source binding",
                    lambda value: value["sources"][0].__setitem__("sha256", "f" * 64),
                ),
                (
                    "wrong artifact binding",
                    lambda value: value["artifacts"][0].__setitem__(
                        "path", "build/oracle/020_linux_mailbox/top.rbf"
                    ),
                ),
                (
                    "wrong report binding",
                    lambda value: value["artifacts"][1].__setitem__(
                        "path", "build/oracle/020_linux_mailbox/top.fit.rpt"
                    ),
                ),
            )
        )

        for name, mutate in mutations:
            with self.subTest(name=name):
                oss_rbf = self.root / "build" / "oss" / "020_linux_mailbox" / "top.rbf"
                oracle_rbf = self.root / "build" / "oracle" / "020_linux_mailbox" / "top.rbf"
                oss_rbf.parent.mkdir(parents=True, exist_ok=True)
                oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
                oss_rbf.write_bytes(b"mailbox-oss")
                oracle_rbf.write_bytes(b"mailbox-oracle")
                oss_value = self._mailbox_manifest("oss", oss_rbf)
                mutate(oss_value)
                oss = self._write_manifest("mailbox-oss.json", oss_value)
                oracle = self._write_manifest(
                    "mailbox-oracle.json", self._mailbox_manifest("oracle", oracle_rbf)
                )
                result = self._run(oss, oracle, experiment="020_linux_mailbox")
                self.assertNotEqual(result.returncode, 0)

    def test_mailbox_report_binding_and_lane_clock_identity_fail_closed(self):
        mutations = (
            ("missing report path", lambda value: value["build"].pop("synthesis_report_path")),
            ("missing report hash", lambda value: value["build"].pop("synthesis_report_sha256")),
            ("stale report hash", lambda value: value["build"].__setitem__("synthesis_report_sha256", "f" * 64)),
            ("traversal report path", lambda value: value["build"].__setitem__("synthesis_report_path", "build/oss/020_linux_mailbox/../oracle/020_linux_mailbox/synthesis-report.rpt")),
            ("duplicate report", lambda value: value["artifacts"].append(dict(value["artifacts"][1]))),
            ("swapped OSS clock identity", lambda value: value["build"]["timing"].__setitem__("clock", "FPGA_CLK1_50")),
        )
        for name, mutate in mutations:
            with self.subTest(name=name):
                oss_rbf = self.root / "build" / "oss" / "020_linux_mailbox" / "top.rbf"
                oracle_rbf = self.root / "build" / "oracle" / "020_linux_mailbox" / "top.rbf"
                oss_rbf.parent.mkdir(parents=True, exist_ok=True)
                oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
                oss_rbf.write_bytes(b"mailbox-oss")
                oracle_rbf.write_bytes(b"mailbox-oracle")
                oss_value = self._mailbox_manifest("oss", oss_rbf)
                mutate(oss_value)
                oss = self._write_manifest("mailbox-oss.json", oss_value)
                oracle = self._write_manifest(
                    "mailbox-oracle.json", self._mailbox_manifest("oracle", oracle_rbf)
                )
                result = self._run(oss, oracle, experiment="020_linux_mailbox")
                self.assertNotEqual(result.returncode, 0)

    def test_mailbox_synthesis_report_must_be_nonempty(self):
        oss_rbf = self.root / "build" / "oss" / "020_linux_mailbox" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "020_linux_mailbox" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"mailbox-oss")
        oracle_rbf.write_bytes(b"mailbox-oracle")
        oss_value = self._mailbox_manifest("oss", oss_rbf)
        oracle_value = self._mailbox_manifest("oracle", oracle_rbf)
        report_path = oss_value["build"]["synthesis_report_path"]
        report = oss_rbf.parent / "timing.json"
        report.write_bytes(b"")
        empty_digest = _sha256(report)

        def replace_report_digests(value):
            def visit(node):
                if isinstance(node, dict):
                    if node.get("path") == report_path and "sha256" in node:
                        node["sha256"] = empty_digest
                    for child in node.values():
                        visit(child)
                elif isinstance(node, list):
                    for child in node:
                        visit(child)

            visit(value)

        replace_report_digests(oss_value)
        for record in (oss_value, oss_value["build"]):
            record["synthesis_report_sha256"] = empty_digest
        oss = self._write_manifest("mailbox-oss.json", oss_value)
        oracle = self._write_manifest(
            "mailbox-oracle.json", oracle_value
        )
        result = self._run(oss, oracle, experiment="020_linux_mailbox")
        self.assertNotEqual(result.returncode, 0)

    def test_oss_static_exclusion_rejects_rewritten_command_with_refreshed_hash(self):
        mutations = (
            ("replacement executable", "yosys.log", lambda text: "command: /usr/bin/true\n"),
            (
                "missing synthesis flag",
                "yosys.log",
                lambda text: text.replace("-nolutram", "", 1),
            ),
            (
                "wrong synthesis source",
                "yosys.log",
                lambda text: text.replace(
                    "experiments/020_linux_mailbox/rtl/top.v",
                    "experiments/010_blinky/rtl/top.v",
                    1,
                ),
            ),
            (
                "wrong route device",
                "nextpnr.log",
                lambda text: text.replace("5CSEBA6U23I7", "5CSEMA4U23C7", 1),
            ),
            (
                "wrong route output",
                "nextpnr.log",
                lambda text: text.replace(
                    "build/oss/020_linux_mailbox/top.rbf",
                    "build/oss/020_linux_mailbox/other.rbf",
                    1,
                ),
            ),
        )
        for name, command_name, mutate in mutations:
            with self.subTest(name=name):
                oss_rbf = self.root / "build" / "oss" / "020_linux_mailbox" / "top.rbf"
                oracle_rbf = self.root / "build" / "oracle" / "020_linux_mailbox" / "top.rbf"
                oss_rbf.parent.mkdir(parents=True, exist_ok=True)
                oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
                oss_rbf.write_bytes(b"mailbox-oss")
                oracle_rbf.write_bytes(b"mailbox-oracle")
                oss_value = self._mailbox_manifest("oss", oss_rbf)
                oracle_value = self._mailbox_manifest("oracle", oracle_rbf)

                command_path = oss_rbf.parent / command_name
                command_path.write_text(
                    mutate(command_path.read_text(encoding="utf-8")),
                    encoding="utf-8",
                )
                command_path_name = f"build/oss/020_linux_mailbox/{command_name}"
                command_digest = _sha256(command_path)

                def refresh(node):
                    if isinstance(node, dict):
                        if node.get("path") == command_path_name and "sha256" in node:
                            node["sha256"] = command_digest
                        for child in node.values():
                            refresh(child)
                    elif isinstance(node, list):
                        for child in node:
                            refresh(child)

                refresh(oss_value)
                oss = self._write_manifest("mailbox-oss.json", oss_value)
                oracle = self._write_manifest("mailbox-oracle.json", oracle_value)
                result = self._run(oss, oracle, experiment="020_linux_mailbox")
                self.assertNotEqual(result.returncode, 0)
                comparison = json.loads((self.output / "comparison.json").read_text())
                self.assertTrue(
                    any("command" in item.lower() or "proof" in item.lower() for item in comparison["failures"]),
                    comparison["failures"],
                )

    def test_oss_command_executable_path_must_bind_to_authenticated_tool(self):
        for command_name, wrong_path in (
            ("yosys.log", "/tmp/yosys"),
            ("nextpnr-help.log", "/tmp/nextpnr-mistral"),
            ("nextpnr.log", "/tmp/nextpnr-mistral"),
        ):
            with self.subTest(command_name=command_name):
                oss_rbf = self.root / "build" / "oss" / "020_linux_mailbox" / "top.rbf"
                oracle_rbf = self.root / "build" / "oracle" / "020_linux_mailbox" / "top.rbf"
                oss_rbf.parent.mkdir(parents=True, exist_ok=True)
                oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
                oss_rbf.write_bytes(b"mailbox-oss")
                oracle_rbf.write_bytes(b"mailbox-oracle")
                oss_value = self._mailbox_manifest("oss", oss_rbf)
                oracle_value = self._mailbox_manifest("oracle", oracle_rbf)

                command_path = oss_rbf.parent / command_name
                original = command_path.read_text(encoding="utf-8")
                command_path.write_text(
                    original.replace(original.split()[1], wrong_path, 1),
                    encoding="utf-8",
                )
                command_digest = _sha256(command_path)
                command_record_path = f"build/oss/020_linux_mailbox/{command_name}"

                def refresh(node):
                    if isinstance(node, dict):
                        if node.get("path") == command_record_path and "sha256" in node:
                            node["sha256"] = command_digest
                        for child in node.values():
                            refresh(child)
                    elif isinstance(node, list):
                        for child in node:
                            refresh(child)

                refresh(oss_value)
                oss = self._write_manifest("mailbox-oss.json", oss_value)
                oracle = self._write_manifest("mailbox-oracle.json", oracle_value)
                result = self._run(oss, oracle, experiment="020_linux_mailbox")
                self.assertNotEqual(result.returncode, 0)
                comparison = json.loads((self.output / "comparison.json").read_text())
                self.assertTrue(
                    any(
                        "tool" in item.lower() or "executable" in item.lower()
                        for item in comparison["failures"]
                    ),
                    comparison["failures"],
                )

    def test_oss_static_source_patterns_are_field_specific_and_verilog_bound(self):
        from scripts.compare_builds import _oss_static_source_scan_failures

        expected = {
            "pll_blocks": "MISTRAL_PLL",
            "dsp_blocks": "MISTRAL_MUL9X9",
            "block_memory_bits": "MISTRAL_M10K",
            "lutram_bits": "MISTRAL_MLAB",
            "sdram_interfaces": "cyclonev_hps_interface_fpga2sdram",
        }
        repo = self.root / "scan-repo"
        for relative in (
            "experiments/020_linux_mailbox/rtl/top.v",
            "boards/de10nano/pins.qsf",
            "boards/de10nano/clocks.sdc",
        ):
            path = repo / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("", encoding="utf-8")

        for field, identifier in expected.items():
            top = repo / "experiments/020_linux_mailbox/rtl/top.v"
            top.write_text(f"module top; {identifier} u0(); endmodule\n", encoding="utf-8")
            source_hashes = {
                relative: _sha256(repo / relative)
                for relative in (
                    "experiments/020_linux_mailbox/rtl/top.v",
                    "boards/de10nano/pins.qsf",
                    "boards/de10nano/clocks.sdc",
                )
            }
            failures = _oss_static_source_scan_failures(
                source_hashes, repo, field, "020_linux_mailbox", "oss"
            )
            self.assertTrue(failures, (field, identifier, failures))

        pattern_rbf = self.root / "build" / "oss" / "020_linux_mailbox" / "top.rbf"
        pattern_rbf.parent.mkdir(parents=True, exist_ok=True)
        pattern_rbf.write_bytes(b"pattern-rbf")
        value = self._mailbox_manifest("oss", pattern_rbf)
        field_patterns = {
            "pll_blocks": ["PLL", "MISTRAL_PLL", "phase_locked", "altpll"],
            "dsp_blocks": [
                "DSP",
                "MUL",
                "MISTRAL_MUL9X9",
                "MISTRAL_MUL18X18",
                "MISTRAL_MUL27X27",
                "MAC",
            ],
            "block_memory_bits": [
                "BRAM",
                "M10K",
                "MISTRAL_M10K",
                "M20K",
                "RAM",
                "ram_block",
                "altsyncram",
            ],
            "lutram_bits": [
                "MLAB",
                "MISTRAL_MLAB",
                "LUTRAM",
                "altsyncram",
                "lpm_ram",
                "mlab_cell",
                r"re:(?:reg|wire|logic)\s*\[[^\]]+\]\s+\w+\s*\[",
            ],
            "sdram_interfaces": [
                "SDRAM",
                "DDR",
                "cyclonev_hps_interface_fpga2sdram",
                "hps_sdram",
            ],
        }
        fields = value["build"]["resource_evidence_provenance"]["fields"]
        for field, patterns in field_patterns.items():
            self.assertEqual(fields[field]["patterns"], patterns)

    def test_oracle_static_exclusion_requires_command_hash_binding(self):
        oss_rbf = self.root / "build" / "oss" / "020_linux_mailbox" / "top.rbf"
        oracle_rbf = self.root / "build" / "oracle" / "020_linux_mailbox" / "top.rbf"
        oss_rbf.parent.mkdir(parents=True, exist_ok=True)
        oracle_rbf.parent.mkdir(parents=True, exist_ok=True)
        oss_rbf.write_bytes(b"mailbox-oss")
        oracle_rbf.write_bytes(b"mailbox-oracle")
        oss = self._write_manifest("mailbox-oss.json", self._mailbox_manifest("oss", oss_rbf))
        oracle_value = self._mailbox_manifest("oracle", oracle_rbf)
        oracle_value["build"]["hard_blocks"]["MLAB/LUTRAM"]["exclusion"].pop("commands")
        oracle_value["build"]["hard_block_evidence"]["MLAB/LUTRAM"]["exclusion"].pop("commands")
        oracle = self._write_manifest("mailbox-oracle.json", oracle_value)

        result = self._run(oss, oracle, experiment="020_linux_mailbox")

        self.assertNotEqual(result.returncode, 0)

    def _run(self, oss, oracle, *, experiment="010_blinky"):
        env = os.environ.copy()
        return subprocess.run(
            [
                str(COMPARE),
                "--oss-manifest",
                str(oss),
                "--oracle-manifest",
                str(oracle),
                "--experiment",
                experiment,
                "--output-dir",
                str(self.output),
            ],
            cwd=ROOT,
            env=env,
            text=True,
            capture_output=True,
        )

    def test_resource_and_rbf_differences_are_informational(self):
        oss = self._write_manifest("oss.json", self._manifest("oss", self.oss_rbf, alm=28))
        oracle = self._write_manifest(
            "oracle.json", self._manifest("oracle", self.oracle_rbf, alm=31)
        )

        result = self._run(oss, oracle)

        self.assertEqual(result.returncode, 0, result.stderr)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertEqual(comparison["status"], "pass")
        self.assertTrue(comparison["differences"])
        self.assertIn("ALM", (self.output / "comparison.md").read_text())
        self.assertIn("informational", (self.output / "comparison.md").read_text().lower())

    def test_failed_oracle_build_is_nonzero(self):
        oss = self._write_manifest("oss.json", self._manifest("oss", self.oss_rbf))
        oracle = self._write_manifest(
            "oracle.json",
            self._manifest("oracle", self.oracle_rbf, status="fail"),
        )

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertEqual(comparison["status"], "fail")
        self.assertTrue(any("build" in item.lower() for item in comparison["failures"]))

    def test_missing_artifact_is_failure_and_not_zero_resource(self):
        oracle_bytes = self.oracle_rbf.read_bytes()
        self.oracle_rbf.unlink()
        try:
            value = self._manifest("oracle", self.oracle_rbf, artifact=True)
            value["build"]["resources"] = {}
            oss = self._write_manifest("oss.json", self._manifest("oss", self.oss_rbf))
            oracle = self._write_manifest("oracle.json", value)

            result = self._run(oss, oracle)
        finally:
            self.oracle_rbf.write_bytes(oracle_bytes)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("missing" in item.lower() for item in comparison["failures"]))
        oracle_lane = comparison["lanes"]["oracle"]
        self.assertNotIn("ALM", oracle_lane["resources"])

    def test_common_source_hashes_must_be_present_and_identical(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        oracle_value["sources"] = [
            source
            for source in oracle_value["sources"]
            if source["path"] != "boards/de10nano/pins.qsf"
        ]
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("common" in item.lower() or "source" in item.lower() for item in comparison["failures"]))

    def test_build_summary_source_hashes_must_match_manifest_sources(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        oracle_value["build"]["source_hashes"]["boards/de10nano/clocks.sdc"] = "d" * 64
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("build summary" in item.lower() for item in comparison["failures"]))

    def test_hard_block_map_must_be_complete_and_well_formed(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        del oracle_value["build"]["hard_blocks"]["DSP"]
        del oracle_value["build"]["hard_block_evidence"]["DSP"]
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("hard" in item.lower() for item in comparison["failures"]))

    def test_rbf_must_use_exact_lane_path_and_match_summary(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        oracle_value["artifacts"][0]["path"] = "build/oss/010_blinky/top.rbf"
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("rbf" in item.lower() for item in comparison["failures"]))

    def test_oracle_hard_block_evidence_kind_and_completeness_are_required(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        del oracle_value["build"]["hard_block_evidence"]["MLAB/LUTRAM"]["exclusion"]
        oracle_value["build"]["hard_block_evidence"]["MLAB/LUTRAM"]["evidence_kind"] = "fitter_summary"
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("evidence" in item.lower() for item in comparison["failures"]))

    def test_oracle_quartus_provenance_is_required_and_exact(self):
        for mutation in ("missing", "wrong-version"):
            oss_value = self._manifest("oss", self.oss_rbf)
            oracle_value = self._manifest("oracle", self.oracle_rbf)
            if mutation == "missing":
                del oracle_value["build"]["authenticated_tools"]
            else:
                oracle_value["build"]["authenticated_tools"]["quartus_sh"]["version"] = "Quartus Prime Version 17.0.0 Build 595"
            oss = self._write_manifest("oss.json", oss_value)
            oracle = self._write_manifest("oracle.json", oracle_value)

            result = self._run(oss, oracle)

            self.assertNotEqual(result.returncode, 0)
            comparison = json.loads((self.output / "comparison.json").read_text())
            self.assertTrue(any("provenance" in item.lower() or "quartus" in item.lower() for item in comparison["failures"]))

    def test_oracle_static_exclusion_patterns_must_be_canonical(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        for name in ("MLAB/LUTRAM", "HPS"):
            oracle_value["build"]["hard_blocks"][name]["exclusion"]["patterns"] = ["arbitrary"]
            oracle_value["build"]["hard_block_evidence"][name]["exclusion"]["patterns"] = ["arbitrary"]
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("pattern" in item.lower() for item in comparison["failures"]))

    def test_oracle_static_exclusion_hash_must_match_manifest_source(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        for name in ("MLAB/LUTRAM", "HPS"):
            for record in (
                oracle_value["build"]["hard_blocks"][name],
                oracle_value["build"]["hard_block_evidence"][name],
            ):
                record["exclusion"]["sources"][0]["sha256"] = "f" * 64
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("hash" in item.lower() for item in comparison["failures"]))

    def test_oracle_static_exclusion_requires_exact_canonical_source_set(self):
        for mutation in ("missing", "extra", "reordered"):
            oss_value = self._manifest("oss", self.oss_rbf)
            oracle_value = self._manifest("oracle", self.oracle_rbf)
            for name in ("MLAB/LUTRAM", "HPS"):
                for record in (
                    oracle_value["build"]["hard_blocks"][name],
                    oracle_value["build"]["hard_block_evidence"][name],
                ):
                    if mutation == "missing":
                        record["exclusion"]["sources"].pop()
                    elif mutation == "extra":
                        record["exclusion"]["sources"].append(
                            {"path": "experiments/010_blinky/oracle/top.qpf", "sha256": "e" * 64}
                        )
                    else:
                        record["exclusion"]["sources"].reverse()
            oss = self._write_manifest("oss.json", oss_value)
            oracle = self._write_manifest("oracle.json", oracle_value)

            result = self._run(oss, oracle)

            self.assertNotEqual(result.returncode, 0)
            comparison = json.loads((self.output / "comparison.json").read_text())
            self.assertTrue(any("path" in item.lower() or "source" in item.lower() for item in comparison["failures"]))

    def test_oracle_static_exclusion_must_use_null_used_not_invented_zero(self):
        oss_value = self._manifest("oss", self.oss_rbf)
        oracle_value = self._manifest("oracle", self.oracle_rbf)
        for name in ("MLAB/LUTRAM", "HPS"):
            oracle_value["build"]["hard_blocks"][name]["used"] = 0
            oracle_value["build"]["hard_block_evidence"][name]["used"] = 0
        oss = self._write_manifest("oss.json", oss_value)
        oracle = self._write_manifest("oracle.json", oracle_value)

        result = self._run(oss, oracle)

        self.assertNotEqual(result.returncode, 0)
        comparison = json.loads((self.output / "comparison.json").read_text())
        self.assertTrue(any("static" in item.lower() or "excluded" in item.lower() for item in comparison["failures"]))

    def test_static_exclusion_passes_as_excluded_and_renders_without_numeric_zero(self):
        oss = self._write_manifest("oss.json", self._manifest("oss", self.oss_rbf))
        oracle = self._write_manifest("oracle.json", self._manifest("oracle", self.oracle_rbf))

        result = self._run(oss, oracle)

        self.assertEqual(result.returncode, 0, result.stderr)
        comparison = json.loads((self.output / "comparison.json").read_text())
        for name in ("MLAB/LUTRAM", "HPS"):
            record = comparison["lanes"]["oracle"]["hard_blocks"][name]
            self.assertIsNone(record["used"])
            self.assertEqual(record["status"], "excluded")
        markdown = (self.output / "comparison.md").read_text()
        self.assertIn("excluded (static)", markdown)


if __name__ == "__main__":
    unittest.main()
