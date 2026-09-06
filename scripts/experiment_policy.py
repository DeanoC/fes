#!/usr/bin/env python3
"""Closed, immutable build policy for the supported FPGA experiments.

The OSS and report lanes consume this module rather than deriving source,
clock, or hard-resource expectations from an operator supplied experiment
name.  Keeping the table here makes an unknown experiment fail before any
tool is invoked and gives the summary lane one resource classification rule.
"""

from __future__ import annotations

import argparse
import json
import math
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from types import MappingProxyType
from typing import Any, Mapping, Sequence


TARGET_DEVICE = "5CSEBA6U23I7"
BOARD_CONSTRAINTS = (
    "boards/de10nano/pins.qsf",
    "boards/de10nano/clocks.sdc",
)
ORDINARY_RESOURCES = frozenset(
    {"MISTRAL_BUF", "MISTRAL_CLKENA", "MISTRAL_COMB", "MISTRAL_FF", "MISTRAL_IO"}
)


class PolicyError(ValueError):
    """Raised when an experiment or its evidence violates the closed policy."""


@dataclass(frozen=True)
class SimJob:
    """One Verilator lint/build/run job belonging to a closed experiment."""

    name: str
    top: str
    sources: tuple[str, ...]
    tb: str
    parameters: Mapping[str, str] = MappingProxyType({})
    cflags: tuple[str, ...] = ()
    lint: bool = True

    def __post_init__(self) -> None:
        if not self.name or not re.fullmatch(r"[a-z][a-z0-9_]*", self.name):
            raise PolicyError("sim job name must be a lowercase identifier")
        if not self.top or not re.fullmatch(r"[A-Za-z_]\w*", self.top):
            raise PolicyError(f"sim job {self.name}: top must be a Verilog identifier")
        if not self.sources or any(not isinstance(path, str) or not path for path in self.sources):
            raise PolicyError(f"sim job {self.name}: source list must contain non-empty paths")
        if not self.tb or not isinstance(self.tb, str):
            raise PolicyError(f"sim job {self.name}: tb must be a non-empty path")
        parameters: dict[str, str] = {}
        for key, value in dict(self.parameters).items():
            if not isinstance(key, str) or not re.fullmatch(r"[A-Za-z_]\w*", key):
                raise PolicyError(f"sim job {self.name}: parameter names must be Verilog identifiers")
            if not isinstance(value, str) or not value:
                raise PolicyError(f"sim job {self.name}: parameter {key} must be a non-empty string")
            parameters[key] = value
        object.__setattr__(self, "parameters", MappingProxyType(parameters))
        if any(not isinstance(flag, str) or not flag for flag in self.cflags):
            raise PolicyError(f"sim job {self.name}: cflags must be non-empty strings")
        if not isinstance(self.lint, bool):
            raise PolicyError(f"sim job {self.name}: lint must be a boolean")

    def as_dict(self) -> dict[str, Any]:
        return {
            "name": self.name,
            "top": self.top,
            "sources": list(self.sources),
            "tb": self.tb,
            "parameters": dict(self.parameters),
            "cflags": list(self.cflags),
            "lint": self.lint,
        }


@dataclass(frozen=True)
class ExperimentPolicy:
    """Immutable source, timing, and resource expectations for one experiment."""

    name: str
    sources: tuple[str, ...]
    top: str
    clock: str
    clock_mhz: float
    allowed_hard_blocks: Mapping[str, int]
    forbidden_source_patterns: tuple[str, ...]
    forbidden_resource_patterns: tuple[str, ...]
    required_source_identifiers: Mapping[str, int] = MappingProxyType({})
    constraints: tuple[str, ...] = BOARD_CONSTRAINTS
    target: str = TARGET_DEVICE
    artifact: str = "top.rbf"
    clock_evidence_names: tuple[str, ...] = ()
    additional_clocks_mhz: Mapping[str, float] = MappingProxyType({})
    nobram: bool = True
    nolutram: bool = True
    nodsp: bool = True
    yosys_post_synth: str = ""
    sim_jobs: tuple[SimJob, ...] = ()
    required_synth_cells: Mapping[str, int] = MappingProxyType({})

    def __post_init__(self) -> None:
        if not self.name or not isinstance(self.name, str):
            raise PolicyError("policy name must be a non-empty string")
        if not self.sources or any(not isinstance(path, str) or not path for path in self.sources):
            raise PolicyError(f"{self.name}: source list must contain non-empty paths")
        if not self.top or not isinstance(self.top, str):
            raise PolicyError(f"{self.name}: top must be a non-empty string")
        if not self.clock or not isinstance(self.clock, str):
            raise PolicyError(f"{self.name}: clock must be a non-empty string")
        clock_evidence_names = tuple(self.clock_evidence_names) or (self.clock,)
        if any(not isinstance(value, str) or not value for value in clock_evidence_names):
            raise PolicyError(f"{self.name}: clock evidence names must be non-empty strings")
        object.__setattr__(self, "clock_evidence_names", clock_evidence_names)
        if not isinstance(self.clock_mhz, (int, float)) or isinstance(self.clock_mhz, bool):
            raise PolicyError(f"{self.name}: clock constraint must be numeric")
        if not math.isfinite(float(self.clock_mhz)) or float(self.clock_mhz) <= 0:
            raise PolicyError(f"{self.name}: clock constraint must be positive and finite")
        clocks = dict(self.additional_clocks_mhz)
        for name, frequency in clocks.items():
            if not isinstance(name, str) or not name or name in clock_evidence_names:
                raise PolicyError(f"{self.name}: additional clock must have a distinct non-empty name")
            if isinstance(frequency, bool) or not isinstance(frequency, (int, float)) or not math.isfinite(frequency) or frequency <= 0:
                raise PolicyError(f"{self.name}: additional clock frequency must be positive and finite")
        object.__setattr__(self, "additional_clocks_mhz", MappingProxyType(clocks))
        if self.target != TARGET_DEVICE:
            raise PolicyError(f"{self.name}: target must be {TARGET_DEVICE}")
        for flag_name, flag in (("nobram", self.nobram), ("nolutram", self.nolutram), ("nodsp", self.nodsp)):
            if not isinstance(flag, bool):
                raise PolicyError(f"{self.name}: {flag_name} must be a boolean")
        if not isinstance(self.yosys_post_synth, str) or "\n" in self.yosys_post_synth:
            raise PolicyError(f"{self.name}: yosys_post_synth must be a single-line string")
        jobs = tuple(self.sim_jobs)
        if any(not isinstance(job, SimJob) for job in jobs):
            raise PolicyError(f"{self.name}: sim_jobs must contain SimJob values")
        names = [job.name for job in jobs]
        if len(names) != len(set(names)):
            raise PolicyError(f"{self.name}: sim job names must be unique")
        object.__setattr__(self, "sim_jobs", jobs)

        synth_cells: dict[str, int] = {}
        for cell, count in dict(self.required_synth_cells).items():
            if not isinstance(cell, str) or not cell:
                raise PolicyError(f"{self.name}: required synth cell names must be non-empty strings")
            if not isinstance(count, int) or isinstance(count, bool) or count < 0:
                raise PolicyError(f"{self.name}: required synth cell count for {cell} must be non-negative")
            synth_cells[cell] = count
        object.__setattr__(self, "required_synth_cells", MappingProxyType(synth_cells))

        allowed: dict[str, int] = {}
        for resource, count in dict(self.allowed_hard_blocks).items():
            if not isinstance(resource, str) or not resource:
                raise PolicyError(f"{self.name}: allowed hard-block names must be non-empty strings")
            if not isinstance(count, int) or isinstance(count, bool) or count < 0:
                raise PolicyError(f"{self.name}: allowed hard-block count for {resource} must be non-negative")
            allowed[resource] = count
        object.__setattr__(self, "allowed_hard_blocks", MappingProxyType(allowed))

        required_source_identifiers: dict[str, int] = {}
        for identifier, count in dict(self.required_source_identifiers).items():
            if not isinstance(identifier, str) or not re.fullmatch(r"[A-Za-z_]\w*", identifier):
                raise PolicyError(f"{self.name}: required source identifiers must be Verilog identifiers")
            if not isinstance(count, int) or isinstance(count, bool) or count < 0:
                raise PolicyError(
                    f"{self.name}: required source identifier count for {identifier} must be non-negative"
                )
            required_source_identifiers[identifier] = count
        object.__setattr__(
            self,
            "required_source_identifiers",
            MappingProxyType(required_source_identifiers),
        )

        for label, values in (
            ("source", self.forbidden_source_patterns),
            ("resource", self.forbidden_resource_patterns),
        ):
            if any(not isinstance(value, str) or not value for value in values):
                raise PolicyError(f"{self.name}: forbidden {label} patterns must be non-empty strings")

    # Compatibility aliases make the contract explicit to shell/build callers
    # while preserving one canonical table field for each value.
    @property
    def source_paths(self) -> tuple[str, ...]:
        return self.sources

    @property
    def source_list(self) -> tuple[str, ...]:
        return self.sources

    @property
    def source_files(self) -> tuple[str, ...]:
        return self.sources

    @property
    def rtl_sources(self) -> tuple[str, ...]:
        return self.sources

    @property
    def rtl(self) -> str:
        return self.sources[0]

    @property
    def source(self) -> str:
        return self.sources[0]

    @property
    def constraint_paths(self) -> tuple[str, ...]:
        return self.constraints

    @property
    def qsf(self) -> str:
        return self.constraints[0]

    @property
    def sdc(self) -> str:
        return self.constraints[1]

    @property
    def clock_name(self) -> str:
        return self.clock

    @property
    def clock_prefix(self) -> str:
        return self.clock

    @property
    def clock_constraint_mhz(self) -> float:
        return float(self.clock_mhz)

    @property
    def clock_constraint(self) -> float:
        return float(self.clock_mhz)

    @property
    def clock_period_ns(self) -> float:
        return 1000.0 / float(self.clock_mhz)

    @property
    def period_ns(self) -> float:
        return self.clock_period_ns

    @property
    def frequency_mhz(self) -> float:
        return float(self.clock_mhz)

    @property
    def requested_mhz(self) -> float:
        return float(self.clock_mhz)

    @property
    def accepted_clock_names(self) -> tuple[str, ...]:
        return self.clock_evidence_names

    @property
    def timing_clock_names(self) -> tuple[str, ...]:
        return self.clock_evidence_names

    def matches_clock(self, name: str) -> bool:
        """Match only an explicitly attested timing clock identity."""

        return isinstance(name, str) and name in self.clock_evidence_names

    @property
    def allowed_hard_block_counts(self) -> Mapping[str, int]:
        return self.allowed_hard_blocks

    @property
    def hard_block_counts(self) -> Mapping[str, int]:
        return self.allowed_hard_blocks

    @property
    def hard_blocks(self) -> Mapping[str, int]:
        return self.allowed_hard_blocks

    @property
    def forbidden_resources(self) -> tuple[str, ...]:
        return self.forbidden_resource_patterns

    @property
    def forbidden_sources(self) -> tuple[str, ...]:
        return self.forbidden_source_patterns

    @property
    def all_source_paths(self) -> tuple[str, ...]:
        return (*self.sources, *self.constraints)

    def _matches_resource_pattern(self, name: str) -> bool:
        lowered = name.lower()
        return any(pattern.lower() in lowered for pattern in self.forbidden_resource_patterns)

    def classify_resource(self, name: str) -> str:
        """Classify a utilization key as ordinary, allowed, forbidden, or unknown."""

        if name in ORDINARY_RESOURCES:
            return "ordinary"
        if name in self.allowed_hard_blocks:
            return "allowed"
        if name in self.required_synth_cells:
            return "allowed"
        if self._matches_resource_pattern(name):
            return "forbidden"
        return "unknown"

    @staticmethod
    def _used(record: Any, resource: str) -> int:
        if isinstance(record, (int, float)) and not isinstance(record, bool):
            used = record
        else:
            if not isinstance(record, Mapping):
                raise PolicyError(f"resource {resource} record must be an object")
            used = record.get("used")
        if isinstance(used, bool) or not isinstance(used, (int, float)):
            raise PolicyError(f"resource {resource} used count must be numeric")
        if not math.isfinite(float(used)) or float(used) < 0 or not float(used).is_integer():
            raise PolicyError(f"resource {resource} used count must be a non-negative integer")
        return int(used)

    def validate_resources(self, resources: Mapping[str, Any]) -> None:
        """Fail closed unless utilization obeys the exact hard-block policy."""

        if not isinstance(resources, Mapping):
            raise PolicyError("resources must be an object")
        for name, record in resources.items():
            if not isinstance(name, str) or not name:
                raise PolicyError("resource name must be a non-empty string")
            used = self._used(record, name)
            classification = self.classify_resource(name)
            if classification == "unknown":
                raise PolicyError(f"unknown resource: {name}")
            if classification == "forbidden" and used != 0:
                raise PolicyError(f"forbidden resource {name} used count {used}")
            if classification == "allowed":
                expected = self.allowed_hard_blocks.get(name, self.required_synth_cells.get(name))
                if expected is not None and used != expected:
                    raise PolicyError(f"resource {name} must use exactly {expected}, got {used}")

        missing = [name for name in self.allowed_hard_blocks if name not in resources]
        if missing:
            raise PolicyError("missing allowed resource evidence: " + ", ".join(sorted(missing)))

    def validate_hard_blocks(self, resources: Mapping[str, Any]) -> None:
        """Alias for callers that provide only hard-block utilization records."""

        self.validate_resources(resources)

    def validate_source_text(self, path: str, text: str) -> None:
        """Reject forbidden source/resource tokens in a production source."""

        path_text = Path(path).as_posix()
        if path_text not in self.all_source_paths:
            raise PolicyError(f"source path is not in the closed policy: {path_text}")
        if not isinstance(text, str):
            raise PolicyError(f"source text for {path_text} must be a string")
        for pattern in self.forbidden_source_patterns:
            # Alphabetic resource names are matched at identifier boundaries;
            # otherwise an innocent word such as ``parameter`` would contain
            # the hard-resource marker ``RAM``.  Underscored vendor names are
            # intentionally still matched as part of their identifier.
            if pattern.isalpha():
                expression = rf"(?<![A-Za-z]){re.escape(pattern)}(?![A-Za-z])"
            else:
                expression = re.escape(pattern)
            if re.search(expression, text, flags=re.IGNORECASE):
                raise PolicyError(f"forbidden source pattern {pattern!r} in {path_text}")

        if path_text not in self.sources:
            return
        if re.search(r"`\s*include\b", text):
            raise PolicyError(f"source includes are not permitted in {path_text}")
        for identifier, expected in self.required_source_identifiers.items():
            expression = rf"(?<![A-Za-z0-9_$]){re.escape(identifier)}(?![A-Za-z0-9_$])"
            actual = len(re.findall(expression, text))
            if actual != expected:
                raise PolicyError(
                    f"source identifier {identifier!r} in {path_text} must occur exactly "
                    f"{expected} time(s), got {actual}"
                )

    def validate_synth_json(self, path: Path) -> None:
        """Require exact Yosys cell counts that nextpnr utilization may omit."""

        if not self.required_synth_cells:
            return
        try:
            design = json.loads(Path(path).read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            raise PolicyError(f"cannot read synth json {path}: {exc}") from exc
        if not isinstance(design, Mapping):
            raise PolicyError("synth json must be an object")
        modules = design.get("modules")
        if not isinstance(modules, Mapping):
            raise PolicyError("synth json has no modules")
        counts: dict[str, int] = {}
        for module in modules.values():
            if not isinstance(module, Mapping):
                continue
            cells = module.get("cells")
            if not isinstance(cells, Mapping):
                continue
            for cell in cells.values():
                if not isinstance(cell, Mapping):
                    continue
                cell_type = cell.get("type")
                if isinstance(cell_type, str) and cell_type:
                    counts[cell_type] = counts.get(cell_type, 0) + 1
        for name, expected in self.required_synth_cells.items():
            actual = counts.get(name, 0)
            if actual != expected:
                raise PolicyError(
                    f"synth cell {name} must occur exactly {expected} time(s), got {actual}"
                )

    def validate_design(self, *, top: str, sources: Sequence[str]) -> None:
        """Validate the top and exact production source list."""

        if top != self.top:
            raise PolicyError(f"top must be {self.top!r}, got {top!r}")
        if tuple(sources) != self.sources:
            raise PolicyError(f"source list does not match policy for {self.name}")

    @property
    def synth_intel_alm_flags(self) -> tuple[str, ...]:
        flags: list[str] = []
        if self.nobram:
            flags.append("-nobram")
        if self.nolutram:
            flags.append("-nolutram")
        if self.nodsp:
            flags.append("-nodsp")
        return tuple(flags)

    def as_dict(self) -> dict[str, Any]:
        return {
            "name": self.name,
            "sources": list(self.sources),
            "constraints": list(self.constraints),
            "top": self.top,
            "clock": self.clock,
            "clock_mhz": float(self.clock_mhz),
            "clock_period_ns": self.clock_period_ns,
            "clock_evidence_names": list(self.clock_evidence_names),
            **({"additional_clocks_mhz": dict(self.additional_clocks_mhz)}
               if self.additional_clocks_mhz else {}),
            "allowed_hard_blocks": dict(self.allowed_hard_blocks),
            "forbidden_source_patterns": list(self.forbidden_source_patterns),
            "forbidden_resource_patterns": list(self.forbidden_resource_patterns),
            "required_source_identifiers": dict(self.required_source_identifiers),
            "target": self.target,
            "artifact": self.artifact,
            "nobram": self.nobram,
            "nolutram": self.nolutram,
            "nodsp": self.nodsp,
            "yosys_post_synth": self.yosys_post_synth,
            "sim_jobs": [job.as_dict() for job in self.sim_jobs],
            "required_synth_cells": dict(self.required_synth_cells),
        }


_COMMON_SOURCE_PATTERNS = (
    "PLL",
    "phase_locked",
    "M10K",
    "BRAM",
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
    "HDMI",
    "altsyncram",
    "lpm_ram",
)
_COMMON_RESOURCE_PATTERNS = (
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
)


_POLICIES: Mapping[str, ExperimentPolicy] = MappingProxyType(
    {
        "010_blinky": ExperimentPolicy(
            name="010_blinky",
            sources=("experiments/010_blinky/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={},
            forbidden_source_patterns=(*_COMMON_SOURCE_PATTERNS, "HPS", "MPU", "ARM"),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            yosys_post_synth=r"cd top; rename LED \LED[0]; ",
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=("experiments/010_blinky/rtl/top.v",),
                    tb="experiments/010_blinky/sim/tb.cpp",
                    parameters={"COUNTER_BITS": "4"},
                ),
            ),
        ),
        "020_linux_mailbox": ExperimentPolicy(
            name="020_linux_mailbox",
            sources=("experiments/020_linux_mailbox/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={"cyclonev_hps_interface_mpu_general_purpose": 1},
            forbidden_source_patterns=(*_COMMON_SOURCE_PATTERNS, "LED", "GPIO", "external_gpio"),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            clock_evidence_names=("protocol.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/020_linux_mailbox/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/020_linux_mailbox/sim/tb.cpp",
                ),
                SimJob(
                    name="wrap",
                    top="mailbox_fsm",
                    sources=("experiments/020_linux_mailbox/rtl/top.v",),
                    tb="experiments/020_linux_mailbox/sim/tb.cpp",
                    parameters={"START_SEQUENCE": "8'hff", "MESSAGE_BYTES": "2"},
                    cflags=("-DMAILBOX_WRAP",),
                    lint=False,
                ),
            ),
        ),
        "030_m10k_rom": ExperimentPolicy(
            name="030_m10k_rom",
            sources=("experiments/030_m10k_rom/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={"MISTRAL_M10K": 1},
            forbidden_source_patterns=(*_COMMON_SOURCE_PATTERNS, "HPS", "MPU", "ARM"),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            nobram=False,
            yosys_post_synth=r"cd top; rename LED \LED[0]; ",
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=("experiments/030_m10k_rom/rtl/top.v",),
                    tb="experiments/030_m10k_rom/sim/tb.cpp",
                    parameters={"ADDR_BITS": "4"},
                ),
            ),
        ),
        "040_mlab_ram": ExperimentPolicy(
            name="040_mlab_ram",
            sources=("experiments/040_mlab_ram/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            forbidden_source_patterns=(
                *(pattern for pattern in _COMMON_SOURCE_PATTERNS if pattern != "MLAB"),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            required_synth_cells={"MISTRAL_MLAB": 8},
            nolutram=False,
            clock_evidence_names=("storage.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/040_mlab_ram/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/040_mlab_ram/sim/tb.cpp",
                ),
            ),
        ),
        "050_lut_mul": ExperimentPolicy(
            name="050_lut_mul",
            sources=("experiments/050_lut_mul/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            forbidden_source_patterns=(
                *_COMMON_SOURCE_PATTERNS,
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            clock_evidence_names=("product.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/050_lut_mul/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/050_lut_mul/sim/tb.cpp",
                ),
            ),
        ),
        "060_dsp_mul": ExperimentPolicy(
            name="060_dsp_mul",
            sources=("experiments/060_dsp_mul/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_MUL9X9": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"DSP", "MAC", "MUL"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            required_synth_cells={"MISTRAL_MUL9X9": 1},
            nodsp=False,
            clock_evidence_names=("product.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/060_dsp_mul/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/060_dsp_mul/sim/tb.cpp",
                ),
            ),
        ),
        "070_mixed_mem": ExperimentPolicy(
            name="070_mixed_mem",
            sources=("experiments/070_mixed_mem/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"MLAB", "M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            required_synth_cells={"MISTRAL_MLAB": 8},
            nobram=False,
            nolutram=False,
            clock_evidence_names=("storage.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/070_mixed_mem/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/070_mixed_mem/sim/tb.cpp",
                ),
            ),
        ),
        "080_dsp_mem": ExperimentPolicy(
            name="080_dsp_mem",
            sources=("experiments/080_dsp_mem/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K": 1,
                "MISTRAL_MUL9X9": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"MLAB", "M10K", "DSP", "MAC", "MUL"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            required_synth_cells={"MISTRAL_MLAB": 8, "MISTRAL_MUL9X9": 1},
            nobram=False,
            nolutram=False,
            nodsp=False,
            clock_evidence_names=(
                "storage.FPGA_CLK1_50",
                "product.FPGA_CLK1_50",
            ),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/080_dsp_mem/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/080_dsp_mem/sim/tb.cpp",
                ),
            ),
        ),
        "090_pll_clock": ExperimentPolicy(
            name="090_pll_clock",
            sources=("experiments/090_pll_clock/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"clk25": 25.0},
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
            },
            forbidden_source_patterns=tuple(
                pattern for pattern in _COMMON_SOURCE_PATTERNS
                if pattern not in {"PLL", "phase_locked"}
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
            },
            required_synth_cells={"altera_pll": 1},
            sim_jobs=(
                SimJob(
                    name="main", top="top",
                    sources=(
                        "experiments/090_pll_clock/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/090_pll_clock/sim/pll_model.v",
                    ),
                    tb="experiments/090_pll_clock/sim/tb.cpp",
                ),
                SimJob(
                    name="meter", top="pll_meter",
                    sources=("experiments/090_pll_clock/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "100_dsp_rom": ExperimentPolicy(
            name="100_dsp_rom",
            sources=("experiments/100_dsp_rom/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K": 1,
                "MISTRAL_MUL9X9": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"M10K", "DSP", "MAC", "MUL"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            required_synth_cells={"MISTRAL_MUL9X9": 1},
            nobram=False,
            nodsp=False,
            clock_evidence_names=(
                "rom_port.FPGA_CLK1_50",
                "product.FPGA_CLK1_50",
            ),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/100_dsp_rom/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/100_dsp_rom/sim/tb.cpp",
                ),
            ),
        ),
    }
)


def policy_for(name: str) -> ExperimentPolicy:
    """Return the frozen policy for *name*, rejecting every other experiment."""

    if not isinstance(name, str) or name not in _POLICIES:
        raise PolicyError(f"unknown experiment: {name!r}")
    return _POLICIES[name]


def validate_source_list(name: str, sources: Sequence[str], *, top: str = "top") -> None:
    """Validate a selected experiment's top and exact production source list."""

    policy_for(name).validate_design(top=top, sources=sources)


def classify_resource(name: str, *, experiment: str = "010_blinky") -> str:
    """Classify one utilization key using an experiment's closed policy."""

    return policy_for(experiment).classify_resource(name)


def validate_resources(experiment: str, resources: Mapping[str, Any]) -> None:
    """Validate utilization records with the selected experiment policy."""

    policy_for(experiment).validate_resources(resources)


def policies() -> Mapping[str, ExperimentPolicy]:
    """Return the closed policy table without exposing mutable internals."""

    return _POLICIES


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _has_symlink_component(path: Path) -> bool:
    current = Path(path.anchor)
    for component in path.parts[1:]:
        current /= component
        if current.is_symlink():
            return True
    return False


def _check_sources(policy: ExperimentPolicy, root: Path) -> None:
    policy.validate_design(top=policy.top, sources=policy.sources)
    for relative in policy.sources:
        path = root / relative
        if _has_symlink_component(path):
            raise PolicyError(f"production source path contains a symlink: {relative}")
        if not path.is_file():
            raise PolicyError(f"missing production source: {relative}")
        try:
            text = path.read_text(encoding="utf-8")
        except OSError as exc:
            raise PolicyError(f"cannot read production source {relative}: {exc}") from exc
        policy.validate_source_text(relative, text)


def _shell_lines(policy: ExperimentPolicy) -> str:
    # Values originate solely from this closed table.  Keep the output a
    # fixed-key, line-oriented protocol so build_oss.sh never needs eval.
    lines = [
        f"name={policy.name}",
        f"source={policy.sources[0]}",
        f"sources={json.dumps(list(policy.sources), separators=(',', ':'))}",
        f"top={policy.top}",
        f"clock={policy.clock}",
        f"clock_mhz={policy.clock_mhz:g}",
        f"qsf={policy.constraints[0]}",
        f"sdc={policy.constraints[1]}",
        f"artifact={policy.artifact}",
        f"nobram={1 if policy.nobram else 0}",
        f"nolutram={1 if policy.nolutram else 0}",
        f"nodsp={1 if policy.nodsp else 0}",
        f"yosys_post_synth={policy.yosys_post_synth}",
        "allowed_hard_blocks=" + json.dumps(dict(policy.allowed_hard_blocks), sort_keys=True, separators=(",", ":")),
    ]
    return "\n".join(lines) + "\n"


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--experiment", required=True)
    parser.add_argument("--format", choices=("json", "shell"), default="json")
    parser.add_argument("--check-sources", action="store_true")
    parser.add_argument("--check-synth-json", type=Path)
    parser.add_argument("--repo-root", type=Path, default=_repo_root())
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    try:
        arguments = _parser().parse_args(argv)
        policy = policy_for(arguments.experiment)
        if arguments.check_sources:
            _check_sources(policy, arguments.repo_root)
        if arguments.check_synth_json is not None:
            policy.validate_synth_json(arguments.check_synth_json)
        if arguments.format == "shell":
            sys.stdout.write(_shell_lines(policy))
        else:
            sys.stdout.write(json.dumps(policy.as_dict(), sort_keys=True, separators=(",", ":")) + "\n")
        return 0
    except (PolicyError, OSError, ValueError) as exc:
        print(f"experiment_policy: {' '.join(str(exc).splitlines())}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
