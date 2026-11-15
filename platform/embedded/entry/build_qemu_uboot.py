#!/usr/bin/env python3
"""Build the pinned vendor U-Boot source for the QEMU virt machine only."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
REVISION = "d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413"


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--reference-repo", required=True, type=Path)
    parser.add_argument("--cross-prefix", required=True)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--jobs", type=int, default=8)
    args = parser.parse_args()
    repo, output = args.reference_repo.resolve(strict=True), args.output.absolute()
    if (".." in output.parts or any(p.is_symlink() for p in (output, *output.parents)) or
            not output.is_relative_to(ROOT / "out") or output.exists()):
        parser.error("output must be a fresh non-symlink directory beneath this worktree's out/")
    if not Path(args.cross_prefix).is_absolute() or not args.cross_prefix.endswith("-") or not 1 <= args.jobs <= 32:
        parser.error("cross-prefix must be absolute and end in -; jobs must be in 1..32")
    env = {k: os.environ[k] for k in ("PATH", "HOME", "LD_LIBRARY_PATH", "PYTHONPATH", "SWIG_LIB", "TMPDIR") if k in os.environ}
    env["LC_ALL"] = "C"
    def git(*command):
        return subprocess.check_output(["git", "-C", str(repo), *command], env=env).decode().strip()
    if git("rev-parse", "HEAD^{commit}") != REVISION or git("status", "--porcelain", "--untracked-files=all"):
        raise ValueError("vendor U-Boot must be a clean checkout of the pinned commit")
    env["SOURCE_DATE_EPOCH"] = git("show", "-s", "--format=%ct", "HEAD")
    make = Path(shutil.which("make", path=env.get("PATH")) or "").resolve()
    cross_gcc = Path(args.cross_prefix + "gcc").resolve()
    if not make.is_file() or not cross_gcc.is_file() or not os.access(cross_gcc, os.X_OK):
        raise ValueError("make and the selected cross compiler are required")
    version = subprocess.check_output([args.cross_prefix + "gcc", "-v"], stderr=subprocess.STDOUT, env=env, text=True)
    actual = re.findall(r"(?m)^COLLECT_GCC=(.+)$", version)
    if len(actual) != 1 or not Path(actual[0]).is_file():
        raise ValueError("cannot identify the underlying cross compiler")
    tools = {"make": {"path": str(make), "sha256": sha(make)},
             "cross_gcc": {"path": str(cross_gcc), "sha256": sha(cross_gcc)},
             "gcc_binary": {"path": str(Path(actual[0]).resolve()), "sha256": sha(Path(actual[0]))}}
    producer = sha(HERE / "build_qemu_uboot.py")
    output.mkdir(parents=True)
    build = output / "build"
    build.mkdir()
    commands = []
    def run(label, command):
        argv = [str(x) for x in command]
        commands.append(argv)
        with (output / (label + ".log")).open("wb") as log:
            subprocess.run(argv, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
    base = [make, "-C", repo, "O=" + str(build), "CROSS_COMPILE=" + args.cross_prefix]
    run("defconfig", [*base, "qemu_arm64_defconfig"])
    # This QEMU diagnostic never builds or uses EFI capsules; disabling their
    # host utility avoids an unrelated optional GnuTLS development dependency.
    run("config", [repo / "scripts/config", "--file", build / ".config", "-d", "TOOLS_MKEFICAPSULE"])
    run("olddefconfig", [*base, "olddefconfig"])
    config = (build / ".config").read_text()
    if ("CONFIG_CMD_BOOTI=y\n" not in config or "CONFIG_BOOTM_LINUX=y\n" not in config or
            "CONFIG_ARMV8_SWITCH_TO_EL1=y\n" in config or "CONFIG_TOOLS_MKEFICAPSULE=y\n" in config):
        raise ValueError("QEMU U-Boot configuration does not match the tested handoff")
    run("build", [*base, "-j", str(args.jobs)])
    binary = build / "u-boot.bin"
    if (not binary.is_file() or not binary.stat().st_size or
            git("rev-parse", "HEAD^{commit}") != REVISION or git("status", "--porcelain", "--untracked-files=all") or
            any(sha(Path(item["path"])) != item["sha256"] for item in tools.values()) or
            sha(HERE / "build_qemu_uboot.py") != producer):
        raise ValueError("U-Boot output missing or build inputs changed")
    result = {"status": "built", "classification": "pinned vendor source built for QEMU virt, not Nano firmware",
              "source_revision": REVISION, "source_tree": git("rev-parse", "HEAD^{tree}"),
              "producer_sha256": producer, "tools": tools, "compiler_version": version,
              "binary_sha256": sha(binary), "config_sha256": sha(build / ".config"),
              "commands": commands, "environment": env,
              "limitations": ["not the vendor Nano defconfig or Nano U-Boot binary",
                              "tool hashes and clean Git state are diagnostic receipts, not signatures"]}
    (output / "result.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    print("Built QEMU U-Boot:", binary)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        raise SystemExit("QEMU U-Boot build: " + str(error))
