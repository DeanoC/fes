"""Controlled invocation and conservative machine inputs for functional IDs.

This is a native Linux HIP lane. Capturing host libraries and KFD topology keeps
reuse conservative across driver/device changes; it is not a portable container
or a claim that HIP place-and-route produces identical bytes on every run.
"""
from __future__ import annotations

import hashlib
import json
import os
import platform
import re
import subprocess
from pathlib import Path


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

def execution_environment(home: Path, tools: dict[str, Path]) -> dict[str, str]:
    # No ambient LD_PRELOAD, YOSYS_DATDIR, ROCm device masking, Python paths or
    # user rc/cache settings enter synthesis/placement. Executables are absolute.
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
        files[str(path)] = hashlib.sha256(path.read_bytes()).hexdigest()

    executables = set(tools.values())
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
    for prefix in sorted({binary.parent.parent for binary in executables}):
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


def _machine_input_files():
    topology = Path("/sys/class/kfd/kfd/topology/nodes")
    properties = sorted(topology.glob("*/properties"))
    if not properties:
        raise ValueError("functional HIP build requires readable KFD GPU topology")
    files = properties + sorted(topology.glob("*/gpu_id"))
    for path in (Path("/sys/module/amdgpu/version"), Path("/etc/os-release")):
        if path.is_file():
            files.append(path)
    return files


def execution_digest(inputs: dict) -> str:
    return hashlib.sha256(json.dumps(inputs, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


class FunctionalInvocation:
    """One controlled compiler invocation, shared by the production producers."""
    def __init__(self, authenticated, gpu_device=0):
        import tempfile
        self.tools = {name: tool.path for name, tool in authenticated.items()}
        self.gpu_device = gpu_device
        self.home = tempfile.TemporaryDirectory(prefix="fes-tool-home-")
        self.env = execution_environment(Path(self.home.name), self.tools)
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
