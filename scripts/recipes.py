"""Immutable format-2 recipe descriptors owned by the FES parent."""
from dataclasses import dataclass
import os
import keyword
import re
import tomllib
from pathlib import Path

TOOLCHAIN_CACHE_ROOT = Path(__file__).resolve().parents[1] / "out/cache/misteross-toolchains"
HIP_ROUTER = "HIP"
HIP_ARCHITECTURES = "gfx1100;gfx1201"

_STRIP_NAMES = frozenset({
    "CC", "CXX", "CPPFLAGS", "CFLAGS", "CXXFLAGS", "LDFLAGS",
    "LD_LIBRARY_PATH", "PKG_CONFIG_PATH", "PYTHON", "PYTHON_CONFIG",
    "MAKEFLAGS", "MFLAGS", "NINJAFLAGS", "NINJA_STATUS",
    "LD_PRELOAD", "SOURCE_DATE_EPOCH",
    "GIT_DIR", "GIT_WORK_TREE", "GIT_SSH_COMMAND",
    "TOOLCHAIN_ROOT", "TOOLCHAIN_INSTALL", "TOOLCHAIN_BUILD",
    "FES_TOOLCHAIN_CACHE_ROOT", "FES_TOOLCHAIN_ROOT",
    "CUDA_HOME", "CUDACXX", "CUDA_PATH",
})
_STRIP_PREFIXES = ("GIT_CONFIG_", "CCACHE_", "DISTCC_", "CMAKE_")
_HIP_IDENTITY_NAMES = frozenset({"ROCM_PATH", "HIPCC"})


@dataclass(frozen=True)
class Format2Recipe:
    """One FES format-2 production recipe."""

    core_id: str
    producer_script: str
    producer_module: str
    authenticate: str
    lock_path: str
    gpu_router: str
    hip_architectures: str
    selection_filename: str
    package_dir_env: str
    package_selection_env: str
    quartus_role: str
    cache_root: Path = TOOLCHAIN_CACHE_ROOT


REGISTRY_PATH = Path(__file__).resolve().parents[1] / "config/core-recipes.toml"
_RECIPE_FIELDS = frozenset(Format2Recipe.__dataclass_fields__) - {"cache_root"}
_IDENTIFIER = r"[A-Za-z_][A-Za-z0-9_]*"


def _relative_path(value):
    # Validate the original spelling, before Path can normalize traversal/dots.
    return all(re.fullmatch(r"[A-Za-z0-9_][A-Za-z0-9_.-]*", part)
               and part not in (".", "..") for part in value.split("/"))


def load_recipes(path=REGISTRY_PATH):
    """Load the closed version-1 data schema; never import or execute producers."""
    with Path(path).open("rb") as source:
        document = tomllib.load(source)
    if set(document) != {"version", "recipes"}:
        raise ValueError("recipe registry requires exactly version and recipes")
    if type(document["version"]) is not int or document["version"] != 1:
        raise ValueError("unsupported recipe registry version")
    entries = document["recipes"]
    if not isinstance(entries, list) or not entries:
        raise ValueError("recipes must be a nonempty array of tables")
    result, selections, environment_names = {}, set(), set()
    for entry in entries:
        if not isinstance(entry, dict) or set(entry) != _RECIPE_FIELDS:
            raise ValueError("recipe fields must exactly match the version-1 schema")
        if any(not isinstance(value, str) or not value.strip()
               or any(ord(char) < 32 or ord(char) == 127 for char in value)
               for value in entry.values()):
            raise ValueError("recipe fields must be nonempty strings without control characters")
        core_id = entry["core_id"]
        if not re.fullmatch(r"fes\.[a-z0-9]+(?:[._-][a-z0-9]+)*", core_id):
            raise ValueError("invalid recipe core_id")
        for field in ("producer_script", "lock_path"):
            if not _relative_path(entry[field]):
                raise ValueError(f"invalid relative {field}")
        module = entry["producer_module"]
        if (not all(re.fullmatch(_IDENTIFIER, part) and not keyword.iskeyword(part)
                    for part in module.split("."))
                or entry["producer_script"] != module.replace(".", "/") + ".py"):
            raise ValueError("invalid producer_module or producer_script mismatch")
        auth = entry["authenticate"]
        if not re.fullmatch(_IDENTIFIER, auth) or keyword.iskeyword(auth):
            raise ValueError("invalid authenticate identifier")
        if entry["gpu_router"] != HIP_ROUTER:
            raise ValueError("only the HIP production route is supported")
        if not re.fullmatch(r"gfx[0-9a-f]+(?:;gfx[0-9a-f]+)*", entry["hip_architectures"]):
            raise ValueError("invalid HIP architectures")
        selection = entry["selection_filename"]
        if ("/" in selection or not _relative_path(selection)
                or not selection.endswith(".package-selection.toml")):
            raise ValueError("invalid selection_filename")
        names = (entry["package_dir_env"], entry["package_selection_env"])
        if any(not re.fullmatch(r"FES_[A-Z0-9_]+_PACKAGE_(?:DIR|SELECTION)", name)
               for name in names):
            raise ValueError("invalid package environment variable")
        if not names[0].endswith("_DIR") or not names[1].endswith("_SELECTION"):
            raise ValueError("package environment variable role mismatch")
        if core_id in result or selection in selections or any(
                name in environment_names for name in names) or names[0] == names[1]:
            raise ValueError("duplicate core ID, selection filename or environment variable")
        result[core_id] = Format2Recipe(**entry)
        selections.add(selection)
        environment_names.update(names)
    return result


FORMAT2_RECIPES = load_recipes()


def recipe_for(core_id):
    """Return the immutable descriptor for a supported format-2 core ID."""
    try:
        return FORMAT2_RECIPES[core_id]
    except KeyError as error:
        supported = ", ".join(FORMAT2_RECIPES)
        raise ValueError(
            f"unknown format-2 recipe {core_id!r}; supported: {supported}"
        ) from error


def producer_environment(env=None, recipe=None):
    """Scrub caller overrides and apply HIP identity without selecting cache mode."""
    recipe = recipe_for("fes.pong") if recipe is None else recipe
    mapped = os.environ.copy() if env is None else dict(env)
    for name in tuple(mapped):
        if name in _HIP_IDENTITY_NAMES:
            continue
        if name in _STRIP_NAMES or name.startswith(_STRIP_PREFIXES):
            del mapped[name]
    mapped.pop("FES_TOOLCHAIN_CACHE_ROOT", None)
    mapped.pop("FES_TOOLCHAIN_ROOT", None)
    mapped["FES_TOOLCHAIN_GPU_ROUTER"] = recipe.gpu_router
    mapped["FES_TOOLCHAIN_HIP_ARCHITECTURES"] = recipe.hip_architectures
    return mapped
