#!/usr/bin/env python3
"""Cross-build and exercise DE25 AArch64 userspace without accessing a board."""
from __future__ import annotations

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
UNITS = ("runtime_test", "protocol_test", "mmio_test", "fpga_manager_test",
         "off_t_test", "production_hardware_test", "de25_computer_test", "core_package_test")
GO_TESTS = (
    "TestClientRejectsWrongProtocolAndUnknownResponseFields",
    "TestClientRejectsInvalidDevelopmentRBFPathsBeforeDial",
    "TestRuntimeSocketOpenIsFalseWhenDialFails",
    "TestNativeHealthIsReadyOnlyForControllableProductionStatesAndKeepsLegacyBooleansFalse",
    "TestNativeReconcileMapsRebootRequiredToFailedUnavailable",
)


def builder_module():
    lane = ROOT / "image/de25-nano"
    # The existing image runner owns SDK/builder verification and snapshots.
    sys.path.insert(0, str(lane))
    spec = importlib.util.spec_from_file_location("de25_image_builder", lane / "build.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def checked(argv, **kwargs):
    print("+ " + " ".join(map(str, argv)), flush=True)
    return subprocess.run(list(map(str, argv)), check=True, **kwargs)


def environment():
    result = dict(os.environ)
    for key in ("PYTHON", "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "MAKEOVERRIDES", "GNUMAKEFLAGS",
                "GOARM", "GOARM64", "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOWORK"):
        result.pop(key, None)
    return result


def check_agent_results(log):
    text = log.read_text()
    for name in GO_TESTS:
        if "--- PASS: " + name + " " not in text:
            raise ValueError("AArch64 agent test did not run successfully: " + name)


def inside(jobs, revision):
    image = builder_module()
    record = json.loads(Path("/image/image.json").read_text())
    archive = Path("/image/aarch64-sdk.tar.gz")
    if image.digest(archive) != record["files"][archive.name]:
        raise ValueError("SDK hash differs from the image receipt")
    key = hashlib.sha256((revision + record["files"][archive.name]).encode()).hexdigest()
    work = Path("/build") / key
    work.mkdir(parents=True, exist_ok=True)
    sdk = work / "sdk"
    # Relocation changes the extracted SDK. Always start with authenticated bytes.
    if sdk.exists():
        shutil.rmtree(sdk)
    sdk.mkdir()
    checked(["tar", "-xzf", archive, "--strip-components=1", "-C", sdk])
    checked([sdk / "relocate-sdk.sh"])
    prefix = sdk / "bin/aarch64-buildroot-linux-gnu-"
    cxx = Path(str(prefix) + "g++")
    if not cxx.is_file():
        raise ValueError("DE25 SDK needs BR2_TOOLCHAIN_BUILDROOT_CXX=y; rebuild the image/SDK")
    compiled = work / "runtime"
    source = ROOT / "sources/libmister-runtime"
    command = ["make", "-C", source, f"-j{jobs}", f"BUILD_DIR={compiled}",
               f"CXX={cxx}", f"AR={prefix}ar", f"NM={prefix}nm", f"CXXFILT={prefix}c++filt",
               f"MISTER_RUNTIME_VERSION=git-{revision[:12]}"]
    checked([*command, "all", "archive-audit",
             *(compiled / "tests/unit" / name for name in UNITS)], env=environment())
    sysroot = subprocess.check_output([str(cxx), "-print-sysroot"], text=True).strip()
    # QEMU prefixes any existing absolute path, not just loader paths. The SDK
    # includes /tmp: using it wholesale redirects trusted-root openat while a
    # newly created /tmp/package path falls back to the host filesystem.
    # Expose only runtime libraries, including this SDK's lib64 aliases.
    qemu_root = work / "qemu-libs"
    if qemu_root.exists():
        shutil.rmtree(qemu_root)
    (qemu_root / "usr").mkdir(parents=True)
    for name in ("lib", "usr/lib"):
        (qemu_root / name).symlink_to(Path(sysroot) / name, target_is_directory=True)
    (qemu_root / "lib64").symlink_to("lib", target_is_directory=True)
    (qemu_root / "usr/lib64").symlink_to("lib", target_is_directory=True)
    qemu = ["qemu-aarch64-static", "-cpu", "cortex-a53", "-L", str(qemu_root)]
    checks = []
    export = Path("/export")
    for name in UNITS:
        binary = compiled / "tests/unit" / name
        image.elf_aarch64(binary.read_bytes()[:64])
        with (export / (name + ".log")).open("w") as log:
            checked([*qemu, binary], cwd=source, stdout=log, stderr=subprocess.STDOUT, timeout=120)
        checks.append({"name": name, "result": "passed", "sha256": image.digest(binary)})
    for name in ("mister-runtime", "libmister-runtime.a"):
        shutil.copy2(compiled / name, export / name)
    image.elf_aarch64((export / "mister-runtime").read_bytes()[:64])
    agent = export / "mister-agent-linux-arm64"
    image.elf_aarch64(agent.read_bytes()[:64])
    with (export / "agent-cli.log").open("w") as log:
        result = checked(["qemu-aarch64-static", "-cpu", "cortex-a53", agent,
                          "--print-maintenance-contract"],
                         stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=30)
        log.write(result.stdout + result.stderr)
        if result.stdout.strip() != "fes-kit-maintenance-v1":
            raise ValueError("AArch64 agent command did not return its maintenance contract")
    with (export / "agent-runtime-tests.log").open("w") as log:
        checked(["qemu-aarch64-static", "-cpu", "cortex-a53", export / "agent-runtime-tests", "-test.v",
                 "-test.run", "^(" + "|".join(GO_TESTS) + ")$", "-test.timeout=30s"],
                cwd=ROOT / "sources/FogCast/internal/misterruntime",
                stdout=log, stderr=subprocess.STDOUT, timeout=60)
    check_agent_results(export / "agent-runtime-tests.log")
    (export / "checks.json").write_text(json.dumps({"runtime_tests": checks,
                                                   "agent_tests": list(GO_TESTS),
                                                   "runtime_build_key": key,
                                                   "qemu_library_prefix": "libraries-only-with-lib64",
                                                   "compiler_version": subprocess.check_output(
                                                       [str(cxx), "--version"], text=True).splitlines()[0]},
                                                  indent=2) + "\n")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image-output", type=Path, default=ROOT / "out/de25-nano-linux")
    parser.add_argument("--output", type=Path, default=ROOT / "out/de25-runtime")
    parser.add_argument("--jobs", type=int, default=2)
    parser.add_argument("--container-runtime", default="docker")
    parser.add_argument("--inside", metavar="REVISION", help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    if not 1 <= args.jobs <= 64:
        parser.error("--jobs must be between 1 and 64")
    if args.inside:
        if str(ROOT) != "/src" or not re.fullmatch(r"[a-f0-9]{40}", args.inside):
            parser.error("internal action requires the isolated DE25 builder")
        inside(args.jobs, args.inside)
        return 0
    image = builder_module()
    output, image_output = args.output.resolve(), args.image_output.resolve()
    if output == image_output or output in image_output.parents or image_output in output.parents:
        parser.error("runtime and image outputs must be separate directories")
    try:
        output.mkdir(parents=True, exist_ok=True)
        revision, source = image.snapshot()
        with image.output_lock(image_output), image.output_lock(output):
            image.validate_output(output)
            (output / "runtime.json").unlink(missing_ok=True)
            (output / "checks.json").unlink(missing_ok=True)
            record = image.verify_receipt(image_output)
            volume = "fes-de25-runtime-" + hashlib.sha256(str(output).encode()).hexdigest()[:16]
            container_image = image.ensure_builder(source, args.container_runtime, volume)
            if container_image != record["builder_image"]:
                raise ValueError("runtime builder differs from the SDK producer")
            env = environment()
            env.update(CGO_ENABLED="0", GOOS="linux", GOARCH="arm64", GOARM64="v8.0",
                       GOMAXPROCS=str(args.jobs))
            fogcast = source / "sources/FogCast"
            for label, command in (
                ("agent-build", ["go", "build", "-p", args.jobs, "-buildvcs=false", "-trimpath",
                                 "-o", output / "mister-agent-linux-arm64", "./cmd/mister-agent"]),
                ("agent-test-build", ["go", "test", "-p", args.jobs, "-c", "-buildvcs=false",
                                      "-trimpath", "-o", output / "agent-runtime-tests",
                                      "./internal/misterruntime"]),
            ):
                with (output / (label + ".log")).open("w") as log:
                    checked(command, cwd=fogcast, env=env, stdout=log, stderr=subprocess.STDOUT)
            checked([args.container_runtime, "run", "--rm", "--platform", "linux/amd64",
                     "--network", "none", "--user", f"{os.getuid()}:{os.getgid()}",
                     "--ulimit", "core=0:0", "--ulimit", "nofile=65536:65536",
                     "--volume", f"{source}:/src:ro", "--volume", f"{volume}:/build",
                     "--volume", f"{image_output}:/image:ro", "--volume", f"{output}:/export",
                     "--env", "LC_ALL=C", "--env", "TZ=UTC", container_image,
                     "python3", "/src/scripts/de25_runtime.py", "--inside", revision,
                     "--jobs", args.jobs])
            checks = json.loads((output / "checks.json").read_text())
            report = {"format": 1, "board": "de25-nano", "architecture": "aarch64",
                      "classification": "userspace-only", "fes_revision": revision,
                      "go_target": "linux/arm64 GOARM64=v8.0 CGO_ENABLED=0",
                      "qemu_cpu": "cortex-a53",
                      "image_fes_revision": record["fes_revision"], "builder_image": container_image,
                      "build_volume": volume,
                      "sdk_sha256": record["files"]["aarch64-sdk.tar.gz"],
                      "runtime_source_tree": subprocess.check_output(
                          ["git", "rev-parse", revision + ":sources/libmister-runtime"],
                          cwd=ROOT, text=True).strip(),
                      "go_version": subprocess.check_output(["go", "version"], cwd=fogcast, text=True).strip(),
                      "checks": checks, "hardware_acceptance": "not-run",
                      "files": {path.name: image.digest(path) for path in sorted(output.iterdir())
                                if path.is_file() and path.name != "runtime.json"}}
            (output / "runtime.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
            print(json.dumps({"status": "passed", "output": str(output),
                              "classification": "userspace-only", "hardware_acceptance": "not-run"}))
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
        parser.exit(1, "de25-runtime: " + str(error) + "\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
