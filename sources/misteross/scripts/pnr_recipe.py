"""The FES recipe registry's single nextpnr argv and environment builder."""
from __future__ import annotations

import hashlib
import json
import math
import os
import re
import subprocess
import tomllib
from functools import lru_cache
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
from types import MappingProxyType
from typing import Mapping, Sequence
from scripts.fes_build_common import nextpnr_gpu_command


FES_ROOT = Path(__file__).resolve().parents[3]
REGISTRY = FES_ROOT / "config/core-recipes.toml"

# nextpnr's own defaults for the 96-option CLI set. The
# Mistral/FES fork switches are fes_*, mistral_*, replicate_enables, remap_*
# and balance_reduction_root; the rest are result-affecting kernel options.
_DEFAULTS_96_OPTIONS = {
    "randomize_seed": False, "placer": "heap", "cstrweight": "unset",
    "starttemp": "unset", "pack_only": False, "no_route": False,
    "no_place": False, "no_pack": False, "ignore_loops": False,
    "ignore_rel_clk": False, "no_tmdriv": False, "sdf_cvc": False,
    "no_print_critical_path_source": False, "placer_heap_alpha": 0.1,
    "placer_heap_beta": 0.9, "placer_heap_cell_placement_timeout": 8,
    "placer_heap_no_ctrl_set": False, "static_dump_density": False,
    "parallel_refine": False, "router2_heatmap": "unset", "tmg_ripup": False,
    "router2_tmg_ripup": False, "router2_alt_weights": False,
    "gpu_batches": "unset", "gpu_cpu": False, "gpu_perf": False,
    "gpu_telemetry": "unset", "gpu_opt": [], "timing_report_paths": 1,
    "seed_racing_contract": False, "fes_scaffold": False, "fes_cart": "unset",
    "fes_cart_region": "unset", "fes_slot_clock": "unset", "fes_cram_region": "unset",
    "mistral_ff4": False, "mistral_clkb": False, "replicate_enables": 0,
    "remap_critical": "unset", "remap_optimize_pins": False,
    "remap_preserve_ffs": False, "remap_plan": "unset", "remap_post_plan": "unset",
    "remap_comb_critical": "unset", "remap_comb_candidate": "unset",
    "remap_comb_plan": "unset", "remap_decompose_critical": "unset",
    "remap_decompose_candidate": "unset", "remap_lut_pair_critical": "unset",
    "remap_lut_pair_candidate": "unset", "remap_lut_pair_compose_copy": False,
    "remap_lut_driver_critical": "unset", "remap_lut_driver_candidate": "unset",
    "remap_candidate": "unset", "remap_groups": 1, "balance_reduction_root": [],
}

# The nine options in the 105-option set, as declared in its source files.
# sources: common/kernel/command.cc and mistral/main.cc. The Mistral declarations
# say both placement flags and signed-DSP packing are opt-in; cohort budget 0
# disables setup-path repair, its report is an input, and model-out is an output
# for an unrepaired calibration. `set` is a repeatable key=value override,
# restricted below to settings that do not have dedicated recipe inputs.
_ADDED_105_OPTIONS = {
    "arrival_pin_assignment": False, "isolated_lut_placement": False,
    "critical_cohort_budget": 0, "critical_cohort_report": "unset",
    "mistral_dsp_signed_lanes": False,
    "set": [],
    # The source says this fails if --router gpu cannot use a GPU device.
    "gpu_required": False,
    "critical_cohort_model_out": "unset", "router2_profile": "unset",
}

# nextpnr main bec82e36 adds two explicit opt-ins. The phase-related harmonic
# clock analysis is disabled unless requested, and Mistral's FF reset recovery
# reference is likewise opt-in; neither changes a producer's default argv.
_ADDED_107_OPTIONS = {
    "phase_related_harmonic_clocks": False,
    "mistral_ff_reset_reference": False,
}
_ADDED_OPTIONS = {**_ADDED_105_OPTIONS, **_ADDED_107_OPTIONS}

# A nextpnr source option set selects one of these recipe-flag schemas. The
# option names not present in either flag table are still included in the exact
# CLI fingerprint; producer-owned inputs and deliberately unused CLI options
# remain closed by the recipe and argv builders.
_SCHEMA_96_OPTIONS = MappingProxyType(_DEFAULTS_96_OPTIONS)
_SCHEMA_105_OPTIONS = MappingProxyType({**_DEFAULTS_96_OPTIONS, **_ADDED_105_OPTIONS})
_SCHEMA_107_OPTIONS = MappingProxyType({**_SCHEMA_105_OPTIONS, **_ADDED_107_OPTIONS})
_OTHER_SOURCE_OPTIONS = frozenset({
    "Werror", "compress_rbf", "debug", "debug_placer", "debug_router",
    "detailed_timing_report", "device", "force", "freq", "gpu_device", "gui",
    "gui_no_aa", "help", "json", "log", "on_failure", "placed_svg",
    "placer_heap_critexp", "placer_heap_timingweight", "post_route", "pre_pack",
    "pre_place", "pre_route", "qsf", "quiet", "rbf", "report", "routed_svg",
    "router", "run", "sdc", "sdf", "seed", "test", "threads",
    "timing_allow_fail", "top", "verbose", "version", "write",
})
_ABSENT_BY_RECIPE_OPTIONS = frozenset({
    "gui", "gui_no_aa", "on_failure", "post_route", "pre_pack", "pre_place",
    "pre_route", "run",
})
_SOURCE_OPTION_SET_SCHEMAS: Mapping[frozenset[str], Mapping[str, object]] = MappingProxyType({
    frozenset(_SCHEMA_96_OPTIONS) | _OTHER_SOURCE_OPTIONS: _SCHEMA_96_OPTIONS,
    frozenset(_SCHEMA_105_OPTIONS) | _OTHER_SOURCE_OPTIONS: _SCHEMA_105_OPTIONS,
    frozenset(_SCHEMA_107_OPTIONS) | _OTHER_SOURCE_OPTIONS: _SCHEMA_107_OPTIONS,
})
_RUNTIME_SCHEMAS: dict[
    tuple[str, str, str, tuple[str, int, int, int, int] | None], Mapping[str, object]
] = {}
_ACTIVE_RUNTIME_SCHEMA_IDENTITIES: dict[
    tuple[str, str, str], tuple[str, int, int, int, int] | None
] = {}
_HELP_OPTION = re.compile(r"^\s*(?:-[A-Za-z0-9]\s+\[\s*)?--([A-Za-z][A-Za-z0-9_-]*)\b")

RECIPE_FIELDS = frozenset({
    "seeds", "timing_weights", "search_weights", "critexp", "clocks",
    "freq", "qsf", "sdc", "router", "backend", "gpu_device",
    "timeout_s", "threads", "timing_allow_fail", "argv_style", "extra",
    "env", "optional_env", "unset_env", "compress_rbf",
    "detailed_timing_report", "flags",
})

FLAG_BOOLEAN_FIELDS = frozenset({
    "randomize_seed", "pack_only", "no_route", "no_place", "no_pack",
    "ignore_loops", "ignore_rel_clk", "no_tmdriv", "sdf_cvc",
    "no_print_critical_path_source", "placer_heap_no_ctrl_set",
    "static_dump_density", "parallel_refine", "tmg_ripup",
    "router2_tmg_ripup", "router2_alt_weights", "gpu_cpu", "gpu_perf",
    "seed_racing_contract", "fes_scaffold", "mistral_ff4", "mistral_clkb",
    "remap_optimize_pins", "remap_preserve_ffs",
    "remap_lut_pair_compose_copy", "arrival_pin_assignment",
    "isolated_lut_placement", "mistral_dsp_signed_lanes", "gpu_required",
    "phase_related_harmonic_clocks", "mistral_ff_reset_reference",
})
FLAG_INTEGER_FIELDS = frozenset({
    "placer_heap_cell_placement_timeout", "timing_report_paths",
    "replicate_enables", "remap_groups", "critical_cohort_budget",
})
# nextpnr rejects values above these limits after loading the design.
_FLAG_INTEGER_LIMITS = {"critical_cohort_budget": 64}
FLAG_INTEGER_OR_UNSET_FIELDS = frozenset({
    "gpu_batches", "remap_comb_candidate", "remap_decompose_candidate",
    "remap_lut_pair_candidate", "remap_lut_driver_candidate",
    "remap_candidate",
})
FLAG_NUMBER_FIELDS = frozenset({
    "cstrweight", "starttemp", "placer_heap_alpha", "placer_heap_beta",
})
FLAG_STRING_FIELDS = frozenset({
    "placer", "router2_heatmap", "gpu_telemetry", "fes_cart",
    "fes_cart_region", "fes_slot_clock", "fes_cram_region",
    "remap_critical", "remap_plan", "remap_post_plan",
    "remap_comb_critical", "remap_comb_plan", "remap_decompose_critical",
    "remap_lut_pair_critical", "remap_lut_driver_critical",
    "critical_cohort_report", "critical_cohort_model_out", "router2_profile",
})
FLAG_LIST_FIELDS = frozenset({"gpu_opt", "balance_reduction_root", "set"})

# --set may tune only placer and router settings that no dedicated option or
# recipe field owns. A dedicated option the builder omits at its default
# would not conflict inside nextpnr, so --set could silently replace its
# recorded value. Constraints (sdc/*, timing/*, target_freq), the seed, the
# device and the FES boundary settings stay with their own recipe inputs.
_SETTABLE_PREFIXES = ("placerHeap/", "placer1/", "router/", "router1/", "router2/", "gpurouter/")
_DEDICATED_SETTINGS = frozenset({
    "placer1/constraintWeight", "placer1/startTemp", "placerHeap/alpha",
    "placerHeap/beta", "placerHeap/criticalityExponent",
    "placerHeap/timingWeight", "placerHeap/cellPlacementTimeout",
    "placerHeap/noCtrlSet", "placerHeap/parallelRefine", "router/tmg_ripup",
    "router2/heatmap", "router2/profilePath", "router2/alt-weights",
    "gpurouter/device", "gpurouter/cpu", "gpurouter/required",
    "gpurouter/perfProfile", "gpurouter/maxBatches",
    "gpurouter/telemetryPath", "gpurouter/telemetrySeed",
})
_SETTING_KEY = re.compile(r"[A-Za-z0-9_][A-Za-z0-9_./-]*\Z")

# gpu-run's environment is normally removed by the functional build boundary.
# Its small, explicit marker set is admitted only after the complete lease
# description and the recipe's vendor mask agree.
_GPU_RUN_MARKERS = frozenset({
    "FES_GPU_IDS", "GPU_RUN_DEVICE", "GPU_RUN_EXPECTED_DEVICE",
    "NEXTPNR_REQUIRE_GPU",
})
_GPU_VISIBILITY_MASKS = frozenset({
    "HIP_VISIBLE_DEVICES", "ROCR_VISIBLE_DEVICES", "CUDA_VISIBLE_DEVICES",
})
_GPU_ID = re.compile(r"[A-Za-z0-9._+-]+\Z")
_AMD_RENDER_NODE = re.compile(r"/dev/dri/renderD[0-9]+\Z")


def _valid_flag_value(name: str, value: object) -> bool:
    if name in FLAG_BOOLEAN_FIELDS:
        return type(value) is bool
    if name in FLAG_INTEGER_FIELDS:
        return type(value) is int and 0 <= value <= _FLAG_INTEGER_LIMITS.get(name, value)
    if name in FLAG_INTEGER_OR_UNSET_FIELDS:
        return value == "unset" or (type(value) is int and value >= 0)
    if name in FLAG_NUMBER_FIELDS:
        return value == "unset" or (
            type(value) in (int, float) and math.isfinite(float(value))
        )
    if name in FLAG_STRING_FIELDS:
        return isinstance(value, str) and bool(value) and not any(
            ord(char) < 32 or ord(char) == 127 for char in value
        )
    if name in FLAG_LIST_FIELDS:
        return isinstance(value, list) and all(
            isinstance(item, str) and bool(item)
            and not any(ord(char) < 32 or ord(char) == 127 for char in item)
            for item in value
        )
    return False


def _valid_settings(flags: Mapping[str, object]) -> bool:
    """Accept only unique key=value settings that no other recipe input owns."""
    keys = []
    for item in flags.get("set", ()):
        key, separator, value = item.partition("=")
        keys.append(key)
        if (not separator or not value or _SETTING_KEY.fullmatch(key) is None
                or not key.startswith(_SETTABLE_PREFIXES) or key in _DEDICATED_SETTINGS):
            return False
    gpu_opt_keys = {"gpurouter/" + item.partition("=")[0] for item in flags.get("gpu_opt", ())}
    return len(keys) == len(set(keys)) and not set(keys) & gpu_opt_keys


def _valid_cohort_options(flags: Mapping[str, object]) -> bool:
    """Reject cohort combinations nextpnr refuses only after loading the design."""
    # A cohort report guides a positive repair budget; the model export
    # calibrates the unrepaired baseline, so it needs budget 0.
    budget = flags.get("critical_cohort_budget", 0)
    return not ((flags.get("critical_cohort_report", "unset") != "unset" and budget == 0)
                or (flags.get("critical_cohort_model_out", "unset") != "unset" and budget != 0))


_SUPPORTED_FLAG_NAMES = frozenset(
    name for defaults in _SOURCE_OPTION_SET_SCHEMAS.values() for name in defaults
)
_FLAG_TYPE_NAMES = (FLAG_BOOLEAN_FIELDS | FLAG_INTEGER_FIELDS
                    | FLAG_INTEGER_OR_UNSET_FIELDS | FLAG_NUMBER_FIELDS
                    | FLAG_STRING_FIELDS | FLAG_LIST_FIELDS)
if _FLAG_TYPE_NAMES != _SUPPORTED_FLAG_NAMES:
    raise RuntimeError("nextpnr recipe flag types do not cover the classified option sets")
if any(not _valid_flag_value(name, value)
       for defaults in _SOURCE_OPTION_SET_SCHEMAS.values()
       for name, value in defaults.items()):
    raise RuntimeError("a classified nextpnr default does not satisfy its option type")


class RecipeError(ValueError):
    """A registered producer disagrees with its pinned P&R recipe."""


def _schema_for_option_set(option_names: Sequence[str] | set[str] | frozenset[str]
                           ) -> Mapping[str, object]:
    actual = frozenset(option_names)
    schema = _SOURCE_OPTION_SET_SCHEMAS.get(actual)
    if schema is None:
        schema = next((classified_schema
                       for classified_options, classified_schema
                       in _SOURCE_OPTION_SET_SCHEMAS.items()
                       if actual == classified_options - _ABSENT_BY_RECIPE_OPTIONS), None)
    if schema is not None:
        return schema
    expected = min(_SOURCE_OPTION_SET_SCHEMAS,
                   key=lambda known: len(known.symmetric_difference(actual)))
    raise RecipeError(
        "nextpnr CLI option set differs from every classified schema; "
        f"missing {sorted(expected - actual)}, extra {sorted(actual - expected)}")


def _schema_for_flag_names(flag_names: Sequence[str] | set[str] | frozenset[str]
                           ) -> Mapping[str, object]:
    actual = frozenset(flag_names)
    for schema in _SOURCE_OPTION_SET_SCHEMAS.values():
        if actual == frozenset(schema):
            return schema
    raise RecipeError("recipe flags do not match a classified nextpnr option schema")


def _effective_flags(core_id: str, recipe: Mapping[str, object],
                     schema: Mapping[str, object]) -> dict[str, object]:
    """Fill shared defaults, then require exact classification of this CLI schema."""
    flags = recipe["flags"]
    listed = set(flags)
    options = set(schema)
    unknown = listed - options
    missing = options - listed - set(_ADDED_OPTIONS)
    if unknown or missing:
        raise RecipeError(
            f"{core_id}: recipe flags differ from the nextpnr option schema; "
            f"missing {sorted(missing)}, extra {sorted(unknown)}")
    effective = {
        name: flags[name] if name in flags else _ADDED_OPTIONS[name]
        for name in schema
    }
    if (any(not _valid_flag_value(name, value) for name, value in effective.items())
            or not _valid_settings(effective) or not _valid_cohort_options(effective)):
        raise RecipeError(f"{core_id}: invalid nextpnr option value")
    return effective


def _runtime_schema_registry_key(core_id: str, pin: str) -> tuple[str, str, str]:
    return str(REGISTRY.resolve()), core_id, pin


def _validated_option_schema(
    core_id: str, option_names: Sequence[str] | set[str] | frozenset[str]
) -> tuple[Mapping[str, object], str]:
    recipe, pin = _selected(core_id)
    schema = _schema_for_option_set(option_names)
    _effective_flags(core_id, recipe, schema)
    return schema, pin


def validate_option_names(core_id: str, option_names: Sequence[str] | set[str] | frozenset[str]
                          ) -> Mapping[str, object]:
    """Select and validate a recipe schema from a supplied compiler option set."""
    schema, pin = _validated_option_schema(core_id, option_names)
    registry_key = _runtime_schema_registry_key(core_id, pin)
    _RUNTIME_SCHEMAS[(*registry_key, None)] = schema
    _ACTIVE_RUNTIME_SCHEMA_IDENTITIES[registry_key] = None
    return schema


@lru_cache(maxsize=32)
def _help_option_names(path: str, device: int, inode: int, size: int, mtime_ns: int
                       ) -> frozenset[str]:
    try:
        result = subprocess.run([path, "--help"], capture_output=True, text=True,
                                timeout=30, check=False)
    except (OSError, subprocess.TimeoutExpired, UnicodeDecodeError) as exc:
        raise RecipeError(f"cannot inspect nextpnr CLI options for {path}: {exc}") from exc
    if result.returncode != 0:
        raise RecipeError(f"nextpnr --help failed while validating its recipe schema: {path}")
    options = frozenset(
        match.group(1).replace("-", "_")
        for line in (result.stdout + "\n" + result.stderr).splitlines()
        if (match := _HELP_OPTION.match(line)) is not None
    )
    if not options:
        raise RecipeError(f"nextpnr --help exposed no CLI options: {path}")
    return options


def _nextpnr_tool_identity(binary: str | Path) -> tuple[Path, tuple[str, int, int, int, int]]:
    path = Path(binary).resolve()
    try:
        info = path.stat()
    except OSError as exc:
        raise RecipeError(f"cannot inspect nextpnr CLI options for {path}: {exc}") from exc
    if not path.is_file() or not os.access(path, os.X_OK):
        raise RecipeError(f"nextpnr CLI option source is not executable: {path}")
    identity = (str(path), info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns)
    return path, identity


def validate_nextpnr_tool(core_id: str, binary: str | Path) -> Mapping[str, object]:
    """Read this executable's --help and validate its full option set."""
    path, identity = _nextpnr_tool_identity(binary)
    _, device, inode, size, mtime_ns = identity
    schema, pin = _validated_option_schema(
        core_id, _help_option_names(str(path), device, inode, size, mtime_ns))
    registry_key = _runtime_schema_registry_key(core_id, pin)
    _RUNTIME_SCHEMAS[(*registry_key, identity)] = schema
    _ACTIVE_RUNTIME_SCHEMA_IDENTITIES[registry_key] = identity
    return schema


def core_for_lock(root: str | Path, lock_path: str | Path) -> str | None:
    """Return the registered core whose per-core lock was authenticated."""
    root = Path(root).resolve()
    lock_path = Path(lock_path)
    if not lock_path.is_absolute():
        lock_path = root / lock_path
    try:
        relative = lock_path.resolve().relative_to(root).as_posix()
    except (OSError, ValueError):
        return None
    return next((entry["core_id"] for entry in _registry()[0]["recipes"]
                 if entry.get("lock_path") == relative), None)


def _lock_nextpnr(core_id: str, relative: object) -> str:
    """Return the nextpnr commit pinned by a recipe's own lock."""
    if (not isinstance(relative, str) or not relative or relative.startswith("/")
            or any(part in ("", ".", "..") for part in relative.split("/"))):
        raise RecipeError(f"{core_id}: invalid toolchain lock path")
    try:
        # The selected core's lock is a pinned producer input and authenticates
        # its toolchain slot; other cores' locks are only validated here.
        from scripts.functional_execution import read_execution_data
        lock = tomllib.loads(read_execution_data(
            FES_ROOT / "sources/misteross" / relative).decode("utf-8"))
        commit = lock["tool"]["nextpnr"]["commit"]
    except (OSError, UnicodeDecodeError, tomllib.TOMLDecodeError, KeyError, TypeError) as exc:
        raise RecipeError(f"{core_id}: cannot read the nextpnr pin in {relative}: {exc}") from exc
    return commit


def _registry() -> tuple[dict, dict[str, dict], dict[str, str]]:
    try:
        # The selected table is sealed into every registered producer record
        # with recipe_digest(). Use the narrow capture helper so a functional
        # source guard does not turn this parent-owned config into an implicit
        # producer source root.
        from scripts.functional_execution import read_execution_data
        document = tomllib.loads(read_execution_data(REGISTRY).decode("utf-8"))
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise RecipeError(f"cannot read P&R recipe registry: {exc}") from exc
    if set(document) != {"version", "recipes"} or document["version"] != 1:
        raise RecipeError("unsupported P&R recipe registry")
    entries = document["recipes"]
    recipes = {}
    pins = {}
    for entry in entries:
        core_id = entry.get("core_id")
        pnr = entry.get("pnr")
        if not isinstance(core_id, str) or not isinstance(pnr, dict):
            raise RecipeError("every registered core must have a [recipes.pnr] table")
        if set(pnr) != RECIPE_FIELDS:
            raise RecipeError(f"{core_id}: P&R fields differ from the closed schema")
        pin = _lock_nextpnr(core_id, entry.get("lock_path"))
        if not isinstance(pnr["flags"], dict):
            raise RecipeError(f"{core_id}: P&R flags must be a table")
        unknown_flags = set(pnr["flags"]) - _SUPPORTED_FLAG_NAMES
        if unknown_flags:
            raise RecipeError(f"{core_id}: unknown nextpnr recipe flags {sorted(unknown_flags)}")
        if (any(not _valid_flag_value(name, value) for name, value in pnr["flags"].items())
                or not _valid_settings(pnr["flags"]) or not _valid_cohort_options(pnr["flags"])):
            raise RecipeError(f"{core_id}: invalid nextpnr option value")
        if (not all(isinstance(pnr[key], list) and pnr[key]
                    for key in ("seeds", "timing_weights", "search_weights", "clocks"))
                or any(type(value) is not int or value < 0 for value in pnr["seeds"])
                or any(type(value) is not int or value <= 0
                       for key in ("timing_weights", "search_weights") for value in pnr[key])
                or type(pnr["critexp"]) is not int or pnr["critexp"] <= 0
                or type(pnr["timeout_s"]) is not int or pnr["timeout_s"] < 0
                or type(pnr["threads"]) is not int or pnr["threads"] <= 0
                or type(pnr["timing_allow_fail"]) is not bool
                or type(pnr["compress_rbf"]) is not bool
                or type(pnr["detailed_timing_report"]) is not bool
                or not isinstance(pnr["freq"], str) or not pnr["freq"]
                or not isinstance(pnr["qsf"], str) or not isinstance(pnr["sdc"], str)
                or pnr["router"] not in ("gpu", "router1", "router2")
                or pnr["backend"] not in ("HIP", "CUDA")
                or pnr["gpu_device"] not in ("lease", "default")
                or pnr["argv_style"] not in ("search", "direct")
                or not isinstance(pnr["extra"], list)
                or any(not isinstance(arg, str) or not arg
                       or any(ord(char) < 32 or ord(char) == 127 for char in arg)
                       for arg in pnr["extra"])
                or not isinstance(pnr["env"], dict)
                or not isinstance(pnr["optional_env"], dict)
                or not isinstance(pnr["unset_env"], list)):
            raise RecipeError(f"{core_id}: invalid P&R recipe values")
        if any(not isinstance(clock, dict) or set(clock) != {"name", "mhz"}
               or not isinstance(clock["name"], str)
               or type(clock["mhz"]) not in (int, float) or clock["mhz"] <= 0
               for clock in pnr["clocks"]):
            raise RecipeError(f"{core_id}: invalid P&R clock entry")
        if any(not isinstance(name, str) or not isinstance(value, str)
               for env_table in (pnr["env"], pnr["optional_env"])
               for name, value in env_table.items()):
            raise RecipeError(f"{core_id}: invalid P&R environment allowlist")
        if set(pnr["env"]) & set(pnr["optional_env"]):
            raise RecipeError(f"{core_id}: environment names cannot be both required and optional")
        if core_id in recipes:
            raise RecipeError(f"duplicate P&R recipe for {core_id}")
        recipes[core_id] = pnr
        pins[core_id] = pin
    if not recipes:
        raise RecipeError("P&R recipe registry is empty")
    return document, recipes, pins


def _selected(core_id: str) -> tuple[dict, str]:
    registry = _registry()
    try:
        return registry[1][core_id], registry[2][core_id]
    except KeyError as exc:
        raise RecipeError(f"unknown P&R recipe {core_id!r}") from exc


def recipe_for(core_id: str, binary: str | Path | None = None) -> dict:
    """Return the P&R table with the schema validated for the selected binary."""
    recipe, pin = _selected(core_id)
    registry_key = _runtime_schema_registry_key(core_id, pin)
    if binary is None:
        identity = _ACTIVE_RUNTIME_SCHEMA_IDENTITIES.get(registry_key)
    elif Path(binary).is_file():
        _, identity = _nextpnr_tool_identity(binary)
        schema_key = (*registry_key, identity)
        if schema_key not in _RUNTIME_SCHEMAS:
            validate_nextpnr_tool(core_id, binary)
    else:
        # Recipe-only unit tests and preflight callers may have a placeholder
        # tool path. Real producers authenticate and validate their tool first.
        identity = _ACTIVE_RUNTIME_SCHEMA_IDENTITIES.get(registry_key)
    schema = _RUNTIME_SCHEMAS.get((*registry_key, identity))
    if schema is not None:
        recipe["flags"] = _effective_flags(core_id, recipe, schema)
    return recipe


def nextpnr_pin(core_id: str) -> str:
    """Return the nextpnr commit that a registered core's lock pins."""
    return _selected(core_id)[1]


def recipe_digest(core_id: str, binary: str | Path | None = None) -> str:
    payload = json.dumps(recipe_for(core_id, binary), sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(payload.encode()).hexdigest()


def required_clocks(core_id: str) -> tuple[tuple[str | None, float], ...]:
    """Return the producer's required clock matcher list from its recipe."""
    return tuple((None if clock["name"] == "*" else clock["name"], clock["mhz"])
                 for clock in recipe_for(core_id)["clocks"])


def core_for_producer(producer_script: str) -> str | None:
    """Return the one core a producer builds; a shared producer must name it."""
    cores = [entry["core_id"] for entry in _registry()[0]["recipes"]
             if entry["producer_script"] == producer_script]
    if len(cores) > 1:
        raise ValueError(f"{producer_script} builds several cores; its record must name core_id")
    return cores[0] if cores else None


def lock_path(root: str | Path, core_id: str) -> Path:
    """Return a registered core's lock path below its misteross source root."""
    entry = next(item for item in _registry()[0]["recipes"] if item["core_id"] == core_id)
    return Path(root) / entry["lock_path"]


def _path(value: str | Path) -> str:
    return os.fspath(value)


def _flag_arguments(flags: Mapping[str, object], defaults: Mapping[str, object]) -> tuple[str, ...]:
    """Serialize only values that differ from the pinned nextpnr's defaults."""
    arguments: list[str] = []
    for name in sorted(defaults):
        value = flags[name]
        if value == defaults[name]:
            continue
        option = "--" + name.replace("_", "-")
        if name in FLAG_BOOLEAN_FIELDS:
            if value:
                arguments.append(option)
        elif name in FLAG_LIST_FIELDS:
            for item in value:
                arguments.extend((option, item))
        else:
            arguments.extend((option, str(value)))
    return tuple(arguments)


_GENERATED_SOCKET_QSF_CORES = frozenset({
    "fes.zx81", "fes.zx80", "fes.ts1000", "fes.ts1500",
    "fes.coleco", "fes.sms", "fes.sg1000", "fes.apple2",
    "fes.c64", "fes.spectrum", "fes.spectrum128", "fes.spectrum-plus3",
    "fes.pentagon", "fes.atari-st",
})


def _validate_input_path(core_id: str, actual: str | Path | None, expected: str, *, qsf: bool) -> None:
    if actual is None:
        if expected:
            raise RecipeError(f"{core_id}: required constraint input is missing")
        return
    value = Path(actual).as_posix()
    if value == expected or value.endswith("/" + expected):
        return
    actual_path = Path(actual)
    if (qsf and core_id in _GENERATED_SOCKET_QSF_CORES
            and actual_path.name == "socket.qsf" and actual_path.is_file()):
        return
    if actual_path.name == Path(expected).name and actual_path.is_file():
        expected_path = FES_ROOT / "sources/misteross" / expected
        if expected_path.is_file():
            from scripts.functional_execution import read_execution_data
            if hashlib.sha256(read_execution_data(actual_path)).digest() == hashlib.sha256(
                    read_execution_data(expected_path)).digest():
                return
    kind = "QSF" if qsf else "SDC"
    raise RecipeError(f"{core_id}: unrecorded {kind} input {actual}")


def argv(
    core_id: str,
    slot: str | Path,
    synth: str | Path,
    seed: int,
    gpu: int | None,
    *,
    output: str | Path,
    device: str = "5CSEBA6U23I7",
    qsf: str | Path | None = None,
    sdc: str | Path | None = None,
    weight: int | None = None,
    critexp: int | None = None,
    extra: Sequence[str] = (),
) -> tuple[str, ...]:
    """Build the complete nextpnr command for a registered FES producer."""
    pin = nextpnr_pin(core_id)
    # Revalidate the exact executable supplied to this argv build. The help
    # probe is cached by that executable's file identity, not by the recipe pin.
    tool_exists = Path(slot).is_file()
    if tool_exists:
        validate_nextpnr_tool(core_id, slot)
    recipe = recipe_for(core_id, slot if tool_exists else None)
    flags = recipe["flags"]
    if tool_exists:
        _, tool_identity = _nextpnr_tool_identity(slot)
        schema_key = (*_runtime_schema_registry_key(core_id, pin), tool_identity)
        defaults = _RUNTIME_SCHEMAS.get(schema_key) or _schema_for_flag_names(flags)
    else:
        defaults = _schema_for_flag_names(flags)
    if set(flags) != set(defaults):
        raise RecipeError(f"{core_id}: the flags do not classify the nextpnr option set")
    if device != "5CSEBA6U23I7":
        raise RecipeError(f"{core_id}: unexpected target device {device!r}")
    if extra and tuple(extra) != tuple(recipe["extra"]):
        raise RecipeError(f"{core_id}: producer supplied an unrecorded nextpnr extra")
    if seed not in recipe["seeds"]:
        raise RecipeError(f"{core_id}: nextpnr seed is not recorded in its recipe")
    if critexp is not None and critexp != recipe["critexp"]:
        raise RecipeError(f"{core_id}: producer critexp disagrees with its recipe")
    if recipe["gpu_device"] == "lease" and (type(gpu) is not int or gpu < 0):
        raise RecipeError(f"{core_id}: leased GPU slot is required")

    qsf_value = qsf if qsf is not None else recipe["qsf"]
    sdc_value = sdc if sdc is not None else recipe["sdc"]
    _validate_input_path(core_id, qsf_value, recipe["qsf"], qsf=True)
    _validate_input_path(core_id, sdc_value, recipe["sdc"], qsf=False)
    output = Path(output)
    report = output / "timing.json"
    routed = output / "routed.json"
    rbf = output / "core.rbf"
    command = [
        _path(slot), "--json", _path(synth), "--device", device,
        "--qsf", _path(qsf_value),
    ]

    if recipe["argv_style"] == "search":
        command.extend(("--seed", str(seed)))
        selected_weight = recipe["timing_weights"][0] if weight is None else weight
        allowed_weights = set(recipe["timing_weights"]) | set(recipe["search_weights"])
        if selected_weight not in allowed_weights:
            raise RecipeError(f"{core_id}: unrecorded placer timing weight {selected_weight}")
        command.extend(("--placer-heap-timingweight", str(selected_weight)))
        command.extend(("--placer-heap-critexp", str(recipe["critexp"])))
        command.extend(("--report", _path(report), "--write", _path(routed),
                        "--rbf", _path(rbf)))
        if recipe["compress_rbf"]:
            command.append("--compress-rbf")
        if recipe["detailed_timing_report"]:
            command.append("--detailed-timing-report")
        if recipe["timing_allow_fail"]:
            command.append("--timing-allow-fail")
        command.extend(("--router", recipe["router"]))
        if recipe["gpu_device"] == "lease":
            command.extend(("--gpu-device", str(gpu)))
        command.extend(recipe["extra"])
        if sdc_value:
            command.extend(("--sdc", _path(sdc_value)))
        if recipe["freq"]:
            command.extend(("--freq", str(recipe["freq"])))
    elif recipe["argv_style"] == "direct":
        if sdc_value:
            command.extend(("--sdc", _path(sdc_value)))
        if recipe["freq"]:
            command.extend(("--freq", str(recipe["freq"])))
        command.extend(("--seed", str(seed)))
        command.extend(("--router", recipe["router"]))
        command.extend(("--rbf", _path(rbf)))
        if recipe["compress_rbf"]:
            command.append("--compress-rbf")
        command.extend(("--write", _path(routed), "--report", _path(report)))
        if recipe["detailed_timing_report"]:
            command.append("--detailed-timing-report")
        if recipe["gpu_device"] == "lease":
            command.extend(("--gpu-device", str(gpu)))
        command.extend(recipe["extra"])
    else:
        raise RecipeError(f"{core_id}: unknown argv style")

    # Defaults stay recorded and golden-tested, but only nondefault values are
    # emitted so the initial recipes retain their exact producer argv.
    if recipe["threads"] != 1 or flags["parallel_refine"]:
        command.extend(("--threads", str(recipe["threads"])))
    command.extend(_flag_arguments(flags, defaults))
    return tuple(command)


def build(
    core_id: str,
    slot: str | Path,
    synth: str | Path,
    seed: int,
    gpu: int | None,
    *,
    output: str | Path,
    env: Mapping[str, str],
    **options,
) -> tuple[tuple[str, ...], dict[str, str]]:
    """Build nextpnr argv and validate its complete controlled environment."""
    command = argv(core_id, slot, synth, seed, gpu, output=output, **options)
    return nextpnr_gpu_command(command, env), environment(
        core_id, env, gpu=gpu, binary=slot if Path(slot).is_file() else None
    )


def _gpu_run_environment(
    core_id: str,
    recipe: Mapping[str, object],
    source: Mapping[str, str],
    gpu: int | None,
) -> set[str]:
    present_markers = set(source) & _GPU_RUN_MARKERS
    if not present_markers:
        return set()
    if present_markers != _GPU_RUN_MARKERS:
        raise RecipeError(f"{core_id}: incomplete gpu-run lease markers")

    raw_ids = source["FES_GPU_IDS"]
    ids = raw_ids.split(",") if isinstance(raw_ids, str) else []
    if (not ids or any(not _GPU_ID.fullmatch(gpu_id) or gpu_id in (".", "..")
                       for gpu_id in ids)
            or len(ids) != len(set(ids)) or ",".join(ids) != raw_ids):
        raise RecipeError(f"{core_id}: invalid gpu-run leased GPU ids")
    if source["GPU_RUN_DEVICE"] != "0":
        raise RecipeError(f"{core_id}: gpu-run logical device marker must be 0")
    if source["NEXTPNR_REQUIRE_GPU"] != "1":
        raise RecipeError(f"{core_id}: gpu-run must require nextpnr GPU routing")
    if gpu is not None and (type(gpu) is not int or gpu < 0 or gpu >= len(ids)):
        raise RecipeError(f"{core_id}: requested nextpnr GPU is outside the active lease")

    backend = recipe["backend"]
    expected_mask = "ROCR_VISIBLE_DEVICES" if backend == "HIP" else "CUDA_VISIBLE_DEVICES"
    present_masks = set(source) & _GPU_VISIBILITY_MASKS
    if present_masks != {expected_mask} or source[expected_mask] != raw_ids:
        raise RecipeError(f"{core_id}: gpu-run visibility mask does not match its lease")

    expected_device = source["GPU_RUN_EXPECTED_DEVICE"]
    if backend == "HIP":
        if expected_device == f"amd:{ids[0]}":
            device_matches = _gpu_device_path("amd", ids[0]) is None
        elif (isinstance(expected_device, str)
              and _AMD_RENDER_NODE.fullmatch(expected_device) is not None):
            device_matches = expected_device == _gpu_device_path("amd", ids[0])
        else:
            device_matches = False
    else:
        device_matches = (expected_device == f"nvidia:{ids[0]}"
                          or ids[0].isdigit()
                          and expected_device == f"/dev/nvidia{ids[0]}")
    if not device_matches:
        raise RecipeError(f"{core_id}: gpu-run expected device does not match its lease")
    return set(_GPU_RUN_MARKERS) | {expected_mask}


@lru_cache(maxsize=1)
def _gpu_lease_module():
    """Load the FES gpu-run helper shared with the module's P&R recipes."""
    helper = FES_ROOT / "scripts" / "gpu_lease.py"
    spec = spec_from_file_location("fes_gpu_lease", helper)
    if spec is None or spec.loader is None:
        raise RecipeError("cannot load the gpu-run device resolver")
    module = module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def _gpu_device_path(vendor: str, gpu_id: str) -> str | None:
    """Resolve a leased physical GPU id with gpu-run's KFD mapping."""
    try:
        return _gpu_lease_module().gpu_device_path(vendor, gpu_id)
    except (AttributeError, ImportError, OSError) as error:
        raise RecipeError("cannot resolve gpu-run leased device path") from error


def environment(
    core_id: str,
    source: Mapping[str, str],
    *,
    gpu: int | None = None,
    binary: str | Path | None = None,
) -> dict[str, str]:
    """Validate and return only recipe values and a coherent active gpu-run lease."""
    recipe = recipe_for(core_id, binary)
    required = recipe["env"]
    optional = recipe["optional_env"]
    lease_names = _gpu_run_environment(core_id, recipe, source, gpu)
    allowed = set(required) | set(optional)
    recipe_source = {name: value for name, value in source.items()
                     if name not in lease_names}
    unexpected = set(recipe_source) - allowed
    missing = set(required) - set(recipe_source)
    if unexpected:
        raise RecipeError(f"{core_id}: unrecorded nextpnr environment names: {sorted(unexpected)}")
    if missing:
        raise RecipeError(f"{core_id}: required nextpnr environment names are missing: {sorted(missing)}")
    for name, expected in required.items():
        actual = recipe_source[name]
        if name == "PATH":
            if not actual.endswith(":/usr/bin:/bin") or not Path(actual.split(":", 1)[0]).is_absolute():
                raise RecipeError(f"{core_id}: PATH does not match the authenticated toolchain lane")
        elif name == "HOME":
            if not Path(actual).is_absolute():
                raise RecipeError(f"{core_id}: HOME must be the private producer home")
        elif actual != expected:
            raise RecipeError(f"{core_id}: environment value for {name} differs from recipe")
    for name, expected in optional.items():
        if name in recipe_source and recipe_source[name] != expected:
            raise RecipeError(f"{core_id}: environment value for {name} differs from recipe")
    if (set(recipe_source) & set(recipe["unset_env"])):
        raise RecipeError(f"{core_id}: explicitly unset environment reached nextpnr")
    lease = {name: source[name] for name in lease_names}
    return dict(recipe_source, **lease)
