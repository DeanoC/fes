#!/usr/bin/env python3
"""Stage pinned MiSTer framework plus local Pong sources; optionally run Quartus."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import sys
import tarfile
import tomllib

sys.path.insert(0, str(Path(__file__).resolve().parent))
from rebuild_core import locate_quartus, quartus_version_line, RebuildError

ROOT = Path(__file__).resolve().parents[1]
LOCAL_SOURCES = (
    "cores/pong/Pong.sv", "cores/pong/rtl/pong_game.sv", "cores/pong/rtl/pong_video.sv",
    "cores/pong/framework.toml", "scripts/build_pong.py", "scripts/rebuild_core.py",
    "scripts/run_logged.sh",
)


def sha(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def git(framework, *args):
    return subprocess.check_output(["git", "-C", str(framework), *args])


def stage(root, framework, pin):
    root, framework = Path(root), Path(framework)
    if git(framework, "rev-parse", "HEAD").decode().strip() != pin["commit"]:
        raise ValueError("framework revision differs from pin")
    if git(framework, "status", "--porcelain", "--untracked-files=all").strip():
        raise ValueError("framework checkout is dirty")
    sources = {}
    for name in LOCAL_SOURCES:
        path = root / name
        if path.is_symlink() or any(p.is_symlink() for p in path.parents if p != root):
            raise ValueError(f"local source is a symlink: {name}")
        sources[name] = sha(path)
    project = root / "build/rebuild/pong/project"
    if any(p.is_symlink() for p in (project, *project.parents)):
        raise ValueError("build path must not contain a symlink")
    archive = git(framework, "archive", "--format=tar", pin["commit"])
    with tarfile.open(fileobj=io.BytesIO(archive)) as tree:
        members = tree.getmembers()
        for member in members:
            name = PurePosixPath(member.name)
            if name.is_absolute() or ".." in name.parts or not (member.isfile() or member.isdir()):
                raise ValueError("framework archive contains unsupported path")
        if project.exists():
            shutil.rmtree(project)
        project.mkdir(parents=True)
        for member in members:
            destination = project / member.name
            if member.isdir():
                destination.mkdir(parents=True, exist_ok=True)
            else:
                destination.parent.mkdir(parents=True, exist_ok=True)
                destination.write_bytes(tree.extractfile(member).read())
    # Keep sys/ unchanged. Override the framework's wall-clock date hook in
    # the project settings, after sourcing its assignments.
    (project / "Pong.qpf").write_text('QUARTUS_VERSION = "17.0"\nPROJECT_REVISION = "Pong"\n')
    qsf = (project / "Template.qsf").read_text()
    (project / "Pong.qsf").write_text(qsf + '\nset_global_assignment -name PRE_FLOW_SCRIPT_FILE "quartus_sh:pong_build_id.tcl"\n')
    (project / "Pong.sdc").write_bytes((project / "Template.sdc").read_bytes())
    (project / "Pong.sv").write_bytes((root / "cores/pong/Pong.sv").read_bytes())
    for name in ("pong_game.sv", "pong_video.sv"):
        (project / "rtl" / name).write_bytes((root / "cores/pong/rtl" / name).read_bytes())
    (project / "files.qip").write_text(
        'set_global_assignment -name SDC_FILE Pong.sdc\n'
        'set_global_assignment -name SYSTEMVERILOG_FILE Pong.sv\n'
        'set_global_assignment -name SYSTEMVERILOG_FILE rtl/pong_game.sv\n'
        'set_global_assignment -name SYSTEMVERILOG_FILE rtl/pong_video.sv\n'
    )
    build_id = '`define BUILD_DATE "' + pin["build_date"] + '"\n'
    (project / "build_id.v").write_text(build_id)
    (project / "pong_build_id.tcl").write_text(
        'set output [open "build_id.v" w]\nputs -nonewline $output {' + build_id + '}\nclose $output\n'
    )
    staged_sources = {str(p.relative_to(project)): sha(p) for p in sorted(project.rglob("*")) if p.is_file()}
    manifest = {"format": 1, "system": "pong", "abi": "mister", "framework": pin,
                "sources": sources, "staged_sources": staged_sources}
    (project.parent / "inputs.json").write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--framework", type=Path, default=ROOT / "build/frameworks/template")
    parser.add_argument("--stage-only", action="store_true")
    args = parser.parse_args()
    pin = tomllib.loads((ROOT / "cores/pong/framework.toml").read_text())
    if not args.framework.exists():
        args.framework.parent.mkdir(parents=True, exist_ok=True)
        subprocess.run(["git", "clone", "--no-checkout", pin["repository"], str(args.framework)], check=True)
        subprocess.run(["git", "-C", str(args.framework), "checkout", "--detach", pin["commit"]], check=True)
    inputs = stage(ROOT, args.framework, pin)
    work = ROOT / "build/rebuild/pong"
    receipt = work / "build.json"
    receipt.unlink(missing_ok=True)
    if args.stage_only:
        print(work / "project")
        return
    quartus_root, quartus_sh = locate_quartus(os.environ.get("QUARTUS_ROOTDIR", ""), ROOT)
    version = quartus_version_line(quartus_sh)
    env = os.environ.copy()
    env["QUARTUS_ROOTDIR"] = str(quartus_root)
    env["QSYS_ROOTDIR"] = str(quartus_sh.parent.parent / "sopc_builder/bin")
    command = [str(quartus_sh), "--flow", "compile", "Pong"]
    subprocess.run([str(ROOT / "scripts/run_logged.sh"), str(work / "quartus.log"), *command],
                   cwd=work / "project", env=env, check=True)
    built = work / "project/output_files/Pong.rbf"
    if not built.is_file() or built.stat().st_size == 0:
        raise ValueError("Quartus did not produce a nonempty Pong.rbf")
    artifact = work / "pong.rbf"
    shutil.copyfile(built, artifact)
    result = {"inputs_sha256": sha(work / "inputs.json"), "framework": inputs["framework"],
              "artifact": "pong.rbf", "sha256": sha(artifact), "size": artifact.stat().st_size,
              "quartus_version": version, "command": command}
    receipt.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, subprocess.CalledProcessError, RebuildError) as exc:
        print(f"build-pong: {exc}", file=sys.stderr)
        sys.exit(1)
