"""Immutable format-2 recipe descriptors owned by the FES parent."""
from dataclasses import dataclass
import os
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


FORMAT2_RECIPES = {
    "fes.pong": Format2Recipe(
        core_id="fes.pong",
        producer_script="scripts/build_fes_pong.py",
        producer_module="scripts.build_fes_pong",
        authenticate="_authenticate_tools",
        lock_path="toolchain.lock",
        gpu_router=HIP_ROUTER,
        hip_architectures=HIP_ARCHITECTURES,
        selection_filename="fes-pong.package-selection.toml",
        package_dir_env="FES_PONG_PACKAGE_DIR",
        package_selection_env="FES_PONG_PACKAGE_SELECTION",
        quartus_role="check only when a twin exists",
    ),
    "fes.zx81": Format2Recipe(
        core_id="fes.zx81",
        producer_script="scripts/build_fes_zx81_oss.py",
        producer_module="scripts.build_fes_zx81_oss",
        authenticate="_authenticate_tools",
        lock_path="toolchain.lock",
        gpu_router=HIP_ROUTER,
        hip_architectures=HIP_ARCHITECTURES,
        selection_filename="fes-zx81.package-selection.toml",
        package_dir_env="FES_ZX81_PACKAGE_DIR",
        package_selection_env="FES_ZX81_PACKAGE_SELECTION",
        quartus_role="bring-up/check oracle",
    ),
    "fes.coleco": Format2Recipe(
        core_id="fes.coleco",
        producer_script="scripts/build_fes_coleco_oss.py",
        producer_module="scripts.build_fes_coleco_oss",
        authenticate="_authenticate_coleco_tools",
        lock_path="cores/fes-coleco/toolchain.lock",
        gpu_router=HIP_ROUTER,
        hip_architectures=HIP_ARCHITECTURES,
        selection_filename="fes-coleco.package-selection.toml",
        package_dir_env="FES_COLECO_PACKAGE_DIR",
        package_selection_env="FES_COLECO_PACKAGE_SELECTION",
        quartus_role="bring-up/check oracle",
    ),
    "fes.sms": Format2Recipe(
        core_id="fes.sms",
        producer_script="scripts/build_fes_sms_oss.py",
        producer_module="scripts.build_fes_sms_oss",
        authenticate="_authenticate_sms_tools",
        lock_path="cores/fes-sms/toolchain.lock",
        gpu_router=HIP_ROUTER,
        hip_architectures=HIP_ARCHITECTURES,
        selection_filename="fes-sms.package-selection.toml",
        package_dir_env="FES_SMS_PACKAGE_DIR",
        package_selection_env="FES_SMS_PACKAGE_SELECTION",
        quartus_role="bring-up/check oracle",
    ),
}


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
