"""Controlled invocation and conservative machine inputs for functional IDs.

Capturing host libraries plus the readable AMD KFD and NVIDIA topology and
driver records keeps reuse conservative across driver and device changes.
On a mixed AMD/NVIDIA host both families are recorded, so a CUDA build cannot
reuse an identity that omitted an NVIDIA change. Canonical record generation
and the producers call this same selection. This is not a portable container
or a claim that place-and-route produces identical bytes on every run.
"""
from __future__ import annotations

import hashlib
import json
import os
import platform
import re
import subprocess
import sys
from pathlib import Path
from scripts.compiler_read_audit import TRACER, read_execution_data


_REQUIRED_YOSYS_SUPPORT = (
    "techmap.v", "cmp2lut.v", "intel/common/altpll_bb.v",
    "intel_alm/cyclonev/cells_sim.v", "intel_alm/common/abc9_model.v",
    "intel_alm/common/abc9_map.v", "intel_alm/common/abc9_unmap.v",
    "intel_alm/common/alm_sim.v", "intel_alm/common/alm_map.v",
    "intel_alm/common/dff_sim.v", "intel_alm/common/dff_map.v",
    "intel_alm/common/dsp_sim.v", "intel_alm/common/dsp_map.v",
    "intel_alm/common/mem_sim.v", "intel_alm/common/misc_sim.v",
    "intel_alm/common/megafunction_bb.v", "intel_alm/common/arith_alm_map.v",
)
_GPU_LEASE_ENVIRONMENT = (
    "FES_GPU_IDS", "GPU_RUN_DEVICE", "GPU_RUN_EXPECTED_DEVICE",
    "NEXTPNR_REQUIRE_GPU", "HIP_VISIBLE_DEVICES", "ROCR_VISIBLE_DEVICES",
    "CUDA_VISIBLE_DEVICES",
)

def execution_environment(home: Path, tools: dict[str, Path]) -> dict[str, str]:
    # No ambient loader, Yosys, GPU, Python-path or user rc/cache settings enter
    # synthesis/placement. FunctionalInvocation separately restores gpu-run's
    # lease marker and visibility mask when one is active. Executables are absolute.
    bins = sorted({str(path.parent) for path in tools.values()})
    return {"PATH": ":".join([*bins, "/usr/bin", "/bin"]),
            "HOME": str(home), "LANG": "C", "LC_ALL": "C", "TZ": "UTC"}


def execution_inputs(tools: dict[str, Path], env: dict[str, str], gpu_device: int) -> dict:
    if type(gpu_device) is not int or gpu_device < 0:
        raise ValueError("functional build requires a nonnegative GPU device")
    files = {}

    def capture(path: Path):
        # System library symlinks are expected; bind their resolved bytes and
        # lexical pathname, unlike source inputs whose symlinks are forbidden.
        files[str(path)] = hashlib.sha256(read_execution_data(path)).hexdigest()

    executables = {*tools.values(), TRACER}
    if "yosys" in tools:
        # synth_intel_alm invokes abc9, whose default external executable is
        # <yosys-bindir>/yosys-abc (passes/techmap/abc9_exe.cc).
        abc = tools["yosys"].parent / "yosys-abc"
        if not abc.is_file() or not os.access(abc, os.X_OK):
            raise ValueError("functional synthesis requires installed yosys-abc")
        executables.add(abc)
        support = tools["yosys"].parent.parent / "share/yosys"
        if any(not (support / relative).is_file() for relative in _REQUIRED_YOSYS_SUPPORT):
            raise ValueError("functional synthesis requires complete Intel ALM support data")

    for binary in sorted(executables):
        if not binary.is_file() or not os.access(binary, os.X_OK):
            raise ValueError(f"functional build executable is unavailable: {binary.name}")
        capture(binary)
        result = subprocess.run(["/usr/bin/ldd", str(binary)], env=env,
                                capture_output=True, text=True, check=False)
        if result.returncode and "not a dynamic executable" not in result.stderr + result.stdout:
            raise ValueError(f"cannot identify dynamic libraries for {binary.name}")
        if "not found" in result.stdout:
            raise ValueError(f"missing dynamic library for {binary.name}")
        for name in re.findall(r"(?:=>\s+|^\s*)(/[^\s]+)", result.stdout, re.MULTILINE):
            capture(Path(name))
    for prefix in sorted({binary.parent.parent for binary in executables if binary != TRACER}):
        support = prefix / "share"
        if support.is_dir():
            for path in sorted(support.rglob("*")):
                if path.is_symlink() and path.is_dir():
                    raise ValueError("functional support directory must not be a symlink")
                if path.is_file():
                    capture(path)

    for path in _machine_input_files():
        capture(path)
    normalized_env = dict(env, HOME="<private-build-home>")
    # Authenticated installation paths are deliberately retained for now. This
    # prevents unsafe cross-install reuse until relocation is qualified.
    return {"version": 1, "system": platform.system(), "machine": platform.machine(),
            "kernel": platform.release(), "gpu_device": gpu_device,
            "environment": normalized_env, "files": dict(sorted(files.items()))}


def _gpu_machine_files(kfd_topology: Path, amdgpu_version: Path, nvidia_gpus: Path,
                       nvidia_version: Path, os_release: Path) -> list[Path]:
    """Record every readable GPU family so mixed hosts cannot hide one of them."""

    files: list[Path] = []
    properties = sorted(kfd_topology.glob("*/properties"))
    if properties:
        files.extend(properties)
        files.extend(sorted(kfd_topology.glob("*/gpu_id")))
        if amdgpu_version.is_file():
            files.append(amdgpu_version)
    information = sorted(nvidia_gpus.glob("*/information")) if nvidia_gpus.is_dir() else []
    if information:
        files.extend(information)
        if nvidia_version.is_file():
            files.append(nvidia_version)
    if not files:
        raise ValueError(
            "functional GPU build requires readable KFD topology or NVIDIA GPU information"
        )
    if os_release.is_file():
        files.append(os_release)
    return files


def _machine_input_files():
    return _gpu_machine_files(
        Path("/sys/class/kfd/kfd/topology/nodes"),
        Path("/sys/module/amdgpu/version"),
        Path("/proc/driver/nvidia/gpus"),
        Path("/proc/driver/nvidia/version"),
        Path("/etc/os-release"),
    )


def execution_digest(inputs: dict) -> str:
    return hashlib.sha256(json.dumps(inputs, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


class FunctionalInvocation:
    """One controlled compiler invocation, shared by the production producers."""
    def __init__(self, authenticated, gpu_device=0, extra_env=None):
        import tempfile
        self.tools = {name: tool.path for name, tool in authenticated.items()}
        self.gpu_device = gpu_device
        self.home = tempfile.TemporaryDirectory(prefix="fes-tool-home-")
        self.env = execution_environment(Path(self.home.name), self.tools)
        from scripts.fes_build_common import gpu_run_environment
        self.env.update(gpu_run_environment())
        if extra_env:
            self.env.update(extra_env)
        # Keep the lease-selected GPU visible through the controlled build env.
        # These values identify the physical device behind logical --gpu-device
        # indices and participate in the functional input digest below.
        if (os.environ.get("FES_GPU_IDS") and os.environ.get("GPU_RUN_DEVICE") is not None
                and os.environ.get("NEXTPNR_REQUIRE_GPU") == "1"):
            self.env.update({key: os.environ[key] for key in _GPU_LEASE_ENVIRONMENT
                             if key in os.environ})
        try:
            self.inputs = execution_inputs(self.tools, self.env, gpu_device)
        except Exception:
            self.home.cleanup()
            raise

    def verify(self):
        if execution_inputs(self.tools, self.env, self.gpu_device) != self.inputs:
            raise ValueError("functional execution inputs changed during build")

    def close(self):
        self.home.cleanup()


def source_roots_for_inputs(pinned_inputs):
    """Conservatively close over each input's owning module and shared scripts."""
    roots = {"scripts"}
    for path in pinned_inputs:
        parts = Path(path).parts
        roots.add("/".join(parts[:2]) if parts[0] == "cores" else parts[0])
    return sorted(roots)


class AuditedRoots(list):
    """Roots whose reads must be checked against the committed manifest."""


def producer_name(module_name, scripts_root):
    """Map a producer's module name to its manifest entry.

    Producers run both as `python scripts/build_x.py` (``__main__``) when they
    build and write the sidecar record, and as ``scripts.build_x`` when FES
    derives the canonical record. Both must pick the same roots, so
    ``__main__`` resolves to the running script's stem.
    """
    if module_name != '__main__':
        return module_name.removeprefix('scripts.')
    path = getattr(sys.modules.get('__main__'), '__file__', None)
    if not path or Path(path).resolve().parent != (Path(scripts_root) / 'scripts').resolve():
        return None
    return Path(path).stem


def source_roots_for_producer(module_name, pinned_inputs, root=None):
    """Use an audited closure when registered; preserve the old fallback exactly."""
    from scripts.source_closure import ROOT, load_manifest
    if (os.environ.get('FES_SOURCE_CLOSURE_BROAD') == '1' or
            os.environ.get('FES_SOURCE_CLOSURE_RECORD_ONLY') == '1'):
        return source_roots_for_inputs(pinned_inputs)
    if root is not None and Path(root).resolve() != ROOT.resolve():
        return source_roots_for_inputs(pinned_inputs)
    name = producer_name(module_name, ROOT)
    roots = None if name is None else load_manifest().get(name)
    if roots is None:
        return source_roots_for_inputs(pinned_inputs)
    # Declared inputs always stay in the key, even for producer variants
    # (e.g. video_socket) whose extra inputs the audit did not exercise.
    merged = list(roots)
    for path in pinned_inputs:
        if not any(path == item or path.startswith(item.rstrip('/') + '/') for item in merged):
            merged.append(path)
    return AuditedRoots(sorted(merged))
