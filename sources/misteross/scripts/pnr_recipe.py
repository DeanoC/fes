"""The FES recipe registry's single nextpnr argv and environment builder."""
from __future__ import annotations

import hashlib
import json
import math
import os
import re
import tomllib
from functools import lru_cache
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
from typing import Mapping, Sequence
from scripts.fes_build_common import nextpnr_gpu_command


FES_ROOT = Path(__file__).resolve().parents[3]
REGISTRY = FES_ROOT / "config/core-recipes.toml"
PINNED_NEXTPNR = "9128800fa6e77c4138088a7107a95376ba47ff15"
CURRENT_FLAGS = {
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

# These are the Mistral/FES switches available at PINNED_NEXTPNR. Keep every
# one classified in each per-core recipe, including options set to their
# current disabled/default value. Later nextpnr additions require a schema
# decision before a lock can move.
FORK_FLAGS = frozenset({
    "fes_scaffold", "fes_cart", "fes_cart_region", "fes_slot_clock",
    "fes_cram_region", "mistral_ff4", "mistral_clkb", "replicate_enables",
    "remap_critical", "remap_optimize_pins", "remap_preserve_ffs",
    "remap_plan", "remap_post_plan", "remap_comb_critical",
    "remap_comb_candidate", "remap_comb_plan", "remap_decompose_critical",
    "remap_decompose_candidate", "remap_lut_pair_critical",
    "remap_lut_pair_candidate", "remap_lut_pair_compose_copy",
    "remap_lut_driver_critical", "remap_lut_driver_candidate",
    "remap_candidate", "remap_groups", "balance_reduction_root",
})

# Result-affecting non-fork switches whose values also need to stay reviewed.
RECIPE_FLAGS = frozenset({
    "randomize_seed", "placer", "cstrweight", "starttemp", "pack_only",
    "no_route", "no_place", "no_pack", "ignore_loops", "ignore_rel_clk",
    "no_tmdriv", "sdf_cvc", "no_print_critical_path_source",
    "placer_heap_alpha", "placer_heap_beta",
    "placer_heap_cell_placement_timeout", "placer_heap_no_ctrl_set",
    "static_dump_density", "parallel_refine", "router2_heatmap",
    "tmg_ripup", "router2_tmg_ripup", "router2_alt_weights",
    "gpu_batches", "gpu_cpu", "gpu_perf", "gpu_telemetry", "gpu_opt",
    "timing_report_paths", "seed_racing_contract",
}) | FORK_FLAGS

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
    "remap_lut_pair_compose_copy",
})
FLAG_INTEGER_FIELDS = frozenset({
    "placer_heap_cell_placement_timeout", "timing_report_paths",
    "replicate_enables", "remap_groups",
})
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
})
FLAG_LIST_FIELDS = frozenset({"gpu_opt", "balance_reduction_root"})

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
        return type(value) is int and value >= 0
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


if (FLAG_BOOLEAN_FIELDS | FLAG_INTEGER_FIELDS | FLAG_INTEGER_OR_UNSET_FIELDS
        | FLAG_NUMBER_FIELDS | FLAG_STRING_FIELDS | FLAG_LIST_FIELDS) != RECIPE_FLAGS:
    raise RuntimeError("nextpnr recipe flag types do not cover the closed flag schema")


class RecipeError(ValueError):
    """A registered producer disagrees with its pinned P&R recipe."""


def _registry() -> tuple[dict, dict[str, dict]]:
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
    for entry in entries:
        core_id = entry.get("core_id")
        pnr = entry.get("pnr")
        if not isinstance(core_id, str) or not isinstance(pnr, dict):
            raise RecipeError("every registered core must have a [recipes.pnr] table")
        if set(pnr) != RECIPE_FIELDS:
            raise RecipeError(f"{core_id}: P&R fields differ from the closed schema")
        if not isinstance(pnr["flags"], dict) or set(pnr["flags"]) != RECIPE_FLAGS:
            raise RecipeError(f"{core_id}: every pinned nextpnr option must be classified")
        if any(not _valid_flag_value(name, value) for name, value in pnr["flags"].items()):
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
    if not recipes:
        raise RecipeError("P&R recipe registry is empty")
    return document, recipes


def recipe_for(core_id: str) -> dict:
    """Return one validated P&R table from config/core-recipes.toml."""
    try:
        return _registry()[1][core_id]
    except KeyError as exc:
        raise RecipeError(f"unknown P&R recipe {core_id!r}") from exc


def recipe_digest(core_id: str) -> str:
    payload = json.dumps(recipe_for(core_id), sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(payload.encode()).hexdigest()


def required_clocks(core_id: str) -> tuple[tuple[str | None, float], ...]:
    """Return the producer's required clock matcher list from its recipe."""
    return tuple((None if clock["name"] == "*" else clock["name"], clock["mhz"])
                 for clock in recipe_for(core_id)["clocks"])


def core_for_producer(producer_script: str) -> str | None:
    for entry in _registry()[0]["recipes"]:
        if entry["producer_script"] == producer_script:
            return entry["core_id"]
    return None


def lock_path(root: str | Path, core_id: str) -> Path:
    """Return a registered core's lock path below its misteross source root."""
    entry = next(item for item in _registry()[0]["recipes"] if item["core_id"] == core_id)
    return Path(root) / entry["lock_path"]


def _path(value: str | Path) -> str:
    return os.fspath(value)


def _flag_arguments(flags: Mapping[str, object]) -> tuple[str, ...]:
    """Serialize only explicitly nondefault options, preserving today's argv."""
    arguments: list[str] = []
    for name in sorted(RECIPE_FLAGS):
        value = flags[name]
        if value == CURRENT_FLAGS[name]:
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
    "fes.zx81", "fes.coleco", "fes.sms", "fes.sg1000", "fes.apple2",
    "fes.c64", "fes.spectrum", "fes.atari-st",
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
    recipe = recipe_for(core_id)
    flags = recipe["flags"]
    if device != "5CSEBA6U23I7":
        raise RecipeError(f"{core_id}: unexpected target device {device!r}")
    if extra and tuple(extra) != tuple(recipe["extra"]):
        raise RecipeError(f"{core_id}: producer supplied an unrecorded nextpnr extra")
    if seed not in recipe["seeds"]:
        raise RecipeError(f"{core_id}: nextpnr seed is not recorded in its recipe")
    if critexp is not None and critexp != recipe["critexp"]:
        raise RecipeError(f"{core_id}: producer critexp disagrees with its recipe")
    if recipe["gpu_device"] == "lease" and (type(gpu) is not int or gpu < 0):
        raise RecipeError(f"{core_id}: leased GPU device is required")

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
    command.extend(_flag_arguments(flags))
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
    return nextpnr_gpu_command(command, env), environment(core_id, env, gpu=gpu)


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
) -> dict[str, str]:
    """Validate and return only recipe values and a coherent active gpu-run lease."""
    recipe = recipe_for(core_id)
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
