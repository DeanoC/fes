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
    {"MISTRAL_BUF", "MISTRAL_CLKENA", "MISTRAL_COMB", "MISTRAL_FF", "MISTRAL_IO", "MISTRAL_DDROUT", "MISTRAL_SDROUT", "MISTRAL_SDRIN", "MISTRAL_DDRIN", "MISTRAL_DDRBIDIR"}
)
MLAB_INIT_CELL = re.compile(r"^storage\.stored\.([0-7])\.0\.0$")


def mlab_init_byte(address: int) -> int:
    """Return the closed 32-by-8 MLAB power-up byte at *address*."""

    return ((int(address) * 73) ^ (int(address) >> 1) ^ 0xA6) & 0xFF


def mlab_init_lane(bit: int) -> int:
    """Return the 32-bit INIT word for MLAB lane *bit*."""

    return sum(((mlab_init_byte(address) >> int(bit)) & 1) << address for address in range(32))


def json_bit_parameter(value: Any) -> int | None:
    """Parse a Yosys JSON binary/hex/decimal parameter as an unbounded integer."""

    if isinstance(value, bool) or value is None:
        return None
    if isinstance(value, int):
        return int(value)
    if isinstance(value, str):
        text = value.strip().lower().replace("_", "")
        if text.startswith("0x"):
            return int(text, 16)
        if text and set(text) <= set("01xz"):
            return int(text.replace("x", "0").replace("z", "0"), 2)
        if text.isdigit():
            return int(text, 10)
    return None


def mlab_init_parameter(value: Any) -> int | None:
    """Parse a Yosys JSON INIT value as a 32-bit integer."""

    parsed = json_bit_parameter(value)
    return None if parsed is None else parsed & 0xFFFFFFFF


def m10k_init_word(address: int, width: int) -> int:
    """Return the closed M10K power-up word at *address* for 10-, 20- or 40-bit tables."""

    if width not in (10, 20, 40):
        raise PolicyError(f"unsupported M10K width {width}")
    low = ((int(address) * 73) ^ (int(address) >> 1) ^ 0xA6) & 0xFFFFF
    if width == 10:
        return low & 0x3FF
    if width == 40:
        return ((~low & 0xFFFFF) << 20) | low
    return low


def m10k_mixed_lane(address: int) -> int:
    """Return the closed mixed-width 10-bit power-up lane at *address*."""

    return ((int(address) * 73) ^ (int(address) >> 1) ^ 0xA6) & 0x3FF


def m10k_tdp_byte_physical_init(address: int, width: int) -> int:
    """Return the physical 20-bit INIT word for a byte-masked TDP table."""

    if width not in (16, 20):
        raise PolicyError(f"unsupported TDP byte-enable width {width}")
    logical = ((int(address) * 73) ^ (int(address) >> 1) ^ 0xA6) & ((1 << width) - 1)
    if width == 20:
        return logical
    lane = width // 2
    mask = (1 << lane) - 1
    return (logical & mask) | (((logical >> lane) & mask) << 10)


def m10k_tdp_abits(width: int) -> int:
    """Return CFG_ABITS for an equal-width 10- or 20-bit true dual-port M10K."""

    if width == 10:
        return 10
    if width == 20:
        return 9
    raise PolicyError(f"unsupported TDP M10K width {width}")


def m10k_tdp_mixed_physical_dbits(logical: int) -> int:
    """Return the physical port width for a mixed-TDP logical payload."""

    if logical in (8, 10):
        return 10
    if logical in (16, 20):
        return 20
    raise PolicyError(f"unsupported mixed-TDP payload width {logical}")


def m10k_tdp_mixed_unit(logical_a: int, logical_b: int) -> int:
    """Return the lane payload width for a mixed-TDP pair."""

    if logical_a in (8, 16) or logical_b in (8, 16):
        return 8
    if logical_a in (10, 20) and logical_b in (10, 20):
        return 10
    raise PolicyError(f"unsupported mixed-TDP pair {logical_a}/{logical_b}")


def m10k_tdp_mixed_lane(address: int, unit: int) -> int:
    """Return the canonical mixed-TDP INIT lane at *address*."""

    if unit not in (8, 10):
        raise PolicyError(f"unsupported mixed-TDP unit {unit}")
    return ((int(address) * 73) ^ (int(address) >> 1) ^ 0xA6) & ((1 << unit) - 1)


def m10k_mixed_abits(dbits: int) -> int:
    """Return CFG_ABITS / CFG_RD_ABITS for a 10-, 20- or 40-bit mixed-width port."""

    if dbits == 10:
        return 10
    if dbits == 20:
        return 9
    if dbits == 40:
        return 8
    raise PolicyError(f"unsupported mixed-width M10K port {dbits}")


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
    required_packed_sites: Mapping[str, int] = MappingProxyType({})
    synth_json_input_ports: Mapping[str, tuple[str, ...]] = MappingProxyType({})
    synth_json_tied_low: Mapping[str, tuple[str, ...]] = MappingProxyType({})
    synth_json_mlab_init: bool = False
    m10k_byte_enable: bool = False
    m10k_dual_clock_width: int = 0
    m10k_mixed_write_dbits: int = 0
    m10k_mixed_read_dbits: int = 0
    m10k_tdp_width: int = 0
    m10k_tdp_byte_width: int = 0
    m10k_tdp_mixed_a: int = 0
    m10k_tdp_mixed_b: int = 0
    nextpnr_router: str = ""
    require_read_clock_arc: bool = False
    m10k_aclr1_gpo_bit: int | None = None
    m10k_require_aclr1: bool = False
    m10k_tdp_constant_clk2: bool = False
    m10k_async_read: bool = False
    m10k_addrstalla_gpo_bit: int | None = None
    m10k_out_reg_b: bool = False

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
        for flag_name, flag in (
            ("nobram", self.nobram),
            ("nolutram", self.nolutram),
            ("nodsp", self.nodsp),
            ("synth_json_mlab_init", self.synth_json_mlab_init),
            ("m10k_byte_enable", self.m10k_byte_enable),
            ("require_read_clock_arc", self.require_read_clock_arc),
            ("m10k_require_aclr1", self.m10k_require_aclr1),
            ("m10k_tdp_constant_clk2", self.m10k_tdp_constant_clk2),
            ("m10k_async_read", self.m10k_async_read),
            ("m10k_out_reg_b", self.m10k_out_reg_b),
        ):
            if not isinstance(flag, bool):
                raise PolicyError(f"{self.name}: {flag_name} must be a boolean")
        if self.m10k_dual_clock_width not in (0, 20, 40):
            raise PolicyError(f"{self.name}: m10k_dual_clock_width must be 0, 20, or 40")
        if self.m10k_aclr1_gpo_bit is not None:
            if not isinstance(self.m10k_aclr1_gpo_bit, int) or isinstance(self.m10k_aclr1_gpo_bit, bool):
                raise PolicyError(f"{self.name}: m10k_aclr1_gpo_bit must be an integer or None")
            if not 0 <= self.m10k_aclr1_gpo_bit <= 31:
                raise PolicyError(f"{self.name}: m10k_aclr1_gpo_bit must be in 0..31")
        if self.m10k_addrstalla_gpo_bit is not None:
            if not isinstance(self.m10k_addrstalla_gpo_bit, int) or isinstance(self.m10k_addrstalla_gpo_bit, bool):
                raise PolicyError(f"{self.name}: m10k_addrstalla_gpo_bit must be an integer or None")
            if not 0 <= self.m10k_addrstalla_gpo_bit <= 31:
                raise PolicyError(f"{self.name}: m10k_addrstalla_gpo_bit must be in 0..31")
        mixed_ports = (self.m10k_mixed_write_dbits, self.m10k_mixed_read_dbits)
        if mixed_ports == (0, 0):
            pass
        elif mixed_ports[0] in (10, 20, 40) and mixed_ports[1] in (10, 20, 40) and mixed_ports[0] != mixed_ports[1]:
            pass
        else:
            raise PolicyError(
                f"{self.name}: mixed-width ports must be an unequal pair of 10, 20, or 40 bits"
            )
        if self.m10k_tdp_width not in (0, 10, 20):
            raise PolicyError(f"{self.name}: m10k_tdp_width must be 0, 10, or 20")
        if self.m10k_tdp_byte_width not in (0, 16, 20):
            raise PolicyError(f"{self.name}: m10k_tdp_byte_width must be 0, 16, or 20")
        mixed_tdp = (self.m10k_tdp_mixed_a, self.m10k_tdp_mixed_b)
        if mixed_tdp == (0, 0):
            pass
        elif mixed_tdp in ((20, 10), (10, 20), (16, 8), (8, 16)):
            pass
        else:
            raise PolicyError(
                f"{self.name}: mixed-TDP ports must be 20/10, 10/20, 16/8, or 8/16"
            )
        if self.m10k_mixed_write_dbits and self.m10k_byte_enable and self.m10k_mixed_write_dbits != 20:
            raise PolicyError(f"{self.name}: mixed-width byte enables require a 20-bit write port")
        if self.m10k_mixed_write_dbits and (
            self.m10k_dual_clock_width
            or self.m10k_tdp_width
            or self.m10k_tdp_byte_width
            or self.m10k_tdp_mixed_a
        ):
            raise PolicyError(f"{self.name}: mixed-width M10K cannot combine equal-width dual-clock or TDP policy")
        if self.m10k_tdp_width and (
            self.m10k_byte_enable
            or self.m10k_dual_clock_width
            or self.m10k_tdp_byte_width
            or self.m10k_tdp_mixed_a
        ):
            raise PolicyError(f"{self.name}: TDP M10K cannot combine byte-enable, equal-width dual-clock, TDP byte-enable, or mixed-TDP policy")
        if self.m10k_tdp_byte_width and (
            self.m10k_byte_enable or self.m10k_dual_clock_width or self.m10k_tdp_mixed_a
        ):
            raise PolicyError(f"{self.name}: TDP byte-enable M10K cannot combine SDP byte-enable, equal-width dual-clock, or mixed-TDP policy")
        if self.m10k_tdp_mixed_a and (self.m10k_byte_enable or self.m10k_dual_clock_width):
            raise PolicyError(f"{self.name}: mixed-TDP M10K cannot combine SDP byte-enable or equal-width dual-clock policy")
        if self.nextpnr_router not in ("", "router1"):
            raise PolicyError(f"{self.name}: nextpnr_router must be empty or router1")
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

        packed: dict[str, int] = {}
        for cell, count in dict(self.required_packed_sites).items():
            if not isinstance(cell, str) or not cell:
                raise PolicyError(f"{self.name}: packed site names must be non-empty strings")
            if not isinstance(count, int) or isinstance(count, bool) or count <= 0:
                raise PolicyError(f"{self.name}: packed site count for {cell} must be a positive integer")
            expected_cells = synth_cells.get(cell)
            if expected_cells is None:
                raise PolicyError(f"{self.name}: packed site {cell} must also be a required synth cell")
            if count > expected_cells:
                raise PolicyError(
                    f"{self.name}: packed site count for {cell} cannot exceed the required synth cell count"
                )
            packed[cell] = count
        object.__setattr__(self, "required_packed_sites", MappingProxyType(packed))

        input_ports: dict[str, tuple[str, ...]] = {}
        for cell, ports in dict(self.synth_json_input_ports).items():
            if not isinstance(cell, str) or not cell:
                raise PolicyError(f"{self.name}: synth json input-port cell names must be non-empty strings")
            names = tuple(ports)
            if not names or any(not isinstance(port, str) or not re.fullmatch(r"[A-Za-z_]\w*", port) for port in names):
                raise PolicyError(f"{self.name}: synth json input ports for {cell} must be Verilog identifiers")
            input_ports[cell] = names
        object.__setattr__(self, "synth_json_input_ports", MappingProxyType(input_ports))

        tied_low: dict[str, tuple[str, ...]] = {}
        for cell, ports in dict(self.synth_json_tied_low).items():
            if not isinstance(cell, str) or not cell:
                raise PolicyError(f"{self.name}: synth json tied-low cell names must be non-empty strings")
            names = tuple(ports)
            if not names or any(not isinstance(port, str) or not re.fullmatch(r"[A-Za-z_]\w*", port) for port in names):
                raise PolicyError(f"{self.name}: synth json tied-low ports for {cell} must be Verilog identifiers")
            tied_low[cell] = names
        object.__setattr__(self, "synth_json_tied_low", MappingProxyType(tied_low))

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
        if self.synth_json_mlab_init:
            self._require_mlab_init(design)
        if self.m10k_byte_enable and not self.m10k_mixed_write_dbits:
            self._require_m10k_byte_enable(design)
        if self.m10k_dual_clock_width:
            self._require_m10k_dual_clock(design)
        if self.m10k_aclr1_gpo_bit is not None or self.m10k_require_aclr1:
            self._require_m10k_aclr1(design)
        if self.m10k_mixed_write_dbits:
            self._require_m10k_mixed_width(design)
        if self.m10k_tdp_width:
            self._require_m10k_tdp(design)
        if self.m10k_tdp_byte_width:
            self._require_m10k_tdp_byte(design)
        if self.m10k_tdp_mixed_a:
            self._require_m10k_tdp_mixed(design)
        if self.m10k_async_read:
            self._require_m10k_async_read(design)
        if self.m10k_addrstalla_gpo_bit is not None:
            self._require_m10k_addrstalla(design)
        if self.m10k_out_reg_b:
            self._require_m10k_out_reg_b(design)

    def _mlab_init_cells(self, design: Mapping[str, Any]) -> dict[int, dict[str, Any]]:
        modules = design.get("modules")
        if not isinstance(modules, Mapping):
            raise PolicyError("synth json has no modules")
        found: dict[int, dict[str, Any]] = {}
        for module in modules.values():
            if not isinstance(module, Mapping):
                continue
            cells = module.get("cells")
            if not isinstance(cells, Mapping):
                continue
            for name, cell in cells.items():
                if not isinstance(name, str) or not isinstance(cell, Mapping):
                    continue
                if cell.get("type") != "MISTRAL_MLAB":
                    continue
                match = MLAB_INIT_CELL.fullmatch(name)
                if match is None:
                    raise PolicyError(f"unexpected MLAB cell name {name!r}")
                bit = int(match.group(1))
                if bit in found:
                    raise PolicyError(f"duplicate MLAB init lane {bit}")
                found[bit] = cell
        if set(found) != set(range(8)):
            raise PolicyError(
                "synth json must contain MLAB lanes 0-7, got " + ",".join(str(bit) for bit in sorted(found))
            )
        return found

    def _require_mlab_init(self, design: Mapping[str, Any]) -> None:
        for bit, cell in self._mlab_init_cells(design).items():
            parameters = cell.get("parameters")
            if not isinstance(parameters, Mapping):
                raise PolicyError(f"MLAB lane {bit} has no parameters")
            actual = mlab_init_parameter(parameters.get("INIT"))
            expected = mlab_init_lane(bit)
            if actual != expected:
                raise PolicyError(
                    f"MLAB lane {bit} INIT must be {expected:032b}, got {parameters.get('INIT')!r}"
                )

    def _m10k_cells(self, design: Mapping[str, Any], cell_type: str = "MISTRAL_M10K") -> list[dict[str, Any]]:
        modules = design.get("modules")
        if not isinstance(modules, Mapping):
            raise PolicyError("synth json has no modules")
        found: list[dict[str, Any]] = []
        for module in modules.values():
            if not isinstance(module, Mapping):
                continue
            cells = module.get("cells")
            if not isinstance(cells, Mapping):
                continue
            for cell in cells.values():
                if isinstance(cell, Mapping) and cell.get("type") == cell_type:
                    found.append(cell)
        return found

    def _require_m10k_byte_enable(self, design: Mapping[str, Any]) -> None:
        cells = self._m10k_cells(design)
        if len(cells) != 1:
            raise PolicyError(f"synth json must contain exactly one MISTRAL_M10K, got {len(cells)}")
        cell = cells[0]
        parameters = cell.get("parameters")
        connections = cell.get("connections")
        if not isinstance(parameters, Mapping) or not isinstance(connections, Mapping):
            raise PolicyError("M10K cell is missing parameters or connections")
        if json_bit_parameter(parameters.get("CFG_BYTE_ENABLE")) != 1:
            raise PolicyError(
                f"M10K CFG_BYTE_ENABLE must be 1, got {parameters.get('CFG_BYTE_ENABLE')!r}"
            )
        if json_bit_parameter(parameters.get("CFG_DUAL_CLOCK")) != 1:
            raise PolicyError(
                f"M10K CFG_DUAL_CLOCK must be 1, got {parameters.get('CFG_DUAL_CLOCK')!r}"
            )
        if json_bit_parameter(parameters.get("CFG_DBITS")) != 20:
            raise PolicyError(f"M10K CFG_DBITS must be 20, got {parameters.get('CFG_DBITS')!r}")
        if json_bit_parameter(parameters.get("CFG_ABITS")) != 9:
            raise PolicyError(f"M10K CFG_ABITS must be 9, got {parameters.get('CFG_ABITS')!r}")
        clk1 = connections.get("CLK1")
        clk2 = connections.get("CLK2")
        if not isinstance(clk1, list) or not clk1:
            raise PolicyError("M10K CLK1 must be connected")
        if not isinstance(clk2, list) or not clk2:
            raise PolicyError("M10K CLK2 must be connected")
        if clk1 == clk2:
            raise PolicyError("M10K CLK1 and CLK2 must be independent")
        byte_enables = connections.get("A1BE")
        if not isinstance(byte_enables, list) or len(byte_enables) != 2:
            raise PolicyError("M10K A1BE must be a two-bit connected port")
        if byte_enables[0] == byte_enables[1]:
            raise PolicyError("M10K A1BE lanes must be independent")
        write_enable = connections.get("A1EN")
        if not isinstance(write_enable, list) or not write_enable:
            raise PolicyError("M10K A1EN must be connected")
        init = json_bit_parameter(parameters.get("INIT"))
        if init is None:
            raise PolicyError("M10K INIT is missing")
        mask = (1 << 20) - 1
        for address in (0, 1, 2, 7, 15, 31, 63, 127, 255):
            actual = (init >> (address * 20)) & mask
            expected = m10k_init_word(address, 20)
            if actual != expected:
                raise PolicyError(
                    f"M10K INIT address {address} must be {expected:#x}, got {actual:#x}"
                )

    def _require_m10k_dual_clock(self, design: Mapping[str, Any]) -> None:
        width = self.m10k_dual_clock_width
        cells = self._m10k_cells(design)
        if len(cells) != 1:
            raise PolicyError(f"synth json must contain exactly one MISTRAL_M10K, got {len(cells)}")
        cell = cells[0]
        parameters = cell.get("parameters")
        connections = cell.get("connections")
        if not isinstance(parameters, Mapping) or not isinstance(connections, Mapping):
            raise PolicyError("M10K cell is missing parameters or connections")
        dual = json_bit_parameter(parameters.get("CFG_DUAL_CLOCK"))
        dbits = json_bit_parameter(parameters.get("CFG_DBITS"))
        abits = json_bit_parameter(parameters.get("CFG_ABITS"))
        init = json_bit_parameter(parameters.get("INIT"))
        if dual != 1:
            raise PolicyError(f"M10K CFG_DUAL_CLOCK must be 1, got {parameters.get('CFG_DUAL_CLOCK')!r}")
        if dbits != width:
            raise PolicyError(f"M10K CFG_DBITS must be {width}, got {parameters.get('CFG_DBITS')!r}")
        expected_abits = 8 if width == 40 else 9
        if abits != expected_abits:
            raise PolicyError(
                f"M10K CFG_ABITS must be {expected_abits}, got {parameters.get('CFG_ABITS')!r}"
            )
        clk1 = connections.get("CLK1")
        clk2 = connections.get("CLK2")
        if not isinstance(clk1, list) or not clk1:
            raise PolicyError("M10K CLK1 must be connected")
        if not isinstance(clk2, list) or not clk2:
            raise PolicyError("M10K CLK2 must be connected")
        if clk1 == clk2:
            raise PolicyError("M10K CLK1 and CLK2 must be independent")
        if init is None:
            raise PolicyError("M10K INIT is missing")
        mask = (1 << width) - 1
        for address in (0, 1, 2, 7, 15, 31, 63, 127, 255):
            actual = (init >> (address * width)) & mask
            expected = m10k_init_word(address, width)
            if actual != expected:
                raise PolicyError(
                    f"M10K INIT address {address} must be {expected:#x}, got {actual:#x}"
                )

    def _require_m10k_aclr1(self, design: Mapping[str, Any]) -> None:
        cells = self._m10k_cells(design)
        if len(cells) != 1:
            raise PolicyError(f"synth json must contain exactly one MISTRAL_M10K, got {len(cells)}")
        connections = cells[0].get("connections")
        if not isinstance(connections, Mapping):
            raise PolicyError("M10K cell is missing connections")
        aclr1 = connections.get("ACLR1")
        if not isinstance(aclr1, list) or len(aclr1) != 1:
            raise PolicyError("M10K ACLR1 must be a connected one-bit fabric net")
        if aclr1[0] in (0, 1, "0", "1"):
            raise PolicyError("M10K ACLR1 must be a connected fabric net")

    def _require_m10k_async_read(self, design: Mapping[str, Any]) -> None:
        cells = self._m10k_cells(design)
        if len(cells) != 1:
            raise PolicyError(f"synth json must contain exactly one MISTRAL_M10K, got {len(cells)}")
        cell = cells[0]
        parameters = cell.get("parameters")
        connections = cell.get("connections")
        if not isinstance(parameters, Mapping) or not isinstance(connections, Mapping):
            raise PolicyError("M10K cell is missing parameters or connections")
        if json_bit_parameter(parameters.get("CFG_ASYNC_READ")) != 1:
            raise PolicyError(
                f"M10K CFG_ASYNC_READ must be 1, got {parameters.get('CFG_ASYNC_READ')!r}"
            )
        dual = json_bit_parameter(parameters.get("CFG_DUAL_CLOCK"))
        if dual not in (None, 0):
            raise PolicyError(f"async-read M10K CFG_DUAL_CLOCK must be omitted or 0, got {parameters.get('CFG_DUAL_CLOCK')!r}")
        if "B1EN" in connections:
            raise PolicyError("async-read M10K must not connect B1EN")
        if "CLK2" in connections:
            raise PolicyError("async-read M10K must not connect CLK2")
        clk1 = connections.get("CLK1")
        if not isinstance(clk1, list) or not clk1 or clk1[0] in ("0", "1"):
            raise PolicyError("async-read M10K CLK1 must be a live fabric clock")
        if "B1ADDR" not in connections or "B1DATA" not in connections:
            raise PolicyError("async-read M10K must connect B1ADDR and B1DATA")

    def _require_m10k_addrstalla(self, design: Mapping[str, Any]) -> None:
        cells = self._m10k_cells(design, "MISTRAL_M10K_TDP")
        if len(cells) != 1:
            raise PolicyError(f"synth json must contain exactly one MISTRAL_M10K_TDP, got {len(cells)}")
        connections = cells[0].get("connections")
        if not isinstance(connections, Mapping):
            raise PolicyError("TDP M10K cell is missing connections")
        stall = connections.get("ADDRSTALLA")
        if not isinstance(stall, list) or len(stall) != 1 or stall[0] in (0, 1, "0", "1"):
            raise PolicyError("TDP M10K ADDRSTALLA must be a connected fabric net")

    def _require_m10k_out_reg_b(self, design: Mapping[str, Any]) -> None:
        cells = self._m10k_cells(design)
        if len(cells) != 1:
            raise PolicyError(f"synth json must contain exactly one MISTRAL_M10K, got {len(cells)}")
        cell = cells[0]
        parameters = cell.get("parameters")
        connections = cell.get("connections")
        if not isinstance(parameters, Mapping) or not isinstance(connections, Mapping):
            raise PolicyError("M10K cell is missing parameters or connections")
        if json_bit_parameter(parameters.get("CFG_OUT_REG_B")) != 1:
            raise PolicyError(
                f"M10K CFG_OUT_REG_B must be 1, got {parameters.get('CFG_OUT_REG_B')!r}"
            )
        out_a = json_bit_parameter(parameters.get("CFG_OUT_REG_A"))
        if out_a not in (None, 0):
            raise PolicyError(f"narrow SDP CFG_OUT_REG_A must be omitted or 0, got {parameters.get('CFG_OUT_REG_A')!r}")
        async_read = json_bit_parameter(parameters.get("CFG_ASYNC_READ"))
        if async_read not in (None, 0):
            raise PolicyError("registered-output M10K cannot set CFG_ASYNC_READ")
        clk2 = connections.get("CLK2")
        if not isinstance(clk2, list) or not clk2 or clk2[0] in ("0", "1"):
            raise PolicyError("registered-output M10K CLK2 must be a live fabric clock")
        if "B1DATA" not in connections or "B1ADDR" not in connections:
            raise PolicyError("registered-output M10K must connect B1ADDR and B1DATA")

    def _require_m10k_mixed_width(self, design: Mapping[str, Any]) -> None:
        write_bits = self.m10k_mixed_write_dbits
        read_bits = self.m10k_mixed_read_dbits
        cells = self._m10k_cells(design)
        if len(cells) != 1:
            raise PolicyError(f"synth json must contain exactly one MISTRAL_M10K, got {len(cells)}")
        cell = cells[0]
        parameters = cell.get("parameters")
        connections = cell.get("connections")
        if not isinstance(parameters, Mapping) or not isinstance(connections, Mapping):
            raise PolicyError("M10K cell is missing parameters or connections")
        if json_bit_parameter(parameters.get("CFG_MIXED_WIDTH")) != 1:
            raise PolicyError(
                f"M10K CFG_MIXED_WIDTH must be 1, got {parameters.get('CFG_MIXED_WIDTH')!r}"
            )
        if json_bit_parameter(parameters.get("CFG_DUAL_CLOCK")) != 1:
            raise PolicyError(
                f"M10K CFG_DUAL_CLOCK must be 1, got {parameters.get('CFG_DUAL_CLOCK')!r}"
            )
        byte_enable = json_bit_parameter(parameters.get("CFG_BYTE_ENABLE"))
        if self.m10k_byte_enable:
            if byte_enable != 1:
                raise PolicyError(
                    f"M10K CFG_BYTE_ENABLE must be 1, got {parameters.get('CFG_BYTE_ENABLE')!r}"
                )
            if write_bits != 20:
                raise PolicyError("mixed-width byte enables require a 20-bit write port")
            byte_enables = connections.get("A1BE")
            if not isinstance(byte_enables, list) or len(byte_enables) != 2:
                raise PolicyError("M10K A1BE must be a two-bit connected port")
            if byte_enables[0] == byte_enables[1]:
                raise PolicyError("M10K A1BE lanes must be independent")
        elif byte_enable not in (None, 0):
            raise PolicyError(
                f"M10K CFG_BYTE_ENABLE must be omitted or 0, got {parameters.get('CFG_BYTE_ENABLE')!r}"
            )
        if json_bit_parameter(parameters.get("CFG_DBITS")) != write_bits:
            raise PolicyError(f"M10K CFG_DBITS must be {write_bits}, got {parameters.get('CFG_DBITS')!r}")
        if json_bit_parameter(parameters.get("CFG_RD_DBITS")) != read_bits:
            raise PolicyError(
                f"M10K CFG_RD_DBITS must be {read_bits}, got {parameters.get('CFG_RD_DBITS')!r}"
            )
        expected_abits = m10k_mixed_abits(write_bits)
        expected_rd_abits = m10k_mixed_abits(read_bits)
        if json_bit_parameter(parameters.get("CFG_ABITS")) != expected_abits:
            raise PolicyError(
                f"M10K CFG_ABITS must be {expected_abits}, got {parameters.get('CFG_ABITS')!r}"
            )
        if json_bit_parameter(parameters.get("CFG_RD_ABITS")) != expected_rd_abits:
            raise PolicyError(
                f"M10K CFG_RD_ABITS must be {expected_rd_abits}, got {parameters.get('CFG_RD_ABITS')!r}"
            )
        clk1 = connections.get("CLK1")
        clk2 = connections.get("CLK2")
        if not isinstance(clk1, list) or not clk1:
            raise PolicyError("M10K CLK1 must be connected")
        if not isinstance(clk2, list) or not clk2:
            raise PolicyError("M10K CLK2 must be connected")
        if clk1 == clk2:
            raise PolicyError("M10K CLK1 and CLK2 must be independent")
        write_data = connections.get("A1DATA")
        read_data = connections.get("B1DATA")
        if not isinstance(write_data, list) or len(write_data) != write_bits:
            raise PolicyError(f"M10K A1DATA must be {write_bits} bits")
        if not isinstance(read_data, list) or len(read_data) != read_bits:
            raise PolicyError(f"M10K B1DATA must be {read_bits} bits")
        init = json_bit_parameter(parameters.get("INIT"))
        if init is None:
            raise PolicyError("M10K INIT is missing")
        for address in (0, 1, 2, 7, 15, 31, 63, 127, 255, 512, 1023):
            actual = (init >> (address * 10)) & 0x3FF
            expected = m10k_mixed_lane(address)
            if actual != expected:
                raise PolicyError(
                    f"M10K INIT lane {address} must be {expected:#x}, got {actual:#x}"
                )

    def _require_m10k_tdp(self, design: Mapping[str, Any]) -> None:
        width = self.m10k_tdp_width
        cells = self._m10k_cells(design, "MISTRAL_M10K_TDP")
        if len(cells) != 1:
            raise PolicyError(f"synth json must contain exactly one MISTRAL_M10K_TDP, got {len(cells)}")
        cell = cells[0]
        parameters = cell.get("parameters")
        connections = cell.get("connections")
        if not isinstance(parameters, Mapping) or not isinstance(connections, Mapping):
            raise PolicyError("TDP M10K cell is missing parameters or connections")
        mixed = json_bit_parameter(parameters.get("CFG_MIXED_WIDTH"))
        if mixed not in (None, 0):
            raise PolicyError(
                f"TDP M10K CFG_MIXED_WIDTH must be omitted or 0, got {parameters.get('CFG_MIXED_WIDTH')!r}"
            )
        byte_enable = json_bit_parameter(parameters.get("CFG_BYTE_ENABLE"))
        if byte_enable not in (None, 0):
            raise PolicyError(
                f"TDP M10K CFG_BYTE_ENABLE must be omitted or 0, got {parameters.get('CFG_BYTE_ENABLE')!r}"
            )
        if json_bit_parameter(parameters.get("CFG_DBITS")) != width:
            raise PolicyError(f"TDP M10K CFG_DBITS must be {width}, got {parameters.get('CFG_DBITS')!r}")
        expected_abits = m10k_tdp_abits(width)
        if json_bit_parameter(parameters.get("CFG_ABITS")) != expected_abits:
            raise PolicyError(
                f"TDP M10K CFG_ABITS must be {expected_abits}, got {parameters.get('CFG_ABITS')!r}"
            )
        clk1 = connections.get("CLK1")
        clk2 = connections.get("CLK2")
        if not isinstance(clk1, list) or not clk1:
            raise PolicyError("TDP M10K CLK1 must be connected")
        if not isinstance(clk2, list) or not clk2:
            raise PolicyError("TDP M10K CLK2 must be connected")
        if self.m10k_tdp_constant_clk2:
            if clk2 != ["0"]:
                raise PolicyError("TDP M10K CLK2 must be tied low")
            if clk1[0] in ("0", "1"):
                raise PolicyError("TDP M10K CLK1 must be a live fabric clock")
        elif clk1 == clk2:
            raise PolicyError("TDP M10K CLK1 and CLK2 must be independent")
        for port in ("A1EN", "B1EN", "A1WE", "B1WE"):
            nets = connections.get(port)
            if not isinstance(nets, list) or not nets:
                raise PolicyError(f"TDP M10K {port} must be connected")
        if self.m10k_tdp_constant_clk2:
            b1en = connections.get("B1EN")
            if b1en != ["0"]:
                raise PolicyError("TDP M10K B1EN must be tied low with constant CLK2")
        write_a = connections.get("A1DATA")
        write_b = connections.get("B1DATA")
        if not isinstance(write_a, list) or len(write_a) != width:
            raise PolicyError(f"TDP M10K A1DATA must be {width} bits")
        if not isinstance(write_b, list) or len(write_b) != width:
            raise PolicyError(f"TDP M10K B1DATA must be {width} bits")
        init = json_bit_parameter(parameters.get("INIT"))
        if init is None:
            raise PolicyError("TDP M10K INIT is missing")
        mask = (1 << width) - 1
        addresses = [0, 1, 2, 7, 15, 31, 63, 127, 255]
        if width == 10:
            addresses.extend((512, 1023))
        for address in addresses:
            actual = (init >> (address * width)) & mask
            expected = m10k_init_word(address, width)
            if actual != expected:
                raise PolicyError(
                    f"TDP M10K INIT address {address} must be {expected:#x}, got {actual:#x}"
                )

    def _require_m10k_tdp_byte(self, design: Mapping[str, Any]) -> None:
        width = self.m10k_tdp_byte_width
        cells = self._m10k_cells(design, "MISTRAL_M10K_TDP")
        if len(cells) != 1:
            raise PolicyError(f"synth json must contain exactly one MISTRAL_M10K_TDP, got {len(cells)}")
        cell = cells[0]
        parameters = cell.get("parameters")
        connections = cell.get("connections")
        if not isinstance(parameters, Mapping) or not isinstance(connections, Mapping):
            raise PolicyError("TDP M10K cell is missing parameters or connections")
        mixed = json_bit_parameter(parameters.get("CFG_MIXED_WIDTH"))
        if mixed not in (None, 0):
            raise PolicyError(
                f"TDP M10K CFG_MIXED_WIDTH must be omitted or 0, got {parameters.get('CFG_MIXED_WIDTH')!r}"
            )
        if json_bit_parameter(parameters.get("CFG_BYTE_ENABLE")) != 1:
            raise PolicyError(
                f"TDP M10K CFG_BYTE_ENABLE must be 1, got {parameters.get('CFG_BYTE_ENABLE')!r}"
            )
        if json_bit_parameter(parameters.get("CFG_DBITS")) != 20:
            raise PolicyError(f"TDP M10K CFG_DBITS must be 20, got {parameters.get('CFG_DBITS')!r}")
        if json_bit_parameter(parameters.get("CFG_ABITS")) != 9:
            raise PolicyError(f"TDP M10K CFG_ABITS must be 9, got {parameters.get('CFG_ABITS')!r}")
        clk1 = connections.get("CLK1")
        clk2 = connections.get("CLK2")
        if not isinstance(clk1, list) or not clk1:
            raise PolicyError("TDP M10K CLK1 must be connected")
        if not isinstance(clk2, list) or not clk2:
            raise PolicyError("TDP M10K CLK2 must be connected")
        if clk1 == clk2:
            raise PolicyError("TDP M10K CLK1 and CLK2 must be independent")
        for port in ("A1EN", "B1EN", "A1WE", "B1WE"):
            nets = connections.get(port)
            if not isinstance(nets, list) or not nets:
                raise PolicyError(f"TDP M10K {port} must be connected")
        for port in ("A1BE", "B1BE"):
            nets = connections.get(port)
            if not isinstance(nets, list) or len(nets) != 2:
                raise PolicyError(f"TDP M10K {port} must be a two-bit connected port")
            if nets[0] == nets[1]:
                raise PolicyError(f"TDP M10K {port} lanes must be independent")
        write_a = connections.get("A1DATA")
        write_b = connections.get("B1DATA")
        if not isinstance(write_a, list) or len(write_a) != 20:
            raise PolicyError("TDP M10K A1DATA must be 20 bits")
        if not isinstance(write_b, list) or len(write_b) != 20:
            raise PolicyError("TDP M10K B1DATA must be 20 bits")
        init = json_bit_parameter(parameters.get("INIT"))
        if init is None:
            raise PolicyError("TDP M10K INIT is missing")
        for address in (0, 1, 2, 7, 15, 31, 63, 127, 255, 511):
            actual = (init >> (address * 20)) & 0xFFFFF
            expected = m10k_tdp_byte_physical_init(address, width)
            if actual != expected:
                raise PolicyError(
                    f"TDP M10K INIT address {address} must be {expected:#x}, got {actual:#x}"
                )

    def _require_m10k_tdp_mixed(self, design: Mapping[str, Any]) -> None:
        logical_a = self.m10k_tdp_mixed_a
        logical_b = self.m10k_tdp_mixed_b
        physical_a = m10k_tdp_mixed_physical_dbits(logical_a)
        physical_b = m10k_tdp_mixed_physical_dbits(logical_b)
        unit = m10k_tdp_mixed_unit(logical_a, logical_b)
        cells = self._m10k_cells(design, "MISTRAL_M10K_TDP")
        if len(cells) != 1:
            raise PolicyError(f"synth json must contain exactly one MISTRAL_M10K_TDP, got {len(cells)}")
        cell = cells[0]
        parameters = cell.get("parameters")
        connections = cell.get("connections")
        if not isinstance(parameters, Mapping) or not isinstance(connections, Mapping):
            raise PolicyError("TDP M10K cell is missing parameters or connections")
        if json_bit_parameter(parameters.get("CFG_MIXED_WIDTH")) != 1:
            raise PolicyError(
                f"TDP M10K CFG_MIXED_WIDTH must be 1, got {parameters.get('CFG_MIXED_WIDTH')!r}"
            )
        byte_enable = json_bit_parameter(parameters.get("CFG_BYTE_ENABLE"))
        if byte_enable not in (None, 0):
            raise PolicyError(
                f"TDP M10K CFG_BYTE_ENABLE must be omitted or 0, got {parameters.get('CFG_BYTE_ENABLE')!r}"
            )
        got_a = json_bit_parameter(parameters.get("CFG_DBITS"))
        got_b = json_bit_parameter(parameters.get("CFG_RD_DBITS"))
        if {got_a, got_b} != {physical_a, physical_b}:
            raise PolicyError(
                f"TDP M10K mixed ports must be {physical_a}/{physical_b}, "
                f"got CFG_DBITS={parameters.get('CFG_DBITS')!r} "
                f"CFG_RD_DBITS={parameters.get('CFG_RD_DBITS')!r}"
            )
        expected_abits = m10k_tdp_abits(got_a)
        if json_bit_parameter(parameters.get("CFG_ABITS")) != expected_abits:
            raise PolicyError(
                f"TDP M10K CFG_ABITS must be {expected_abits}, got {parameters.get('CFG_ABITS')!r}"
            )
        expected_rd_abits = m10k_tdp_abits(got_b)
        if json_bit_parameter(parameters.get("CFG_RD_ABITS")) != expected_rd_abits:
            raise PolicyError(
                f"TDP M10K CFG_RD_ABITS must be {expected_rd_abits}, got {parameters.get('CFG_RD_ABITS')!r}"
            )
        clk1 = connections.get("CLK1")
        clk2 = connections.get("CLK2")
        if not isinstance(clk1, list) or not clk1:
            raise PolicyError("TDP M10K CLK1 must be connected")
        if not isinstance(clk2, list) or not clk2:
            raise PolicyError("TDP M10K CLK2 must be connected")
        if clk1 == clk2:
            raise PolicyError("TDP M10K CLK1 and CLK2 must be independent")
        for port in ("A1EN", "B1EN", "A1WE", "B1WE"):
            nets = connections.get(port)
            if not isinstance(nets, list) or not nets:
                raise PolicyError(f"TDP M10K {port} must be connected")
        write_a = connections.get("A1DATA")
        write_b = connections.get("B1DATA")
        if not isinstance(write_a, list) or len(write_a) != got_a:
            raise PolicyError(f"TDP M10K A1DATA must be {got_a} bits")
        if not isinstance(write_b, list) or len(write_b) != got_b:
            raise PolicyError(f"TDP M10K B1DATA must be {got_b} bits")
        init = json_bit_parameter(parameters.get("INIT"))
        if init is None:
            raise PolicyError("TDP M10K INIT is missing")
        addresses = [0, 1, 2, 7, 15, 31, 63, 127, 255, 511, 1023]
        for address in addresses:
            actual = (init >> (address * 10)) & 0x3FF
            expected = m10k_tdp_mixed_lane(address, unit)
            if actual != expected:
                raise PolicyError(
                    f"TDP M10K INIT lane {address} must be {expected:#x}, got {actual:#x}"
                )

    def validate_timing_report(self, timing: Mapping[str, Any]) -> None:
        """Require the independent 25 MHz read-clock arc when the closed policy asks for it."""

        if not self.require_read_clock_arc:
            return
        if not isinstance(timing, Mapping):
            raise PolicyError("timing report must be an object")
        paths = timing.get("critical_paths")
        if not isinstance(paths, list):
            raise PolicyError("timing report has no critical_paths")
        for path in paths:
            if not isinstance(path, Mapping):
                continue
            if path.get("from") != "posedge read_clock":
                continue
            delay = path.get("max_delay")
            if isinstance(delay, bool) or not isinstance(delay, (int, float)):
                continue
            if abs(float(delay) - 40.0) <= 1e-6:
                return
        raise PolicyError("timing report must include a 25 MHz read_clock critical path")

    def apply_synth_json(self, path: Path) -> None:
        """Fix Yosys JSON extras that nextpnr cannot consume as-is.

        Unknown ports on known library cells default to output.
        """

        if (
            not self.synth_json_input_ports
            and not self.synth_json_tied_low
            and self.m10k_aclr1_gpo_bit is None
            and not self.m10k_async_read
            and self.m10k_addrstalla_gpo_bit is None
            and not self.m10k_out_reg_b
        ):
            return
        try:
            design = json.loads(Path(path).read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            raise PolicyError(f"cannot read synth json {path}: {exc}") from exc
        if not isinstance(design, dict):
            raise PolicyError("synth json must be an object")
        modules = design.get("modules")
        if not isinstance(modules, dict):
            raise PolicyError("synth json has no modules")
        changed = False
        found: dict[str, int] = {}
        for module in modules.values():
            if not isinstance(module, dict):
                continue
            cells = module.get("cells")
            if not isinstance(cells, dict):
                continue
            if self._attach_m10k_aclr1(cells):
                changed = True
            if self._apply_m10k_async_read(cells):
                changed = True
            if self._attach_m10k_addrstalla(cells):
                changed = True
            if self._apply_m10k_out_reg_b(cells):
                changed = True
            for cell in cells.values():
                if not isinstance(cell, dict):
                    continue
                cell_type = cell.get("type")
                extra = self.synth_json_input_ports.get(cell_type, ()) if isinstance(cell_type, str) else ()
                tied = self.synth_json_tied_low.get(cell_type, ()) if isinstance(cell_type, str) else ()
                if not extra and not tied:
                    continue
                found[cell_type] = found.get(cell_type, 0) + 1
                connections = cell.get("connections")
                if not isinstance(connections, dict):
                    raise PolicyError(f"synth cell {cell_type} has no connections")
                directions = cell.get("port_directions")
                if not isinstance(directions, dict):
                    directions = {}
                    cell["port_directions"] = directions
                for port in extra:
                    if port not in connections:
                        raise PolicyError(f"synth cell {cell_type} is missing input port {port}")
                    if directions.get(port) != "input":
                        directions[port] = "input"
                        changed = True
                for port in tied:
                    if connections.get(port) != ["0"] or directions.get(port) != "input":
                        connections[port] = ["0"]
                        directions[port] = "input"
                        changed = True
                for port in connections:
                    if port in extra or port in tied or port in directions:
                        continue
                    directions[port] = "output"
                    changed = True
        missing = [
            name
            for name in {**dict(self.synth_json_input_ports), **dict(self.synth_json_tied_low)}
            if found.get(name, 0) == 0
        ]
        if missing:
            raise PolicyError("synth json is missing cells that need extra input ports: " + ", ".join(missing))
        if changed:
            Path(path).write_text(json.dumps(design) + "\n", encoding="utf-8")

    def _attach_m10k_aclr1(self, cells: dict[str, Any]) -> bool:
        """Connect M10K ACLR1 to the selected HPS GPO bit after Yosys omits it."""

        bit = self.m10k_aclr1_gpo_bit
        if bit is None:
            return False
        gp_out = None
        memories: list[dict[str, Any]] = []
        for cell in cells.values():
            if not isinstance(cell, dict):
                continue
            if cell.get("type") == "cyclonev_hps_interface_mpu_general_purpose":
                connections = cell.get("connections")
                if isinstance(connections, dict):
                    bits = connections.get("gp_out")
                    if isinstance(bits, list) and len(bits) > bit:
                        gp_out = bits
            if cell.get("type") == "MISTRAL_M10K":
                memories.append(cell)
        if not memories:
            return False
        if gp_out is None:
            raise PolicyError("synth json has no HPS gp_out for M10K ACLR1")
        net = gp_out[bit]
        if net in (0, 1, "0", "1"):
            raise PolicyError("M10K ACLR1 GPO bit must be a fabric net")
        changed = False
        for cell in memories:
            connections = cell.get("connections")
            if not isinstance(connections, dict):
                raise PolicyError("M10K cell is missing connections")
            directions = cell.get("port_directions")
            if not isinstance(directions, dict):
                directions = {}
                cell["port_directions"] = directions
            if connections.get("ACLR1") != [net] or directions.get("ACLR1") != "input":
                connections["ACLR1"] = [net]
                directions["ACLR1"] = "input"
                changed = True
        return changed

    def _attach_m10k_addrstalla(self, cells: dict[str, Any]) -> bool:
        """Connect TDP ADDRSTALLA to the selected HPS GPO bit after Yosys omits it."""

        bit = self.m10k_addrstalla_gpo_bit
        if bit is None:
            return False
        gp_out = None
        memories: list[dict[str, Any]] = []
        for cell in cells.values():
            if not isinstance(cell, dict):
                continue
            if cell.get("type") == "cyclonev_hps_interface_mpu_general_purpose":
                connections = cell.get("connections")
                if isinstance(connections, dict):
                    bits = connections.get("gp_out")
                    if isinstance(bits, list) and len(bits) > bit:
                        gp_out = bits
            if cell.get("type") == "MISTRAL_M10K_TDP":
                memories.append(cell)
        if not memories:
            return False
        if gp_out is None:
            raise PolicyError("synth json has no HPS gp_out for M10K ADDRSTALLA")
        net = gp_out[bit]
        if net in (0, 1, "0", "1"):
            raise PolicyError("M10K ADDRSTALLA GPO bit must be a fabric net")
        changed = False
        for cell in memories:
            connections = cell.get("connections")
            if not isinstance(connections, dict):
                raise PolicyError("TDP M10K cell is missing connections")
            directions = cell.get("port_directions")
            if not isinstance(directions, dict):
                directions = {}
                cell["port_directions"] = directions
            if connections.get("ADDRSTALLA") != [net] or directions.get("ADDRSTALLA") != "input":
                connections["ADDRSTALLA"] = [net]
                directions["ADDRSTALLA"] = "input"
                changed = True
        return changed

    def _apply_m10k_out_reg_b(self, cells: dict[str, Any]) -> bool:
        """Request the M10K B-port output register after Yosys omits it."""

        if not self.m10k_out_reg_b:
            return False
        changed = False
        found = 0
        for cell in cells.values():
            if not isinstance(cell, dict) or cell.get("type") != "MISTRAL_M10K":
                continue
            found += 1
            parameters = cell.get("parameters")
            if not isinstance(parameters, dict):
                parameters = {}
                cell["parameters"] = parameters
            registered = f"{1:032b}"
            unregistered = f"{0:032b}"
            if parameters.get("CFG_OUT_REG_B") != registered:
                parameters["CFG_OUT_REG_B"] = registered
                changed = True
            if parameters.get("CFG_OUT_REG_A") not in (None, unregistered):
                parameters["CFG_OUT_REG_A"] = unregistered
                changed = True
        return changed

    def _apply_m10k_async_read(self, cells: dict[str, Any]) -> bool:
        """Rewrite Yosys M10K JSON into the combinational-read contract."""

        if not self.m10k_async_read:
            return False
        changed = False
        found = 0
        for cell in cells.values():
            if not isinstance(cell, dict) or cell.get("type") != "MISTRAL_M10K":
                continue
            found += 1
            parameters = cell.get("parameters")
            if not isinstance(parameters, dict):
                parameters = {}
                cell["parameters"] = parameters
            async_value = f"{1:032b}"
            if parameters.get("CFG_ASYNC_READ") != async_value:
                parameters["CFG_ASYNC_READ"] = async_value
                changed = True
            dual = json_bit_parameter(parameters.get("CFG_DUAL_CLOCK"))
            if dual not in (None, 0):
                parameters["CFG_DUAL_CLOCK"] = f"{0:032b}"
                changed = True
            connections = cell.get("connections")
            if not isinstance(connections, dict):
                raise PolicyError("M10K cell is missing connections")
            directions = cell.get("port_directions")
            if not isinstance(directions, dict):
                directions = {}
                cell["port_directions"] = directions
            for port in ("B1EN", "CLK2"):
                if port in connections or port in directions:
                    connections.pop(port, None)
                    directions.pop(port, None)
                    changed = True
            for port in ("ACLR0", "ACLR1"):
                if connections.get(port) != ["0"] or directions.get(port) != "input":
                    connections[port] = ["0"]
                    directions[port] = "input"
                    changed = True
        return changed

    def validate_routed_json(self, path: Path) -> None:
        """Require packed BEL co-location that utilization counts cannot express."""

        if not self.required_packed_sites:
            return
        try:
            design = json.loads(Path(path).read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            raise PolicyError(f"cannot read routed json {path}: {exc}") from exc
        if not isinstance(design, Mapping):
            raise PolicyError("routed json must be an object")
        modules = design.get("modules")
        if not isinstance(modules, Mapping):
            raise PolicyError("routed json has no modules")
        cells_by_type: dict[str, list[str]] = {name: [] for name in self.required_packed_sites}
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
                if not isinstance(cell_type, str) or cell_type not in cells_by_type:
                    continue
                attributes = cell.get("attributes")
                if not isinstance(attributes, Mapping):
                    raise PolicyError(f"routed {cell_type} cell has no attributes")
                bel = attributes.get("NEXTPNR_BEL")
                if not isinstance(bel, str) or not bel:
                    raise PolicyError(f"routed {cell_type} cell is missing NEXTPNR_BEL")
                cells_by_type[cell_type].append(bel)
        for name, expected_sites in self.required_packed_sites.items():
            bels = cells_by_type[name]
            expected_cells = self.required_synth_cells[name]
            if len(bels) != expected_cells:
                raise PolicyError(
                    f"routed {name} must occur exactly {expected_cells} time(s), got {len(bels)}"
                )
            sites: set[str] = set()
            lanes: set[int] = set()
            for bel in bels:
                parts = bel.split(".")
                if len(parts) < 4:
                    raise PolicyError(f"routed {name} BEL {bel!r} is not a site.lane coordinate")
                try:
                    lane = int(parts[-1])
                except ValueError as exc:
                    raise PolicyError(f"routed {name} BEL {bel!r} has a non-integer lane") from exc
                sites.add(".".join(parts[:3]))
                lanes.add(lane)
            if len(sites) != expected_sites:
                raise PolicyError(
                    f"routed {name} must occupy exactly {expected_sites} physical site(s), got {len(sites)}"
                )
            if expected_sites == 1 and lanes != set(range(expected_cells)):
                raise PolicyError(
                    f"routed {name} lanes must be {list(range(expected_cells))}, got {sorted(lanes)}"
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
            **({"required_packed_sites": dict(self.required_packed_sites)}
               if self.required_packed_sites else {}),
            **(
                {
                    "synth_json_input_ports": {
                        cell: list(ports) for cell, ports in self.synth_json_input_ports.items()
                    }
                }
                if self.synth_json_input_ports
                else {}
            ),
            **(
                {
                    "synth_json_tied_low": {
                        cell: list(ports) for cell, ports in self.synth_json_tied_low.items()
                    }
                }
                if self.synth_json_tied_low
                else {}
            ),
            **({"synth_json_mlab_init": True} if self.synth_json_mlab_init else {}),
            **({"m10k_byte_enable": True} if self.m10k_byte_enable else {}),
            **({"m10k_dual_clock_width": self.m10k_dual_clock_width}
               if self.m10k_dual_clock_width else {}),
            **(
                {
                    "m10k_mixed_write_dbits": self.m10k_mixed_write_dbits,
                    "m10k_mixed_read_dbits": self.m10k_mixed_read_dbits,
                }
                if self.m10k_mixed_write_dbits
                else {}
            ),
            **({"m10k_tdp_width": self.m10k_tdp_width} if self.m10k_tdp_width else {}),
            **({"m10k_tdp_byte_width": self.m10k_tdp_byte_width} if self.m10k_tdp_byte_width else {}),
            **(
                {
                    "m10k_tdp_mixed_a": self.m10k_tdp_mixed_a,
                    "m10k_tdp_mixed_b": self.m10k_tdp_mixed_b,
                }
                if self.m10k_tdp_mixed_a
                else {}
            ),
            **({"nextpnr_router": self.nextpnr_router} if self.nextpnr_router else {}),
            **({"require_read_clock_arc": True} if self.require_read_clock_arc else {}),
            **({"m10k_aclr1_gpo_bit": self.m10k_aclr1_gpo_bit}
               if self.m10k_aclr1_gpo_bit is not None else {}),
            **({"m10k_require_aclr1": True} if self.m10k_require_aclr1 else {}),
            **({"m10k_tdp_constant_clk2": True} if self.m10k_tdp_constant_clk2 else {}),
            **({"m10k_async_read": True} if self.m10k_async_read else {}),
            **({"m10k_addrstalla_gpo_bit": self.m10k_addrstalla_gpo_bit}
               if self.m10k_addrstalla_gpo_bit is not None else {}),
            **({"m10k_out_reg_b": True} if self.m10k_out_reg_b else {}),
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
        "110_pll_reset": ExperimentPolicy(
            name="110_pll_reset",
            sources=("experiments/110_pll_reset/rtl/top.v",),
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
                        "experiments/110_pll_reset/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/110_pll_reset/sim/pll_model.v",
                    ),
                    tb="experiments/110_pll_reset/sim/tb.cpp",
                ),
                SimJob(
                    name="meter", top="pll_meter",
                    sources=("experiments/110_pll_reset/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "120_pll_dsp": ExperimentPolicy(
            name="120_pll_dsp",
            sources=("experiments/120_pll_dsp/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("host_port.FPGA_CLK1_50",),
            additional_clocks_mhz={"clk25": 25.0},
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_MUL9X9": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "phase_locked", "DSP", "MAC", "MUL"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
            },
            required_synth_cells={"altera_pll": 1, "MISTRAL_MUL9X9": 1},
            nodsp=False,
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/120_pll_dsp/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/090_pll_clock/sim/pll_model.v",
                    ),
                    tb="experiments/120_pll_dsp/sim/tb.cpp",
                ),
            ),
        ),
        "130_pll_dsp_40": ExperimentPolicy(
            name="130_pll_dsp_40",
            sources=("experiments/130_pll_dsp_40/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("host_port.FPGA_CLK1_50",),
            additional_clocks_mhz={"clk40": 40.0},
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_MUL9X9": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "phase_locked", "DSP", "MAC", "MUL"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
            },
            required_synth_cells={"altera_pll": 1, "MISTRAL_MUL9X9": 1},
            nodsp=False,
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/130_pll_dsp_40/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/130_pll_dsp_40/sim/pll_model.v",
                    ),
                    tb="experiments/130_pll_dsp_40/sim/tb.cpp",
                ),
            ),
        ),
        "140_pll_dsp_20": ExperimentPolicy(
            name="140_pll_dsp_20",
            sources=("experiments/140_pll_dsp_20/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("host_port.FPGA_CLK1_50",),
            additional_clocks_mhz={"clk20": 20.0},
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_MUL9X9": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "phase_locked", "DSP", "MAC", "MUL"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
            },
            required_synth_cells={"altera_pll": 1, "MISTRAL_MUL9X9": 1},
            nodsp=False,
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/140_pll_dsp_20/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/140_pll_dsp_20/sim/pll_model.v",
                    ),
                    tb="experiments/140_pll_dsp_20/sim/tb.cpp",
                ),
            ),
        ),
        "150_pll_dsp_80": ExperimentPolicy(
            name="150_pll_dsp_80",
            sources=("experiments/150_pll_dsp_80/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("host_port.FPGA_CLK1_50",),
            additional_clocks_mhz={"clk80": 80.0},
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_MUL9X9": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "phase_locked", "DSP", "MAC", "MUL"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
            },
            required_synth_cells={"altera_pll": 1, "MISTRAL_MUL9X9": 1},
            nodsp=False,
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/150_pll_dsp_80/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/150_pll_dsp_80/sim/pll_model.v",
                    ),
                    tb="experiments/150_pll_dsp_80/sim/tb.cpp",
                ),
            ),
        ),
        "160_pll_dsp_100": ExperimentPolicy(
            name="160_pll_dsp_100",
            sources=("experiments/160_pll_dsp_100/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("host_port.FPGA_CLK1_50",),
            additional_clocks_mhz={"clk100": 100.0},
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_MUL9X9": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "phase_locked", "DSP", "MAC", "MUL"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
            },
            required_synth_cells={"altera_pll": 1, "MISTRAL_MUL9X9": 1},
            nodsp=False,
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/160_pll_dsp_100/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/160_pll_dsp_100/sim/pll_model.v",
                    ),
                    tb="experiments/160_pll_dsp_100/sim/tb.cpp",
                ),
            ),
        ),
        "170_pll_dual": ExperimentPolicy(
            name="170_pll_dual",
            sources=("experiments/170_pll_dual/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"clocks[0]": 25.0, "clocks[1]": 40.0},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/170_pll_dual/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/170_pll_dual/sim/pll_model.v",
                    ),
                    tb="experiments/170_pll_dual/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/170_pll_dual/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "180_pll_frac": ExperimentPolicy(
            name="180_pll_frac",
            sources=("experiments/180_pll_frac/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"fractional_clock": 12.288},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/180_pll_frac/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/180_pll_frac/sim/pll_model.v",
                    ),
                    tb="experiments/180_pll_frac/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/180_pll_frac/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "190_pll_frac_441": ExperimentPolicy(
            name="190_pll_frac_441",
            sources=("experiments/190_pll_frac_441/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"fractional_clock": 11.2896},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/190_pll_frac_441/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/190_pll_frac_441/sim/pll_model.v",
                    ),
                    tb="experiments/190_pll_frac_441/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/190_pll_frac_441/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "200_pll_frac_dual": ExperimentPolicy(
            name="200_pll_frac_dual",
            sources=("experiments/200_pll_frac_dual/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"clocks[0]": 12.288, "clocks[1]": 24.576},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/200_pll_frac_dual/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/200_pll_frac_dual/sim/pll_model.v",
                    ),
                    tb="experiments/200_pll_frac_dual/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/200_pll_frac_dual/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "210_pll_duty": ExperimentPolicy(
            name="210_pll_duty",
            sources=("experiments/210_pll_duty/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"duty_clock": 25.0},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/210_pll_duty/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/210_pll_duty/sim/pll_model.v",
                    ),
                    tb="experiments/210_pll_duty/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/210_pll_duty/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "220_pll_phase": ExperimentPolicy(
            name="220_pll_phase",
            sources=("experiments/220_pll_phase/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"meter.testclk": 25.0, "phase90": 25.0},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/220_pll_phase/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/220_pll_phase/sim/pll_model.v",
                    ),
                    tb="experiments/220_pll_phase/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/220_pll_phase/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "230_pll_phase_180": ExperimentPolicy(
            name="230_pll_phase_180",
            sources=("experiments/230_pll_phase_180/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"meter.testclk": 25.0, "phase180": 25.0},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/230_pll_phase_180/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/230_pll_phase_180/sim/pll_model.v",
                    ),
                    tb="experiments/230_pll_phase_180/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/230_pll_phase_180/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "240_pll_phase_270": ExperimentPolicy(
            name="240_pll_phase_270",
            sources=("experiments/240_pll_phase_270/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"meter.testclk": 25.0, "phase270": 25.0},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/240_pll_phase_270/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/240_pll_phase_270/sim/pll_model.v",
                    ),
                    tb="experiments/240_pll_phase_270/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/240_pll_phase_270/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "250_pll_triple": ExperimentPolicy(
            name="250_pll_triple",
            sources=("experiments/250_pll_triple/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"clocks[0]": 25.0, "clocks[1]": 50.0, "clocks[2]": 100.0},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/250_pll_triple/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/250_pll_triple/sim/pll_model.v",
                    ),
                    tb="experiments/250_pll_triple/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/250_pll_triple/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "260_pll_quad": ExperimentPolicy(
            name="260_pll_quad",
            sources=("experiments/260_pll_quad/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={
                "clocks[0]": 25.0,
                "clocks[1]": 50.0,
                "clocks[2]": 100.0,
                "clocks[3]": 75.0,
            },
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/260_pll_quad/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/260_pll_quad/sim/pll_model.v",
                    ),
                    tb="experiments/260_pll_quad/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/260_pll_quad/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "270_pll_multi_duty": ExperimentPolicy(
            name="270_pll_multi_duty",
            sources=("experiments/270_pll_multi_duty/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"clocks[0]": 25.0, "clocks[1]": 50.0, "clocks[2]": 100.0},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/270_pll_multi_duty/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/270_pll_multi_duty/sim/pll_model.v",
                    ),
                    tb="experiments/270_pll_multi_duty/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/270_pll_multi_duty/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "280_pll_quadrature": ExperimentPolicy(
            name="280_pll_quadrature",
            sources=("experiments/280_pll_quadrature/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={
                "clocks[0]": 25.0,
                "clocks[1]": 25.0,
                "clocks[2]": 25.0,
                "clocks[3]": 25.0,
            },
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/280_pll_quadrature/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/280_pll_quadrature/sim/pll_model.v",
                    ),
                    tb="experiments/280_pll_quadrature/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/280_pll_quadrature/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "290_pll_phase_select": ExperimentPolicy(
            name="290_pll_phase_select",
            sources=("experiments/290_pll_phase_select/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={
                "clocks[0]": 25.0,
                "clocks[1]": 25.0,
                "clocks[2]": 25.0,
                "clocks[3]": 25.0,
            },
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/290_pll_phase_select/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/290_pll_phase_select/sim/pll_model.v",
                    ),
                    tb="experiments/290_pll_phase_select/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/290_pll_phase_select/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "300_pll_ref25": ExperimentPolicy(
            name="300_pll_ref25",
            sources=("experiments/300_pll_ref25/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=25.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"clocks[0]": 25.0, "clocks[1]": 50.0, "clocks[2]": 100.0},
            constraints=(
                "boards/de10nano/pins.qsf",
                "experiments/300_pll_ref25/clocks.sdc",
            ),
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/300_pll_ref25/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/300_pll_ref25/sim/pll_model.v",
                    ),
                    tb="experiments/300_pll_ref25/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/300_pll_ref25/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "310_pll_ref100": ExperimentPolicy(
            name="310_pll_ref100",
            sources=("experiments/310_pll_ref100/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=100.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"clocks[0]": 25.0, "clocks[1]": 50.0, "clocks[2]": 100.0},
            constraints=(
                "boards/de10nano/pins.qsf",
                "experiments/310_pll_ref100/clocks.sdc",
            ),
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/310_pll_ref100/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/310_pll_ref100/sim/pll_model.v",
                    ),
                    tb="experiments/310_pll_ref100/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/310_pll_ref100/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "320_pll_phase50": ExperimentPolicy(
            name="320_pll_phase50",
            sources=("experiments/320_pll_phase50/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={
                "clocks[0]": 50.0,
                "clocks[1]": 50.0,
                "clocks[2]": 50.0,
                "clocks[3]": 50.0,
            },
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/320_pll_phase50/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/320_pll_phase50/sim/pll_model.v",
                    ),
                    tb="experiments/320_pll_phase50/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/320_pll_phase50/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "330_pll_phase100": ExperimentPolicy(
            name="330_pll_phase100",
            sources=("experiments/330_pll_phase100/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={
                "clocks[0]": 100.0,
                "clocks[1]": 100.0,
                "clocks[2]": 100.0,
                "clocks[3]": 100.0,
            },
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/330_pll_phase100/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/330_pll_phase100/sim/pll_model.v",
                    ),
                    tb="experiments/330_pll_phase100/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/330_pll_phase100/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "340_pll_phase45": ExperimentPolicy(
            name="340_pll_phase45",
            sources=("experiments/340_pll_phase45/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={
                "clocks[0]": 50.0,
                "clocks[1]": 50.0,
                "clocks[2]": 50.0,
                "clocks[3]": 50.0,
            },
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/340_pll_phase45/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/340_pll_phase45/sim/pll_model.v",
                    ),
                    tb="experiments/340_pll_phase45/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/340_pll_phase45/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "350_pll_two": ExperimentPolicy(
            name="350_pll_two",
            sources=("experiments/350_pll_two/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={
                "integer_clock": 25.0,
                "fractional_clock": 12.288,
            },
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 2,
            },
            forbidden_source_patterns=tuple(
                pattern for pattern in _COMMON_SOURCE_PATTERNS
                if pattern not in {"PLL", "phase_locked"}
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 2,
            },
            required_synth_cells={"altera_pll": 2},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/350_pll_two/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/350_pll_two/sim/pll_model.v",
                    ),
                    tb="experiments/350_pll_two/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/350_pll_two/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "360_pll_clkena": ExperimentPolicy(
            name="360_pll_clkena",
            sources=("experiments/360_pll_clkena/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"gated_clock": 25.0},
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
                "cyclonev_clkena": 1,
            },
            required_synth_cells={"altera_pll": 1, "cyclonev_clkena": 1},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/360_pll_clkena/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/360_pll_clkena/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/360_pll_clkena/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "370_pll_clkena_low": ExperimentPolicy(
            name="370_pll_clkena_low",
            sources=("experiments/370_pll_clkena_low/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"gated_clock": 25.0},
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
                "cyclonev_clkena": 1,
            },
            required_synth_cells={"altera_pll": 1, "cyclonev_clkena": 1},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/370_pll_clkena_low/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/370_pll_clkena_low/sim/pll_model.v",
                        "experiments/370_pll_clkena_low/sim/clkena_model.v",
                    ),
                    tb="experiments/370_pll_clkena_low/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/370_pll_clkena_low/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "380_pll_clkena_branch": ExperimentPolicy(
            name="380_pll_clkena_branch",
            sources=("experiments/380_pll_clkena_branch/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"meter.testclk": 25.0, "gated_clock": 25.0},
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
                "cyclonev_clkena": 1,
            },
            required_synth_cells={"altera_pll": 1, "cyclonev_clkena": 1},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/380_pll_clkena_branch/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/380_pll_clkena_branch/sim/pll_model.v",
                        "experiments/380_pll_clkena_branch/sim/clkena_model.v",
                    ),
                    tb="experiments/380_pll_clkena_branch/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/380_pll_clkena_branch/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "390_pll_clkena_status": ExperimentPolicy(
            name="390_pll_clkena_status",
            sources=("experiments/390_pll_clkena_status/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"gated_clock": 25.0},
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
                "cyclonev_clkena": 1,
            },
            required_synth_cells={"altera_pll": 1, "cyclonev_clkena": 1},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/390_pll_clkena_status/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/390_pll_clkena_status/sim/pll_model.v",
                        "experiments/390_pll_clkena_status/sim/clkena_model.v",
                    ),
                    tb="experiments/390_pll_clkena_status/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/390_pll_clkena_status/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "400_pll_clkena_reg2": ExperimentPolicy(
            name="400_pll_clkena_reg2",
            sources=("experiments/400_pll_clkena_reg2/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"gated_clock": 25.0},
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
                "cyclonev_clkena": 1,
            },
            required_synth_cells={"altera_pll": 1, "cyclonev_clkena": 1},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/400_pll_clkena_reg2/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/400_pll_clkena_reg2/sim/pll_model.v",
                        "experiments/400_pll_clkena_reg2/sim/clkena_model.v",
                    ),
                    tb="experiments/400_pll_clkena_reg2/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/400_pll_clkena_reg2/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "410_dsp_triple": ExperimentPolicy(
            name="410_dsp_triple",
            sources=("experiments/410_dsp_triple/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_MUL9X9": 3,
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
            required_synth_cells={"MISTRAL_MUL9X9": 3},
            required_packed_sites={"MISTRAL_MUL9X9": 1},
            nodsp=False,
            yosys_post_synth="setattr -mod -unset keep_hierarchy packed_product; flatten; ",
            clock_evidence_names=("product.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/410_dsp_triple/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/410_dsp_triple/sim/tb.cpp",
                ),
            ),
        ),
        "420_dsp_mul18": ExperimentPolicy(
            name="420_dsp_mul18",
            sources=("experiments/420_dsp_mul18/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_MUL18X18": 1,
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
            required_synth_cells={"MISTRAL_MUL18X18": 1},
            nodsp=False,
            clock_evidence_names=("product.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/420_dsp_mul18/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/420_dsp_mul18/sim/tb.cpp",
                ),
            ),
        ),
        "430_dsp_mul27": ExperimentPolicy(
            name="430_dsp_mul27",
            sources=("experiments/430_dsp_mul27/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_MUL27X27": 1,
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
            required_synth_cells={"MISTRAL_MUL27X27": 1},
            nodsp=False,
            clock_evidence_names=("product.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/430_dsp_mul27/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/430_dsp_mul27/sim/tb.cpp",
                ),
            ),
        ),
        "440_dsp_preadder": ExperimentPolicy(
            name="440_dsp_preadder",
            sources=("experiments/440_dsp_preadder/rtl/top.v",),
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
                "dsp9_preadder": 2,
            },
            required_synth_cells={"MISTRAL_MUL9X9": 1},
            nodsp=False,
            synth_json_input_ports={"MISTRAL_MUL9X9": ("Z",)},
            yosys_post_synth=(
                "chtype -set MISTRAL_MUL9X9 t:dsp9_preadder; "
                "setparam -set PREADDER_EN 1 t:MISTRAL_MUL9X9; "
                "setparam -set PREADDER_SUB 1 t:MISTRAL_MUL9X9; "
                "setparam -set A_SIGNED 0 t:MISTRAL_MUL9X9; "
                "setparam -set B_SIGNED 0 t:MISTRAL_MUL9X9; "
            ),
            clock_evidence_names=("product.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/440_dsp_preadder/rtl/top.v",
                        "experiments/440_dsp_preadder/sim/dsp9_preadder.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/440_dsp_preadder/sim/tb.cpp",
                ),
            ),
        ),
        "450_dsp_mac": ExperimentPolicy(
            name="450_dsp_mac",
            sources=("experiments/450_dsp_mac/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_MUL18X18": 1,
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
                "dsp18_mac": 2,
            },
            required_synth_cells={"MISTRAL_MUL18X18": 1},
            nodsp=False,
            synth_json_input_ports={"MISTRAL_MUL18X18": ("C",)},
            yosys_post_synth=(
                "chtype -set MISTRAL_MUL18X18 t:dsp18_mac; "
                "setparam -set A_SIGNED 0 t:MISTRAL_MUL18X18; "
                "setparam -set B_SIGNED 0 t:MISTRAL_MUL18X18; "
            ),
            clock_evidence_names=("product.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/450_dsp_mac/rtl/top.v",
                        "experiments/450_dsp_mac/sim/dsp18_mac.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/450_dsp_mac/sim/tb.cpp",
                ),
            ),
        ),
        "460_dsp_reg": ExperimentPolicy(
            name="460_dsp_reg",
            sources=("experiments/460_dsp_reg/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_MUL18X18": 1,
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
                "dsp18_reg": 2,
            },
            required_synth_cells={"MISTRAL_MUL18X18": 1},
            nodsp=False,
            synth_json_input_ports={"MISTRAL_MUL18X18": ("CLK",)},
            yosys_post_synth=(
                "chtype -set MISTRAL_MUL18X18 t:dsp18_reg; "
                "setparam -set A_SIGNED 0 t:MISTRAL_MUL18X18; "
                "setparam -set B_SIGNED 0 t:MISTRAL_MUL18X18; "
                "setparam -set INREG_CTRL_AX 1 t:MISTRAL_MUL18X18; "
                "setparam -set INREG_CTRL_AY 1 t:MISTRAL_MUL18X18; "
                "setparam -set OREG_CTRL 1 t:MISTRAL_MUL18X18; "
            ),
            clock_evidence_names=("product.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/460_dsp_reg/rtl/top.v",
                        "experiments/460_dsp_reg/sim/dsp18_reg.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/460_dsp_reg/sim/tb.cpp",
                ),
            ),
        ),
        "470_mlab_init": ExperimentPolicy(
            name="470_mlab_init",
            sources=("experiments/470_mlab_init/rtl/top.v",),
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
            synth_json_mlab_init=True,
            clock_evidence_names=("storage.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/470_mlab_init/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/470_mlab_init/sim/tb.cpp",
                ),
            ),
        ),
        "480_m10k_sdp20": ExperimentPolicy(
            name="480_m10k_sdp20",
            sources=("experiments/480_m10k_sdp20/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_dual_clock_width=20,
            require_read_clock_arc=True,
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/480_m10k_sdp20/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/480_m10k_sdp20/sim/tb.cpp",
                ),
            ),
        ),
        "490_m10k_sdp40": ExperimentPolicy(
            name="490_m10k_sdp40",
            sources=("experiments/490_m10k_sdp40/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_dual_clock_width=40,
            require_read_clock_arc=True,
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/490_m10k_sdp40/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/490_m10k_sdp40/sim/tb.cpp",
                ),
            ),
        ),
        "500_m10k_be20": ExperimentPolicy(
            name="500_m10k_be20",
            sources=("experiments/500_m10k_be20/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_byte_enable=True,
            require_read_clock_arc=True,
            synth_json_input_ports={"MISTRAL_M10K": ("CLK1", "CLK2", "A1EN", "A1BE")},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/500_m10k_be20/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/500_m10k_be20/sim/tb.cpp",
                ),
            ),
        ),
        "510_m10k_mix40r10": ExperimentPolicy(
            name="510_m10k_mix40r10",
            sources=("experiments/510_m10k_mix40r10/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K", "RAM"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_mixed_write_dbits=40,
            m10k_mixed_read_dbits=10,
            nextpnr_router="router1",
            require_read_clock_arc=True,
            synth_json_input_ports={"MISTRAL_M10K": ("CLK1", "CLK2", "A1EN", "B1EN")},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/510_m10k_mix40r10/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/510_m10k_mix40r10/sim/tb.cpp",
                ),
            ),
        ),
        "520_m10k_mix10r40": ExperimentPolicy(
            name="520_m10k_mix10r40",
            sources=("experiments/520_m10k_mix10r40/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K", "RAM"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_mixed_write_dbits=10,
            m10k_mixed_read_dbits=40,
            nextpnr_router="router1",
            require_read_clock_arc=True,
            synth_json_input_ports={"MISTRAL_M10K": ("CLK1", "CLK2", "A1EN", "B1EN")},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/520_m10k_mix10r40/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/520_m10k_mix10r40/sim/tb.cpp",
                ),
            ),
        ),
        "530_m10k_tdp10": ExperimentPolicy(
            name="530_m10k_tdp10",
            sources=("experiments/530_m10k_tdp10/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K", "RAM"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K_TDP": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_tdp_width=10,
            require_read_clock_arc=True,
            synth_json_input_ports={
                "MISTRAL_M10K_TDP": ("CLK1", "CLK2", "A1EN", "B1EN", "A1WE", "B1WE")
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/530_m10k_tdp10/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/530_m10k_tdp10/sim/tb.cpp",
                ),
            ),
        ),
        "540_m10k_tdp20": ExperimentPolicy(
            name="540_m10k_tdp20",
            sources=("experiments/540_m10k_tdp20/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K", "RAM"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K_TDP": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_tdp_width=20,
            require_read_clock_arc=True,
            synth_json_input_ports={
                "MISTRAL_M10K_TDP": ("CLK1", "CLK2", "A1EN", "B1EN", "A1WE", "B1WE")
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/540_m10k_tdp20/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/540_m10k_tdp20/sim/tb.cpp",
                ),
            ),
        ),
        "550_m10k_tdp_be20": ExperimentPolicy(
            name="550_m10k_tdp_be20",
            sources=("experiments/550_m10k_tdp_be20/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K", "RAM"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K_TDP": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_tdp_byte_width=20,
            require_read_clock_arc=True,
            synth_json_input_ports={
                "MISTRAL_M10K_TDP": (
                    "CLK1",
                    "CLK2",
                    "A1EN",
                    "B1EN",
                    "A1WE",
                    "B1WE",
                    "A1BE",
                    "B1BE",
                )
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/550_m10k_tdp_be20/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/550_m10k_tdp_be20/sim/tb.cpp",
                ),
            ),
        ),
        "560_m10k_tdp_be16": ExperimentPolicy(
            name="560_m10k_tdp_be16",
            sources=("experiments/560_m10k_tdp_be16/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K", "RAM"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K_TDP": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_tdp_byte_width=16,
            require_read_clock_arc=True,
            synth_json_input_ports={
                "MISTRAL_M10K_TDP": (
                    "CLK1",
                    "CLK2",
                    "A1EN",
                    "B1EN",
                    "A1WE",
                    "B1WE",
                    "A1BE",
                    "B1BE",
                )
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/560_m10k_tdp_be16/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/560_m10k_tdp_be16/sim/tb.cpp",
                ),
            ),
        ),
        "570_m10k_tdp_mix20_10": ExperimentPolicy(
            name="570_m10k_tdp_mix20_10",
            sources=("experiments/570_m10k_tdp_mix20_10/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K", "RAM"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K_TDP": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_tdp_mixed_a=20,
            m10k_tdp_mixed_b=10,
            require_read_clock_arc=True,
            synth_json_input_ports={
                "MISTRAL_M10K_TDP": ("CLK1", "CLK2", "A1EN", "B1EN", "A1WE", "B1WE")
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/570_m10k_tdp_mix20_10/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/570_m10k_tdp_mix20_10/sim/tb.cpp",
                ),
            ),
        ),
        "580_m10k_tdp_mix10_20": ExperimentPolicy(
            name="580_m10k_tdp_mix10_20",
            sources=("experiments/580_m10k_tdp_mix10_20/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K", "RAM"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K_TDP": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_tdp_mixed_a=10,
            m10k_tdp_mixed_b=20,
            require_read_clock_arc=True,
            synth_json_input_ports={
                "MISTRAL_M10K_TDP": ("CLK1", "CLK2", "A1EN", "B1EN", "A1WE", "B1WE")
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/580_m10k_tdp_mix10_20/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/580_m10k_tdp_mix10_20/sim/tb.cpp",
                ),
            ),
        ),
        "590_m10k_tdp_mix16_8": ExperimentPolicy(
            name="590_m10k_tdp_mix16_8",
            sources=("experiments/590_m10k_tdp_mix16_8/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K", "RAM"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K_TDP": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_tdp_mixed_a=16,
            m10k_tdp_mixed_b=8,
            require_read_clock_arc=True,
            synth_json_input_ports={
                "MISTRAL_M10K_TDP": ("CLK1", "CLK2", "A1EN", "B1EN", "A1WE", "B1WE")
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/590_m10k_tdp_mix16_8/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/590_m10k_tdp_mix16_8/sim/tb.cpp",
                ),
            ),
        ),
        "600_m10k_tdp_mix8_16": ExperimentPolicy(
            name="600_m10k_tdp_mix8_16",
            sources=("experiments/600_m10k_tdp_mix8_16/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K", "RAM"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K_TDP": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_tdp_mixed_a=8,
            m10k_tdp_mixed_b=16,
            require_read_clock_arc=True,
            synth_json_input_ports={
                "MISTRAL_M10K_TDP": ("CLK1", "CLK2", "A1EN", "B1EN", "A1WE", "B1WE")
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/600_m10k_tdp_mix8_16/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/600_m10k_tdp_mix8_16/sim/tb.cpp",
                ),
            ),
        ),
        "610_pll_frac_7425": ExperimentPolicy(
            name="610_pll_frac_7425",
            sources=("experiments/610_pll_frac_7425/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"fractional_clock": 74.25},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/610_pll_frac_7425/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/610_pll_frac_7425/sim/pll_model.v",
                    ),
                    tb="experiments/610_pll_frac_7425/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/610_pll_frac_7425/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "620_ddr_clock": ExperimentPolicy(
            name="620_ddr_clock",
            sources=("experiments/620_ddr_clock/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            constraints=(
                "experiments/620_ddr_clock/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            forbidden_source_patterns=(*_COMMON_SOURCE_PATTERNS, "LED", "GPIO", "external_gpio"),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altddio_out": 1,
            },
            required_synth_cells={"altddio_out": 1},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/620_ddr_clock/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/620_ddr_clock/sim/ddr_model.v",
                    ),
                    tb="experiments/620_ddr_clock/sim/tb.cpp",
                ),
            ),
        ),
        "630_sdr_output": ExperimentPolicy(
            name="630_sdr_output",
            sources=("experiments/630_sdr_output/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            constraints=(
                "experiments/630_sdr_output/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            forbidden_source_patterns=(*_COMMON_SOURCE_PATTERNS, "LED", "GPIO", "external_gpio"),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/630_sdr_output/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/630_sdr_output/sim/tb.cpp",
                ),
            ),
        ),
        "640_sdr_input": ExperimentPolicy(
            name="640_sdr_input",
            sources=("experiments/640_sdr_input/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            constraints=(
                "experiments/640_sdr_input/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            forbidden_source_patterns=(*_COMMON_SOURCE_PATTERNS, "LED", "GPIO", "external_gpio"),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/640_sdr_input/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/640_sdr_input/sim/tb.cpp",
                ),
            ),
        ),
        "650_ddr_input": ExperimentPolicy(
            name="650_ddr_input",
            sources=("experiments/650_ddr_input/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            constraints=(
                "experiments/650_ddr_input/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            forbidden_source_patterns=(*_COMMON_SOURCE_PATTERNS, "LED", "GPIO", "external_gpio"),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altddio_in": 1,
            },
            required_synth_cells={"altddio_in": 1},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/650_ddr_input/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/650_ddr_input/sim/altddio_in_model.v",
                    ),
                    tb="experiments/650_ddr_input/sim/tb.cpp",
                ),
            ),
        ),
        "660_ddr_data": ExperimentPolicy(
            name="660_ddr_data",
            sources=("experiments/660_ddr_data/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            constraints=(
                "experiments/660_ddr_data/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            forbidden_source_patterns=(*_COMMON_SOURCE_PATTERNS, "LED", "GPIO", "external_gpio"),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altddio_out": 1,
            },
            required_synth_cells={"altddio_out": 1},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/660_ddr_data/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/660_ddr_data/sim/altddio_out_model.v",
                    ),
                    tb="experiments/660_ddr_data/sim/tb.cpp",
                ),
            ),
        ),
        "670_altiobuf": ExperimentPolicy(
            name="670_altiobuf",
            sources=("experiments/670_altiobuf/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            constraints=(
                "experiments/670_altiobuf/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            forbidden_source_patterns=(*_COMMON_SOURCE_PATTERNS, "LED", "GPIO", "external_gpio"),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altiobuf_in": 1,
                "altiobuf_out": 1,
                "altiobuf_bidir": 1,
            },
            required_synth_cells={"altiobuf_in": 1, "altiobuf_out": 1, "altiobuf_bidir": 1},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/670_altiobuf/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/670_altiobuf/sim/altiobuf_model.v",
                    ),
                    tb="experiments/670_altiobuf/sim/tb.cpp",
                ),
            ),
        ),
        "680_m10k_mix20be10": ExperimentPolicy(
            name="680_m10k_mix20be10",
            sources=("experiments/680_m10k_mix20be10/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
                "MISTRAL_M10K": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_mixed_write_dbits=20,
            m10k_mixed_read_dbits=10,
            m10k_byte_enable=True,
            nextpnr_router="router1",
            require_read_clock_arc=True,
            synth_json_input_ports={"MISTRAL_M10K": ("CLK1", "CLK2", "A1EN", "B1EN", "A1BE")},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/680_m10k_mix20be10/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                        "experiments/680_m10k_mix20be10/sim/m10k_mixbe_model.v",
                    ),
                    tb="experiments/680_m10k_mix20be10/sim/tb.cpp",
                ),
            ),
        ),
        "690_ddr_bidir": ExperimentPolicy(
            name="690_ddr_bidir",
            sources=("experiments/690_ddr_bidir/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            constraints=(
                "experiments/690_ddr_bidir/pins.qsf",
                "boards/de10nano/clocks.sdc",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
            },
            forbidden_source_patterns=(*_COMMON_SOURCE_PATTERNS, "LED", "GPIO", "external_gpio"),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altddio_bidir": 1,
            },
            required_synth_cells={"altddio_bidir": 1},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/690_ddr_bidir/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/690_ddr_bidir/sim/altddio_bidir_model.v",
                    ),
                    tb="experiments/690_ddr_bidir/sim/tb.cpp",
                ),
            ),
        ),
        "700_m10k_aclr": ExperimentPolicy(
            name="700_m10k_aclr",
            sources=("experiments/700_m10k_aclr/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_dual_clock_width=20,
            m10k_aclr1_gpo_bit=5,
            require_read_clock_arc=True,
            synth_json_input_ports={"MISTRAL_M10K": ("CLK1", "CLK2", "A1EN", "ACLR1")},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/700_m10k_aclr/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/700_m10k_aclr/sim/tb.cpp",
                ),
            ),
        ),
        "710_m10k_aclr_prim": ExperimentPolicy(
            name="710_m10k_aclr_prim",
            sources=("experiments/710_m10k_aclr_prim/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
                "MISTRAL_M10K": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_dual_clock_width=20,
            m10k_require_aclr1=True,
            require_read_clock_arc=True,
            synth_json_input_ports={"MISTRAL_M10K": ("CLK1", "CLK2", "A1EN", "ACLR1")},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/710_m10k_aclr_prim/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                        "experiments/710_m10k_aclr_prim/sim/m10k_aclr_model.v",
                    ),
                    tb="experiments/710_m10k_aclr_prim/sim/tb.cpp",
                ),
            ),
        ),
        "720_m10k_aclr_infer": ExperimentPolicy(
            name="720_m10k_aclr_infer",
            sources=("experiments/720_m10k_aclr_infer/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K": 1,
                "altera_pll": 1,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_dual_clock_width=20,
            m10k_require_aclr1=True,
            require_read_clock_arc=True,
            synth_json_input_ports={"MISTRAL_M10K": ("CLK1", "CLK2", "A1EN", "ACLR1")},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/720_m10k_aclr_infer/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/720_m10k_aclr_infer/sim/tb.cpp",
                ),
            ),
        ),
        "730_m10k_tdp_tclk": ExperimentPolicy(
            name="730_m10k_tdp_tclk",
            sources=("experiments/730_m10k_tdp_tclk/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K_TDP": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K_TDP": 1,
            },
            nobram=False,
            m10k_tdp_width=20,
            m10k_tdp_constant_clk2=True,
            require_read_clock_arc=False,
            synth_json_input_ports={
                "MISTRAL_M10K_TDP": ("CLK1", "CLK2", "A1EN", "B1EN", "A1WE", "B1WE")
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/730_m10k_tdp_tclk/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/730_m10k_tdp_tclk/sim/m10k_tdp_tclk_model.v",
                    ),
                    tb="experiments/730_m10k_tdp_tclk/sim/tb.cpp",
                ),
            ),
        ),
        "740_m10k_dual_pll": ExperimentPolicy(
            name="740_m10k_dual_pll",
            sources=("experiments/740_m10k_dual_pll/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            additional_clocks_mhz={"pixel_clock": 74.25},
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 2,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"PLL", "M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "altera_pll": 2,
                "cyclonev_clkena": 1,
            },
            required_synth_cells={
                "MISTRAL_M10K": 1,
                "altera_pll": 2,
                "cyclonev_clkena": 1,
            },
            nobram=False,
            m10k_dual_clock_width=20,
            require_read_clock_arc=True,
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/740_m10k_dual_pll/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/360_pll_clkena/sim/pll_model.v",
                        "experiments/360_pll_clkena/sim/clkena_model.v",
                    ),
                    tb="experiments/740_m10k_dual_pll/sim/tb.cpp",
                ),
            ),
        ),
        "750_dsp18x19": ExperimentPolicy(
            name="750_dsp18x19",
            sources=("experiments/750_dsp18x19/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_MUL18X19": 1,
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
                "dsp18x19": 2,
            },
            required_synth_cells={"MISTRAL_MUL18X19": 1},
            nodsp=False,
            synth_json_input_ports={"MISTRAL_MUL18X19": ("A", "B", "C", "D")},
            yosys_post_synth=(
                "chtype -set MISTRAL_MUL18X19 t:dsp18x19; "
                "setparam -set A_SIGNED 0 t:MISTRAL_MUL18X19; "
                "setparam -set B_SIGNED 0 t:MISTRAL_MUL18X19; "
                "setparam -set C_SIGNED 0 t:MISTRAL_MUL18X19; "
                "setparam -set D_SIGNED 0 t:MISTRAL_MUL18X19; "
            ),
            clock_evidence_names=("product.FPGA_CLK1_50",),
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/750_dsp18x19/rtl/top.v",
                        "experiments/750_dsp18x19/sim/dsp18x19.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/750_dsp18x19/sim/tb.cpp",
                ),
            ),
        ),
        "760_pll_52": ExperimentPolicy(
            name="760_pll_52",
            sources=("experiments/760_pll_52/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"clk52": 52.0},
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/760_pll_52/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/760_pll_52/sim/pll_model.v",
                    ),
                    tb="experiments/760_pll_52/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/760_pll_52/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "770_m10k_async_read": ExperimentPolicy(
            name="770_m10k_async_read",
            sources=("experiments/770_m10k_async_read/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K": 1,
            },
            required_synth_cells={"MISTRAL_M10K": 1},
            nobram=False,
            m10k_async_read=True,
            require_read_clock_arc=False,
            synth_json_input_ports={"MISTRAL_M10K": ("CLK1", "A1EN", "A1BE")},
            synth_json_tied_low={"MISTRAL_M10K": ("ACLR0", "ACLR1")},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/770_m10k_async_read/rtl/top.v",
                        "experiments/770_m10k_async_read/sim/m10k_async_model.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                    ),
                    tb="experiments/770_m10k_async_read/sim/tb.cpp",
                ),
            ),
        ),
        "780_quartus_sdc": ExperimentPolicy(
            name="780_quartus_sdc",
            sources=("experiments/780_quartus_sdc/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=("meter.refclk",),
            additional_clocks_mhz={"clk25": 25.0},
            constraints=(
                "experiments/780_quartus_sdc/pins.qsf",
                "experiments/780_quartus_sdc/clocks.sdc",
            ),
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
                    name="main",
                    top="top",
                    sources=(
                        "experiments/780_quartus_sdc/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/090_pll_clock/sim/pll_model.v",
                    ),
                    tb="experiments/780_quartus_sdc/sim/tb.cpp",
                ),
                SimJob(
                    name="meter",
                    top="pll_meter",
                    sources=("experiments/780_quartus_sdc/rtl/top.v",),
                    tb="experiments/090_pll_clock/sim/meter_sim.cpp",
                    parameters={"WINDOW_BITS": "12"},
                ),
            ),
        ),
        "790_m10k_addrstall": ExperimentPolicy(
            name="790_m10k_addrstall",
            sources=("experiments/790_m10k_addrstall/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K_TDP": 1,
            },
            required_synth_cells={"MISTRAL_M10K_TDP": 1},
            nobram=False,
            m10k_addrstalla_gpo_bit=29,
            require_read_clock_arc=False,
            synth_json_input_ports={
                "MISTRAL_M10K_TDP": (
                    "CLK1",
                    "CLK2",
                    "A1EN",
                    "B1EN",
                    "A1WE",
                    "B1WE",
                    "ADDRSTALLA",
                )
            },
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/790_m10k_addrstall/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/790_m10k_addrstall/sim/m10k_addrstall_model.v",
                    ),
                    tb="experiments/790_m10k_addrstall/sim/tb.cpp",
                ),
            ),
        ),
        "800_m10k_out_reg": ExperimentPolicy(
            name="800_m10k_out_reg",
            sources=("experiments/800_m10k_out_reg/rtl/top.v",),
            top="top",
            clock="FPGA_CLK1_50",
            clock_mhz=50.0,
            clock_evidence_names=(
                "FPGA_CLK1_50_MISTRAL",
                "FPGA_CLK1_50_MISTRAL_IB_PAD_O_MISTRAL_CLKBUF_A_Q",
            ),
            allowed_hard_blocks={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K": 1,
            },
            forbidden_source_patterns=(
                *(
                    pattern
                    for pattern in _COMMON_SOURCE_PATTERNS
                    if pattern not in {"M10K"}
                ),
                "LED",
                "GPIO",
                "external_gpio",
            ),
            forbidden_resource_patterns=_COMMON_RESOURCE_PATTERNS,
            required_source_identifiers={
                "cyclonev_hps_interface_mpu_general_purpose": 1,
                "MISTRAL_M10K": 1,
            },
            required_synth_cells={"MISTRAL_M10K": 1},
            nobram=False,
            m10k_out_reg_b=True,
            require_read_clock_arc=False,
            synth_json_input_ports={
                "MISTRAL_M10K": (
                    "CLK1",
                    "CLK2",
                    "A1EN",
                    "A1BE",
                    "B1EN",
                )
            },
            synth_json_tied_low={"MISTRAL_M10K": ("ACLR0", "ACLR1")},
            sim_jobs=(
                SimJob(
                    name="main",
                    top="top",
                    sources=(
                        "experiments/800_m10k_out_reg/rtl/top.v",
                        "experiments/020_linux_mailbox/sim/hps_gp_model.v",
                        "experiments/800_m10k_out_reg/sim/m10k_out_reg_model.v",
                    ),
                    tb="experiments/800_m10k_out_reg/sim/tb.cpp",
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
        f"nextpnr_router={policy.nextpnr_router}",
        "allowed_hard_blocks=" + json.dumps(dict(policy.allowed_hard_blocks), sort_keys=True, separators=(",", ":")),
    ]
    return "\n".join(lines) + "\n"


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--experiment", required=True)
    parser.add_argument("--format", choices=("json", "shell"), default="json")
    parser.add_argument("--check-sources", action="store_true")
    parser.add_argument("--check-synth-json", type=Path)
    parser.add_argument("--fix-synth-json", type=Path)
    parser.add_argument("--check-routed-json", type=Path)
    parser.add_argument("--repo-root", type=Path, default=_repo_root())
    return parser


def main(argv: Sequence[str] | None = None) -> int:
    try:
        arguments = _parser().parse_args(argv)
        policy = policy_for(arguments.experiment)
        if arguments.check_sources:
            _check_sources(policy, arguments.repo_root)
        if arguments.fix_synth_json is not None:
            policy.apply_synth_json(arguments.fix_synth_json)
        if arguments.check_synth_json is not None:
            policy.validate_synth_json(arguments.check_synth_json)
        if arguments.check_routed_json is not None:
            policy.validate_routed_json(arguments.check_routed_json)
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
