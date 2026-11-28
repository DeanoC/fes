#!/usr/bin/env python3
"""Source-bound registered pc386 product seal: the pc386 producer.

It seals fes.pc386 with Quartus from the machine netlist the pc386 lock's Yosys
elaborates, as scripts/pc386_quartus.py defines it, and writes a design-v2
record (#1984). --with-lockstep runs a fresh full lockstep first.
"""
from __future__ import annotations
import argparse
import ast
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile
import tomllib

# Check every core-owned module before Python can execute it. In particular,
# importing a sibling through a symlink must not escape the selected source.
PRODUCT_PATH = Path(__file__).absolute()
TOOLS = PRODUCT_PATH.parent
ROOT = PRODUCT_PATH.parents[3]
if ROOT.resolve() != ROOT:
    raise ValueError("pc386 source root must not traverse a symlink")
for relative in ("cores/fes-pc386/tools/build_pc386_product.py",
                 "cores/fes-pc386/tools/build_pc386_with_lockstep.py",
                 "cores/fes-pc386/tools/pc386_run_lockstep.py",
                 "cores/fes-pc386/tools/pc386_execution.py",
                 "cores/fes-pc386/tools/pc386_owned_job.py",
                 "cores/fes-pc386/tools/pc386_seal_evidence.py"):
    current = ROOT
    parts = Path(relative).parts
    for index, part in enumerate(parts):
        current = current / part
        if current.is_symlink() or (not current.is_file() if index == len(parts) - 1 else not current.is_dir()):
            raise ValueError("pc386 producer modules must be regular, non-symlinked source files")
scripts_dir = ROOT / 'scripts'
if scripts_dir.is_symlink() or not scripts_dir.is_dir():
    raise ValueError("pc386 shared source directory must not be linked")
for path in scripts_dir.glob('*.py'):
    if path.is_symlink() or not path.is_file():
        raise ValueError("pc386 imported script source must be a regular file")
for directory in (str(ROOT), str(TOOLS)):
    if directory not in sys.path:
        sys.path.insert(0, directory)

from scripts import pc386_quartus as machine
from scripts.pc386_quartus import *
from scripts import fes_build_common as board
from scripts import lockfile
from scripts import source_closure
from scripts import yosys_netlist
from scripts.core_package import encode_manifest
from scripts.export_core_package import build_identity, encode_build_record, export_package
from scripts.input_identity import DESIGN_V2, with_identities, manifest_toolchain
from scripts.functional_execution import execution_digest, execution_environment as _base_execution_environment
from scripts.export_core_package import source_input_closure
import pc386_execution
import pc386_owned_job

# A caller may already have imported a different checkout's scripts package.
# Reject that process state rather than record one tree while executing another.
for name, module in tuple(sys.modules.items()):
    if name.startswith('scripts.') and getattr(module, '__file__', None):
        if not Path(module.__file__).resolve().is_relative_to(scripts_dir):
            raise ValueError('pc386 imported helper belongs to a different checkout: ' + name)
for module, filename in ((machine, scripts_dir / 'pc386_quartus.py'),
                         (pc386_execution, TOOLS / 'pc386_execution.py'),
                         (pc386_owned_job, TOOLS / 'pc386_owned_job.py')):
    if Path(module.__file__).absolute() != filename:
        raise ValueError('pc386 imported implementation differs from the selected source')

RECIPE = "cores/fes-pc386/tools/build_pc386_product.py"
LOCK = machine.QUARTUS_LOCK
TOOLCHAIN_LOCK = machine.LOCK
_authenticate_tools = machine._authenticate_tools
_check_seed = machine._check_seed
check_lockstep_report = machine.check_lockstep_report
PINNED_INPUTS = tuple(sorted(set(machine.PINNED_INPUTS) | {
    RECIPE,
    "cores/fes-pc386/tools/build_pc386_with_lockstep.py",
    "cores/fes-pc386/tools/pc386_run_lockstep.py",
    "cores/fes-pc386/tools/pc386_execution.py",
    "cores/fes-pc386/tools/pc386_owned_job.py",
    "cores/fes-pc386/tools/pc386_seal_evidence.py",
    "scripts/functional_execution.py", "scripts/input_identity.py",
    "scripts/export_core_package.py", "scripts/core_package.py", "scripts/run_logged.sh",
}))

def script_imports(root: Path) -> set[str]:
    """Every scripts module this producer imports, with what each imports in turn."""
    closure = set()
    for node in ast.walk(ast.parse((root / RECIPE).read_text(encoding="utf-8"), filename=RECIPE)):
        if isinstance(node, ast.ImportFrom) and node.module == "scripts":
            names = [alias.name for alias in node.names]
        elif isinstance(node, ast.ImportFrom) and node.module and node.module.startswith("scripts."):
            names = [node.module.split(".")[1]]
        elif isinstance(node, ast.Import):
            names = [alias.name.split(".")[1] for alias in node.names if alias.name.startswith("scripts.")]
        else:
            continue
        for name in names:
            closure |= source_closure.imports(name, root)
    return closure


def require_running_root(root: Path) -> Path:
    root = Path(root).absolute()
    if root.resolve() != root or root != ROOT:
        raise board.BuildError("pc386 product root differs from the running producer checkout")
    return root


class ProductTools(dict):
    """Authenticated tools and the slot that supplied Yosys."""
    def __init__(self, tools, router):
        super().__init__(tools)
        self.gpu_router = router


def _slot_directory(root: Path, cache_root: Path, router: str) -> Path:
    from scripts import toolchain_cache
    _backend, architectures, configuration = gpu_choice(router)
    cuda_architectures = board._cuda_architectures_from_configuration(configuration)
    try:
        request = toolchain_cache.request_from_environment(
            root, root / TOOLCHAIN_LOCK, cache_root=cache_root, gpu_router=router,
            hip_architectures=architectures, cuda_architectures=cuda_architectures)
        slot = toolchain_cache.install_directory(request, "nextpnr")
        toolchain_cache._check_no_symlink_components(slot, allow_missing=True)
        return slot
    except toolchain_cache.CacheError as exc:
        raise board.BuildError(f"cannot identify {router} toolchain slot: {exc}") from exc


def authenticate_product_tools(root: Path, *, cache_root: Path | None = None,
                               gpu_router: str | None = None) -> ProductTools:
    # Select only an entirely absent GPU-specific install as unavailable. A
    # present partial/corrupt slot must fail authentication, not fall through.
    root = require_running_root(root)
    if gpu_router is None and cache_root is None:
        raise board.BuildError("select --gpu-router when using a local pc386 toolchain")
    routers = ((gpu_router,) if gpu_router is not None else
               tuple(router for router, compiler in (("HIP", "hipcc"), ("CUDA", "nvcc"))
                     if shutil.which(compiler) is not None))
    for router in routers:
        if gpu_router is None:
            slot = _slot_directory(Path(root), Path(cache_root), router)
            try:
                mode = slot.lstat().st_mode
            except FileNotFoundError:
                continue
            if not stat.S_ISDIR(mode):
                raise board.BuildError(f"{router} toolchain slot is not a real directory")
        tools = _authenticate_tools(root, cache_root=cache_root, gpu_router=router)
        return ProductTools({"yosys": tools["yosys"],
                             "quartus": board.AuthenticatedTool(tools["quartus_sh"],
                                                                tools["identities"]["quartus_sh"])}, router)
    raise board.BuildError("no installed pc386 HIP or CUDA toolchain slot")


def selection_identities():
    return {"yosys": "selection-only", "quartus": "selection-only"}


def record_tool_options(tools):
    return {"gpu_router": tools.gpu_router}


def execution_environment(home: Path, paths: dict[str, Path]) -> dict[str, str]:
    """Use and capture the same host probe inputs when tools reauthenticate."""
    env = _base_execution_environment(home, paths)
    # Toolchain cache keys include compiler probes. Preserve the selected host
    # search path and accelerator selectors, then capture and recheck them as
    # actual execution inputs; never invent a different backend identity.
    env["PATH"] = os.environ.get("PATH", "/usr/bin:/bin")
    for name in ("ROCM_PATH", "HIPCC", "CUDA_HOME", "CUDACXX", "CUDA_PATH"):
        if os.environ.get(name):
            env[name] = os.environ[name]
    return env


def execution_inputs(paths, env, device=0):
    controlled = dict(env, QUARTUS_ROOTDIR=str(paths["quartus"].parent.parent))
    return pc386_execution.execution_inputs(paths, controlled, device)


def selected_recipe(root: Path) -> dict:
    data = tomllib.loads((Path(root).parents[1] / "config/core-recipes.toml").read_text())
    matches = [r for r in data["recipes"] if r["core_id"] == "fes.pc386"
               and r.get("board", "de10-nano") == "de10-nano"]
    if len(matches) != 1:
        raise board.BuildError("pc386 must have one product recipe")
    entry = matches[0]
    if (entry.get("producer_script") != RECIPE or entry.get("toolchain") != "quartus"
            or entry.get("front_end") != "yosys"):
        raise board.BuildError("pc386 product recipe must select its Yosys/Quartus sealer")
    q = entry["quartus"]
    lock = quartus_lock(root)
    if (q["version"], q["edition"], q["device_family"]) != (lock.version, lock.edition, lock.device_family):
        raise board.BuildError("pc386 product recipe differs from its Quartus lock")
    if len(q["seeds"]) != 1:
        raise board.BuildError("pc386 product recipe must select its single sealed seed")
    return entry


def _require_clean_source(root: Path, *, identity_version: int = 2):
    if identity_version != 2:
        raise board.BuildError("pc386 product requires functional identity version 2")
    return board._require_clean_source(require_running_root(root), pinned_inputs=PINNED_INPUTS)


def create_build_record(root: Path, repository: str, revision: str, identities: dict[str, str],
                        seed: int = 1, gpu_router: str = "HIP", *, identity_version: int = 2,
                        execution=None) -> bytes:
    if identity_version != 2 or execution is None or execution.get("toolchain") != "quartus":
        raise board.BuildError("pc386 requires controlled format-2 Quartus execution")
    root = require_running_root(root)
    top = Path(subprocess.check_output(["git", "rev-parse", "--show-toplevel"], cwd=root, text=True).strip())
    q = selected_recipe(root)["quartus"]
    if q["seeds"] != [_check_seed(seed)]:
        raise board.BuildError("selected pc386 seed differs from its recipe")
    backend, _, configuration = gpu_choice(gpu_router)
    from scripts.source_closure import TOOLCHAIN_RECIPE_FILES
    paths = sorted(set(PINNED_INPUTS) | script_imports(root)
                   | {"scripts/source_closure.py", *TOOLCHAIN_RECIPE_FILES})
    inputs = source_input_closure(root, paths)
    qsf = project_qsf(root, root / OUTPUT / "project", "0" * 32, seed, threads=q["threads"])
    recipe_choices = {key: value for key, value in q.items() if key not in ("version", "threads")}
    params = {
        "core_id": "fes.pc386", "device": board.TARGET, "top": board.TOP, "seed": seed,
        "gpu_backend": backend, "toolchain_configuration": configuration,
        "compiler": quartus_lock(root).compiler, "quartus_version": q["version"], "quartus_threads": q["threads"],
        "quartus_recipe_sha256": hashlib.sha256(json.dumps(recipe_choices, sort_keys=True).encode()).hexdigest(),
        "execution_sha256": execution_digest(execution), "front_end": "yosys", "device_family": q["device_family"],
        "front_end_program_sha256": hashlib.sha256(machine_program(Path(MACHINE_NETLIST), "$MACHINE_SOURCES").encode()).hexdigest(),
        "quartus_qsf_sha256": hashlib.sha256(qsf.encode()).hexdigest(),
        "system_clock_hz": SYSTEM_CLOCK_HZ, "pixel_clock_hz": 74_250_000, "audio_clock_hz": 12_288_000,
        "reference_clock_hz": 50_000_000, "audio_sample_hz": 48_000,
        "guest_rates": json.dumps(guest_rates(), sort_keys=True, separators=(",", ":")),
        # design-v2 binds the firmware by its output: the images and the ROM hex
        # Yosys reads, as firmware.lock pins them (#1984 F2).
        "firmware": json.dumps({**firmware_digests(root), "rom_hex": pinned_rom_digest(root)},
                               sort_keys=True, separators=(",", ":")),
        "machine_sources_sha256": hashlib.sha256("\n".join(MACHINE_SOURCES).encode()).hexdigest(),
    }
    if execution.get("selection_only"):
        params["selection_only"] = True
    fields = {"format": 2, "toolchain": "quartus", "repository": repository, "revision": revision,
              "source_path": root.relative_to(top).as_posix(), "source_roots": paths, "source_inputs": inputs,
              "recipe": RECIPE, "recipe_sha256": inputs[RECIPE], "abi_definition": ABI_DEFINITION,
              "abi_definition_sha256": inputs[ABI_DEFINITION], "dependencies": {}, "tools": dict(identities),
              "parameters": params}
    return encode_build_record(with_identities(fields, root=root, policy=DESIGN_V2))


def run_quartus(quartus_sh: Path, quartus_root: Path, project: Path, log: Path) -> None:
    env = os.environ.copy()
    env["QUARTUS_ROOTDIR"] = str(quartus_root)
    try:
        status = pc386_owned_job.run(
            [str(ROOT / "scripts" / "run_logged.sh"), str(log), str(quartus_sh), "--flow", "compile", board.TOP],
            cwd=project, env=env, timeout=3 * 60 * 60, label="pc386 Quartus")
    except TimeoutError as exc:
        raise board.BuildError(f"Quartus compile exceeded its time cap; log: {log}") from exc
    if status != 0:
        raise board.BuildError(f"Quartus compile failed (exit {status}); log: {log}")


def manifest(root: Path, record: bytes, evidence: dict, repository: str, revision: str,
             identities: dict[str, str]) -> bytes:
    core = dict(CORE)
    core["name"] = f"{CORE['name']} (Quartus)"
    core["description"] = f"Quartus bring-up build. {CORE['description']}"
    return encode_manifest({
        "format": 2,
        "core": core,
        "target": {"platform": "de10_nano", "device": board.TARGET, "programming_profile": "fes-gp-v1"},
        "payload": {"file": "core.rbf", **evidence["rbf"]},
        "abi": {"id": "fes.computer", "major": 1, "minor": 1},
        "interfaces": [{"id": name, "major": 1, "minor": 0, "required": True} for name in INTERFACES],
        "build": {"id": build_identity(record), "repository": repository, "revision": revision,
                  "recipe_sha256": json.loads(record)["recipe_sha256"],
                  "toolchain": manifest_toolchain(record)},
    })


def check_lockstep_metadata(root: Path, data: bytes, yosys: board.AuthenticatedTool,
                            gpu_router: str, *, expected_oracle: dict | None = None,
                            linked_names: tuple[str, ...] | None = None) -> dict:
    """Validate source and tool binding in a full default lockstep report.

    Fresh seals additionally regenerate the CPU netlist in
    check_lockstep_report. This metadata check alone is not a seal.
    """
    try:
        report = json.loads(data)
    except ValueError as exc:
        raise board.BuildError(f"lockstep report is not JSON: {exc}") from exc
    run = report.get("run") if isinstance(report, dict) else None
    if not isinstance(run, dict) or run.get("passed") is not True:
        raise board.BuildError("lockstep report did not pass")
    if run.get("complete_corpus") is not True:
        raise board.BuildError("lockstep report does not prove a complete corpus; use the current lockstep runner")
    if run.get("default_suite") is not True:
        raise board.BuildError(f"lockstep report is not the default suite: {run.get('arguments')}")
    netlist = report.get("netlist")
    if not isinstance(netlist, dict):
        raise board.BuildError("lockstep report is not a --netlist run")
    expected = {"program_sha256": yosys_netlist.program_sha256(yosys_netlist.SEAL),
                "cpu_source_sha256": yosys_netlist.cpu_source_sha256(root),
                "yosys": yosys_netlist.lock_record(root, yosys, gpu_router, TOOLCHAIN_LOCK)}
    for key, value in expected.items():
        if netlist.get(key) != value:
            raise board.BuildError(f"lockstep report's netlist {key} is {netlist.get(key)!r}, this build's is {value!r}")
    verilator = report.get("verilator")
    try:
        pinned = lockfile.load_lock(root / I386_LOCK)["verilator"].commit
    except (OSError, lockfile.LockfileError, KeyError, ValueError) as exc:
        raise board.BuildError(f"cannot read {I386_LOCK}: {exc}") from exc
    if not isinstance(verilator, dict) or verilator.get("lock") != I386_LOCK or verilator.get("commit") != pinned:
        raise board.BuildError(f"lockstep report's Verilator is not {I386_LOCK}'s {pinned}: {verilator!r}")
    if expected_oracle is None:
        expected_oracle = oracle_identity(root)
    measured_oracle = report.get("oracle")
    if (not isinstance(measured_oracle, dict)
            or set(measured_oracle) != set(expected_oracle) | {"linked_sha256", "testbench_sha256"}
            or any(measured_oracle.get(key) != value for key, value in expected_oracle.items())):
        raise board.BuildError(f"lockstep report's oracle identity {measured_oracle!r} is not {expected_oracle!r}")
    linked = measured_oracle["linked_sha256"]
    if (not isinstance(linked, dict) or set(linked) != set(linked_names or LINKED_NAMES)
            or not all(valid_sha256(value) for value in linked.values())
            or not valid_sha256(measured_oracle["testbench_sha256"])):
        raise board.BuildError("lockstep report's oracle binary digests are missing or invalid")
    digest = netlist.get("netlist_sha256")
    if not valid_sha256(digest):
        raise board.BuildError("lockstep report's CPU netlist digest is invalid")
    return {"report_sha256": hashlib.sha256(data).hexdigest(), "netlist_sha256": digest,
            "netlist_program_sha256": expected["program_sha256"], "yosys_tool": yosys.identity,
            "cpu_source_closure_sha256": expected["cpu_source_sha256"], "simulator": verilator,
            "oracle": measured_oracle}


def build(root: Path = ROOT, package_store=None, *, cache_root: Path | None = None, seed: int = 1,
          gpu_router: str | None = None, lockstep_report: Path | None = None,
          identity_version: int = 2) -> Path:
    root = require_running_root(root)
    _check_seed(seed)
    if lockstep_report is None:
        raise board.BuildError("fes.pc386 needs --lockstep-report (scripts/test_fes_i386_lockstep.py --netlist)")
    try:
        lockstep_data = Path(lockstep_report).read_bytes()
    except OSError as exc:
        raise board.BuildError(f"cannot read the lockstep report: {exc}") from exc
    root = Path(root).resolve()
    package_store = root / "build/packages" if package_store is None else Path(package_store).resolve()
    if package_store != root / "build/packages":
        raise board.BuildError("fes.pc386 package store must be build/packages")
    repository, revision = _require_clean_source(root, identity_version=identity_version)
    tools = authenticate_product_tools(root, cache_root=cache_root, gpu_router=gpu_router)
    router = tools.gpu_router
    paths = {name: tool.path for name, tool in tools.items()}
    identities = {name: tool.identity for name, tool in tools.items()}
    with tempfile.TemporaryDirectory(prefix="fes-pc386-quartus-home-") as home:
        env = execution_environment(Path(home), paths)
        env["QUARTUS_ROOTDIR"] = str(paths["quartus"].parent.parent)
        execution = execution_inputs(paths, env)
        record = create_build_record(root, repository, revision, identities, seed, router,
                                     identity_version=identity_version, execution=execution)
        build_id = build_identity(record)
        output = None
        original_env = dict(os.environ)
        try:
            os.environ.clear()
            os.environ.update(env)
            output = board._prepare_output(root, relative=OUTPUT, build_outputs=BUILD_OUTPUTS)
            board._write_atomic(output / "build-inputs.json", record)
            lockstep = check_lockstep_report(root, lockstep_data, tools["yosys"], router, output)
            board._write_atomic(output / LOCKSTEP_REPORT, lockstep_data)
            threads = selected_recipe(root)["quartus"]["threads"]
            project = write_project(root, output, build_id, seed, threads=threads)
            actual_qsf = (project / "top.qsf").read_text(encoding="utf-8").replace(build_id, "0" * 32)
            if hashlib.sha256(actual_qsf.encode()).hexdigest() != json.loads(record)["parameters"]["quartus_qsf_sha256"]:
                raise board.BuildError("Quartus project differs from the canonical record")
            # build_pc386_with_lockstep runs the firmware recipe first, as its
            # own process; the seal checks its output against the pin.
            rom = check_rom(root)
            elaborate_machine(root, output, project, tools["yosys"].path)
            check_rom(root)
            run_quartus(paths["quartus"], paths["quartus"].parent.parent, project, output / "quartus.log")
            board._write_atomic(output / "quartus-version.log", (quartus_lock(root).version + "\n").encode())
            evidence = validate_build_evidence(output, project)
            evidence.update({"build_id": build_id, "device": board.TARGET, "top": board.TOP, "seed": seed,
                             "gpu_backend": gpu_choice(router)[0],
                             "firmware": {**firmware_digests(root), "rom_hex": board._sha256(rom)},
                             "inputs": {p: board._sha256(root / p) for p in sorted(PINNED_INPUTS)},
                             "tools": identities, "toolchain": manifest_toolchain(record),
                             "netlist": {"program_sha256": yosys_netlist.program_sha256(yosys_netlist.SEAL),
                                         "cpu_source_sha256": yosys_netlist.cpu_source_sha256(root),
                                         "machine_netlist_sha256": board._sha256(project / MACHINE_NETLIST),
                                         "yosys": yosys_netlist.lock_record(root, tools["yosys"], router, TOOLCHAIN_LOCK)},
                             "lockstep": lockstep})
            board._write_atomic(output / "build-summary.json",
                                (json.dumps(evidence, indent=2, sort_keys=True) + "\n").encode())
            encoded = manifest(root, record, evidence, repository, revision, identities)
            board._write_atomic(output / "manifest.toml", encoded)
            if authenticate_product_tools(root, cache_root=cache_root, gpu_router=router) != tools:
                raise board.BuildError("authenticated tool identity changed during build")
            if _require_clean_source(root, identity_version=identity_version) != (repository, revision):
                raise board.BuildError("source identity changed during build")
            require_same_oracle(root, lockstep["oracle"])
            if (create_build_record(root, repository, revision, identities, seed, router,
                                    identity_version=identity_version, execution=execution) != record
                    or execution_inputs(paths, env) != execution):
                raise board.BuildError("source or execution inputs changed during build")
            import pc386_seal_evidence
            if Path(pc386_seal_evidence.__file__).absolute() != TOOLS / 'pc386_seal_evidence.py':
                raise board.BuildError("pc386 evidence helper belongs to a different checkout")
            # The generic exporter publishes several paths. Keep its whole
            # operation private, then expose only the validated complete entry.
            with tempfile.TemporaryDirectory(prefix="pc386-export-", dir=root / "build") as private:
                package = export_package(encoded, output / "core.rbf", Path(private))
                original_record = package.with_name(package.name + "." + json.loads(record)["identities"]["record"]
                                                    + ".build-inputs.json")
                evidence_dir = pc386_seal_evidence.publish(root, package, original_record, output)
                return pc386_seal_evidence.publish_local(root, package_store, package, original_record, evidence_dir)
        except Exception:
            if output is not None:
                board._invalidate_failed_artifact(output)
            raise
        finally:
            os.environ.clear()
            os.environ.update(original_env)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--cache-root", type=Path)
    parser.add_argument("--package-output", type=Path)
    parser.add_argument("--identity-version", type=int, default=2)
    parser.add_argument("--seed", type=int, default=1, help="Quartus fitter seed; a timing miss fails")
    parser.add_argument("--gpu-router", choices=("HIP", "CUDA"),
                        help="select an authenticated toolchain slot; no GPU routing is used")
    mode = parser.add_mutually_exclusive_group(required=True)
    mode.add_argument("--lockstep-report", type=Path,
                      help="full report.json of scripts/test_fes_i386_lockstep.py --netlist on this tree")
    mode.add_argument("--with-lockstep", action="store_true",
                      help="run the core-owned fresh full lockstep and seal orchestration")
    args = parser.parse_args()
    try:
        if args.with_lockstep:
            root = require_running_root(args.root)
            tools = authenticate_product_tools(root, cache_root=args.cache_root, gpu_router=args.gpu_router)
            paths = {name: tool.path for name, tool in tools.items()}
            with tempfile.TemporaryDirectory(prefix="fes-pc386-lockstep-home-") as home:
                env = execution_environment(Path(home), paths)
                env["QUARTUS_ROOTDIR"] = str(paths["quartus"].parent.parent)
                command = [sys.executable, str(root / "cores/fes-pc386/tools/build_pc386_with_lockstep.py"),
                           "--root", str(root), "--gpu-router", tools.gpu_router, "--seed", str(args.seed),
                           "--download"]
                if args.cache_root is not None:
                    command.extend(("--cache-root", str(args.cache_root)))
                if args.package_output is not None:
                    command.extend(("--package-output", str(args.package_output)))
                if args.identity_version != 2:
                    raise board.BuildError("pc386 product requires functional identity version 2")
                return subprocess.run(command, cwd=root, env=env, check=False).returncode
        print(build(args.root, args.package_output, cache_root=args.cache_root, seed=args.seed,
                    gpu_router=args.gpu_router, lockstep_report=args.lockstep_report,
                    identity_version=args.identity_version))
    except (board.BuildError, OSError, ValueError) as exc:
        print(f"FES pc386 (Quartus): {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
