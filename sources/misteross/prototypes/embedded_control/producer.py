#!/usr/bin/env python3
"""Build and check a simulation-only DE25 identity/drain producer prototype.

This produces a host executable and diagnostic receipt, never an RBF, FES
package, production seal or board image. Verification executes the local probe.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

if __package__ in (None, ""):
    sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from scripts.export_core_package import build_identity, encode_build_record
from scripts.source_repository import canonical_repository

ROOT = Path(__file__).resolve().parents[2]
FES = ROOT.parents[1]
RECIPE = "prototypes/embedded_control/producer.py"
CONTRACT = "cores/fes-common/generated/fes_embedded_control.vh"
RTL = "cores/fes-common/rtl/fes_embedded_control.sv"
TOP = "prototypes/embedded_control/embedded_producer_top.sv"
PROBE = "prototypes/embedded_control/probe.cpp"
INPUTS = tuple(sorted((RECIPE, CONTRACT, RTL, TOP, PROBE,
    "scripts/export_core_package.py", "scripts/core_package.py", "scripts/compiler_read_audit.py",
    "scripts/source_repository.py", "scripts/rfc3986_validator.py")))
SCHEMA = "fes.embedded-control-simulation.v1"
CLASS = "host-simulation-only"
ARTIFACT = "identity-probe"


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def source_hashes() -> dict[str, str]:
    result = {}
    for name in INPUTS:
        path = ROOT / name
        if path.is_symlink() or not path.is_file():
            raise ValueError(f"source must be a regular file: {name}")
        result[name] = sha(path)
    return result


def git(*args: str, check=True) -> subprocess.CompletedProcess:
    return subprocess.run(["git", "-C", str(ROOT), *args], text=True, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, check=check)


def source_state(allow_dirty: bool) -> tuple[str, str, str]:
    revision = git("rev-parse", "--verify", "HEAD").stdout.strip()
    repository = canonical_repository(git("remote", "get-url", "origin").stdout.strip())
    tracked = git("ls-files", "--error-unmatch", "--", *INPUTS, check=False).returncode == 0
    diff = git("diff", "--quiet", "HEAD", "--", *INPUTS, check=False).returncode
    if diff not in (0, 1):
        raise ValueError("cannot check source state")
    dirty = not tracked or diff == 1
    if dirty and not allow_dirty:
        raise ValueError("prototype inputs must be committed; use --allow-dirty for a labelled working-tree diagnostic")
    return repository, revision, "working-tree" if dirty else "committed"


def executable(name: str) -> Path:
    value = shutil.which(name)
    if value is None:
        raise ValueError(f"required simulation tool is missing: {name}")
    path = Path(value).resolve()
    if not path.is_file():
        raise ValueError(f"simulation tool is not a file: {name}")
    return path


def build_environment(paths: dict[str, Path]) -> dict[str, str]:
    return {"PATH": os.pathsep.join(dict.fromkeys([str(paths[k].parent) for k in ("verilator", "g++", "make")] + ["/usr/bin", "/bin"])), "LC_ALL": "C"}


def first_line(command: list[str], env: dict[str, str]) -> str:
    return subprocess.check_output(command, env=env, text=True, stderr=subprocess.STDOUT, timeout=15).splitlines()[0]


def support_hash(root: Path) -> str:
    include = root / "include"
    if not include.is_dir():
        raise ValueError("Verilator support include tree is missing")
    digest = hashlib.sha256()
    for path in sorted(include.rglob("*")):
        relative = path.relative_to(include).as_posix()
        if path.is_symlink():
            digest.update(f"link\0{relative}\0{os.readlink(path)}\n".encode())
        elif path.is_file():
            digest.update(f"file\0{relative}\0{sha(path)}\n".encode())
    return digest.hexdigest()


def tools_snapshot() -> tuple[dict[str, str], dict[str, Path]]:
    paths = {name: executable(name) for name in ("verilator", "verilator_bin", "g++", "make")}
    paths["python"] = Path(sys.executable).resolve()
    env = build_environment(paths)
    config = subprocess.check_output([str(paths["verilator"]), "-V"], env=env, text=True, timeout=15)
    root = next((line.split("=", 1)[1].strip() for line in config.splitlines()
                 if line.strip().startswith("VERILATOR_ROOT") and "=" in line and line.split("=", 1)[1].strip()), None)
    if root is None:
        raise ValueError("cannot identify Verilator support tree")
    versions = {name: first_line([str(path), "--version"], env) for name, path in paths.items()}
    if versions["verilator"] != versions["verilator_bin"]:
        raise ValueError("Verilator wrapper and compiler versions differ")
    names = {"g++": "cxx", "verilator_bin": "verilator-bin"}
    tools = {names.get(name, name): f"{path} | {versions[name]} | sha256:{sha(path)}" for name, path in paths.items()}
    tools["verilator-support"] = f"{root}/include | sha256:{support_hash(Path(root))}"
    return tools, paths


def record(source: dict[str, str], provenance: tuple[str, str, str], tools: dict[str, str], abi: int) -> bytes:
    repository, revision, state = provenance
    fields = {
        "format": 2, "repository": repository, "revision": revision, "source_path": "sources/misteross",
        "recipe": RECIPE, "recipe_sha256": source[RECIPE], "abi_definition": CONTRACT,
        "abi_definition_sha256": source[CONTRACT], "dependencies": {}, "tools": tools,
        "source_roots": list(INPUTS), "source_inputs": source,
        "parameters": {"top": "embedded_producer_top", "control_abi": abi, "simulation_only": True,
                       "source_state": state, "language": "systemverilog", "cpp_standard": "c++17",
                       "verilator_flags": "--cc --exe --build -Wall", "data_width": 128},
    }
    return encode_build_record(fields)


def canonical(value: object) -> bytes:
    return (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()


def regular(directory: Path, name: str) -> Path:
    path = directory / name
    if path.is_symlink() or not path.is_file():
        raise ValueError(f"prototype {name} is not a regular file")
    return path


def verify(directory: Path) -> dict:
    directory = Path(directory).absolute()
    if directory.is_symlink() or not directory.is_dir():
        raise ValueError("prototype output is not a regular directory")
    descriptor_bytes = regular(directory, "descriptor.json").read_bytes()
    descriptor = json.loads(descriptor_bytes)
    if not isinstance(descriptor, dict) or canonical(descriptor) != descriptor_bytes or set(descriptor) != {"schema", "classification", "build_id", "control_abi", "build_inputs_sha256", "artifact"}:
        raise ValueError("prototype descriptor is noncanonical or has unexpected fields")
    if descriptor["schema"] != SCHEMA or descriptor["classification"] != CLASS:
        raise ValueError("not a host-only simulation descriptor")
    inputs = regular(directory, "build-inputs.json").read_bytes()
    fields = json.loads(inputs)
    if encode_build_record(fields) != inputs or fields["format"] != 2:
        raise ValueError("build-input record is not canonical format 2")
    parameters = fields["parameters"]
    if (fields["recipe"] != RECIPE or fields["abi_definition"] != CONTRACT or
            fields["source_roots"] != list(INPUTS) or set(fields["source_inputs"]) != set(INPUTS) or
            set(parameters) != {"top", "control_abi", "simulation_only", "source_state", "language", "cpp_standard", "verilator_flags", "data_width"} or
            parameters.get("top") != "embedded_producer_top" or parameters.get("data_width") != 128 or
            parameters.get("simulation_only") is not True or parameters.get("source_state") not in ("committed", "working-tree")):
        raise ValueError("build-input record does not describe this simulation producer")
    if hashlib.sha256(inputs).hexdigest() != descriptor["build_inputs_sha256"] or build_identity(inputs) != descriptor["build_id"]:
        raise ValueError("descriptor and build-input identity differ")
    abi = descriptor["control_abi"]
    if type(abi) is not int or not 0 < abi <= 0xffffffff or fields["parameters"].get("control_abi") != abi:
        raise ValueError("control ABI differs from the build record")
    item = descriptor["artifact"]
    if not isinstance(item, dict) or set(item) != {"file", "sha256", "size", "type"} or item["file"] != ARTIFACT or item["type"] != "host-executable":
        raise ValueError("not the expected simulation artifact")
    artifact = regular(directory, ARTIFACT)
    if type(item["size"]) is not int or item["size"] <= 0 or artifact.stat().st_size != item["size"] or sha(artifact) != item["sha256"]:
        raise ValueError("simulation artifact differs from descriptor")
    # The host executable is a diagnostic output generated in this checkout;
    # hashes bind bytes but do not authenticate code from an untrusted source.
    result = subprocess.run([str(artifact), descriptor["build_id"], str(abi)], stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, text=True, timeout=30, check=False)
    if result.returncode:
        raise ValueError("live simulation rejected descriptor identity: " + result.stderr.strip())
    observed = json.loads(result.stdout)
    if observed != {"status": "passed", "build_id": descriptor["build_id"], "control_abi": abi, "drain": True}:
        raise ValueError("live simulation returned unexpected identity or drain evidence")
    return observed


def build(output: Path, abi: int, allow_dirty: bool) -> dict:
    output = Path(output).absolute()
    if ".." in output.parts or any(p.is_symlink() for p in (output, *output.parents)) or not output.is_relative_to(FES / "out") or output.exists():
        raise ValueError("output must be a fresh private directory beneath the FES worktree out/")
    if type(abi) is not int or not 0 < abi <= 0xffffffff:
        raise ValueError("an explicit nonzero 32-bit control ABI is required")
    provenance = source_state(allow_dirty)
    sources = source_hashes()
    tools, paths = tools_snapshot()
    inputs = record(sources, provenance, tools, abi)
    build_id = build_identity(inputs)
    if build_id == "0" * 32:
        raise ValueError("invalid empty build identity")
    output.mkdir(parents=True)
    (output / "build-inputs.json").write_bytes(inputs)
    obj = output / "obj"
    # Keep host compiler choices explicit and inherited make/compiler overrides
    # out of this diagnostic invocation. No GPU or FPGA tool is used.
    env = build_environment(paths)
    command = [str(paths["verilator"]), "--cc", "--exe", "--build", "--top-module", "embedded_producer_top", "-Wall",
               "--Mdir", str(obj), "-I" + str(ROOT / "cores/fes-common/generated"),
               "-GBUILD_ID=128'h" + build_id, "-GCONTROL_ABI=" + str(abi),
               "-CFLAGS", "-std=c++17 -O2", str(ROOT / RTL), str(ROOT / TOP), str(ROOT / PROBE)]
    with (output / "build.log").open("w") as log:
        subprocess.run(command, cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=180, check=True)
    artifact = output / ARTIFACT
    shutil.copy2(obj / "Vembedded_producer_top", artifact)
    if sources != source_hashes() or provenance != source_state(allow_dirty) or tools != tools_snapshot()[0]:
        raise ValueError("prototype inputs or tools changed while building")
    descriptor = {"schema": SCHEMA, "classification": CLASS, "build_id": build_id, "control_abi": abi,
                  "build_inputs_sha256": hashlib.sha256(inputs).hexdigest(),
                  "artifact": {"file": ARTIFACT, "type": "host-executable", "sha256": sha(artifact), "size": artifact.stat().st_size}}
    (output / "descriptor.json").write_bytes(canonical(descriptor))
    observed = verify(output)
    report = {"status": "passed", "classification": CLASS, "source_state": provenance[2],
              "revision": provenance[1], "build_id": build_id, "observed": observed,
              "command": command, "source_sha256": sources, "tools": tools,
              "descriptor_sha256": sha(output / "descriptor.json"), "artifact_sha256": sha(artifact),
              "limitations": ["host Verilator model, no RBF or production package", "no board routing, timing, DDR or hardware qualification", "diagnostic tool hashes are not production toolchain authentication"]}
    (output / "result.json").write_bytes(canonical(report))
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="action", required=True)
    create = sub.add_parser("build")
    create.add_argument("--output", required=True, type=Path)
    create.add_argument("--control-abi", required=True, type=lambda v: int(v, 0))
    create.add_argument("--allow-dirty", action="store_true")
    check = sub.add_parser("verify")
    check.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    try:
        result = build(args.output, args.control_abi, args.allow_dirty) if args.action == "build" else verify(args.output)
        print(json.dumps(result, sort_keys=True))
        return 0
    except (ValueError, OSError, subprocess.SubprocessError, json.JSONDecodeError) as error:
        print(f"embedded identity prototype: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
