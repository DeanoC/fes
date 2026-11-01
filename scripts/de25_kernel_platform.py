#!/usr/bin/env python3
"""Build the opt-in, unbooted DE25 permanent-owner kernel and base DTB."""
from __future__ import annotations

import argparse
import datetime
import difflib
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from scripts import de25_kernel_test as dep
PROFILE = "image/kernel/de25-platform"
DTB = "intel/socfpga_agilex5_de25_nano.dtb"
RELEASE = "6.12.11-fes-de25-owner"
ENABLED = ("FES_DE25_DMA", "FES_DE25_DMA_PLATFORM", "OF_OVERLAY", "OF_CONFIGFS",
           "CONFIGFS_FS", "FPGA", "FPGA_MGR_STRATIX10_SOC", "FPGA_BRIDGE",
           "FPGA_REGION", "OF_FPGA_REGION", "INTEL_STRATIX10_SERVICE", "ARM_SMMU_V3",
           "IOMMU_DEFAULT_DMA_STRICT", "DMA_CMA", "OF",
           "FTRACE", "TRACEPOINTS", "TRACING", "EVENT_TRACING")
DISABLED = ("LOCALVERSION_AUTO", "FES_DE25_DMA_KUNIT_TEST")


def write_json(path, data):
    Path(path).write_text(json.dumps(data, indent=2, sort_keys=True) + "\n")


def apply_profile(kernel, profile):
    kernel, profile = Path(kernel), Path(profile)
    record = json.loads((profile / "source.json").read_text())
    if record["format"] != 1 or record["kernel_revision"] != dep.selected_lock()["revision"]:
        raise ValueError("kernel profile revision differs from locked vendor source")
    patch = profile / "permanent-suppliers.patch"
    if dep.digest(patch) != record["patch_sha256"]:
        raise ValueError("supplier patch differs from its source record")
    for name, row in record["files"].items():
        path = kernel / name
        if path.is_symlink() or dep.digest(path) != row["before_sha256"]:
            raise ValueError("supplier input differs: " + name)
    for name, sha in record["unchanged_supplier_sources"].items():
        if dep.digest(kernel / name) != sha:
            raise ValueError("unchanged supplier source differs: " + name)
    dep.checked(["patch", "--batch", "--forward", "--fuzz=0", "-p1", "--input", patch.resolve()], cwd=kernel)
    for name, row in record["files"].items():
        if dep.digest(kernel / name) != row["after_sha256"]:
            raise ValueError("supplier result differs: " + name)
    return record


def integrate(kernel, source):
    record = apply_profile(kernel, source / PROFILE)
    target = kernel / dep.KERNEL_DRIVER
    shutil.copytree(source / dep.DRIVER, target)
    for name, text in {
        "drivers/misc/Kconfig": '\nsource "drivers/misc/fes-de25-dma/Kconfig"\n',
        "drivers/misc/Makefile": '\nobj-$(CONFIG_FES_DE25_DMA) += fes-de25-dma/\n',
    }.items():
        path = kernel / name
        if "fes-de25-dma" in path.read_text():
            raise ValueError("kernel already contains owner integration")
        path.write_text(path.read_text() + text)
    header = (source / dep.DRIVER / "generated/fes_de25_scanout.h").read_text()
    def constant(name):
        values = re.findall(r"^#define FES_DE25_SCANOUT_" + name + r" (0x[0-9a-f]+)U$", header, re.M)
        if len(values) != 1:
            raise ValueError("missing generated constant: " + name)
        return int(values[0], 16)
    address = constant("ARCHITECTURAL_LW_BASE") + constant("PERIPHERAL_OFFSET")
    tree = kernel / "arch/arm64/boot/dts/intel/socfpga_agilex5_de25_nano.dts"
    fragment = ("\n/* FES permanent owner candidate: no access until a known profile loads. */\n"
                "/ {\n\tfes_de25_scanout: dma-owner@%x {\n"
                '\t\tcompatible = "fes,de25-dma";\n'
                "\t\treg = <0 0x%x 0 0x%x>;\n"
                "\t\tiommus = <&smmu %d>;\n"
                "\t\tfes,fpga-region = <&{/fpga-region}>;\n\t};\n};\n") % (
                    address, address, constant("CONTROL_SPAN"), constant("SMMU_STREAM_ID"))
    tree.write_text(tree.read_text() + fragment)
    return record


def verify_config(text):
    for name in ENABLED:
        if "CONFIG_" + name + "=y\n" not in text:
            raise ValueError("required built-in setting missing: " + name)
    for name in DISABLED:
        # Invisible symbols can be absent when their dependency is disabled.
        # Any assigned value is refused; canonical false is absent or not-set.
        if re.search(r"^CONFIG_" + name + r"=", text, re.M):
            raise ValueError("forbidden configuration enabled: " + name)
    if 'CONFIG_LOCALVERSION="-fes-de25-owner"\n' not in text:
        raise ValueError("kernel release suffix differs")


def inside(jobs, revision):
    work, export = Path("/build"), Path("/export")
    kernel, build, sdk = work / "linux", work / "kernel-build", work / "sdk"
    dep.checked(["cp", "-a", "/kernel-input", kernel])
    profile = integrate(kernel, ROOT)
    # Compile the DTB verifier before the long kernel build. It links the exact
    # pinned libfdt implementation rather than inventing a second FDT parser.
    verifier = work / "verify-tree"
    libfdt = kernel / "scripts/dtc/libfdt"
    dep.checked(["gcc", "-std=gnu11", "-Wall", "-Wextra", "-I" + str(libfdt),
                 ROOT / PROFILE / "verify_tree.c", *sorted(libfdt.glob("*.c")), "-o", verifier])
    sdk.mkdir()
    dep.checked(["tar", "-xzf", "/image/aarch64-sdk.tar.gz", "--strip-components=1", "-C", sdk])
    dep.checked([sdk / "relocate-sdk.sh"])
    build.mkdir()
    vendor = export / "vendor.config"
    config = build / ".config"
    shutil.copyfile(vendor, config)
    change = [kernel / "scripts/config", "--file", config, "--set-str", "LOCALVERSION", "-fes-de25-owner"]
    for name in ENABLED:
        change.extend(("--enable", name))
    for name in DISABLED:
        change.extend(("--disable", name))
    dep.checked(change)
    env = dict(os.environ, LC_ALL="C", TZ="UTC", KBUILD_BUILD_USER="FES",
               KBUILD_BUILD_HOST="de25-owner", KBUILD_BUILD_VERSION="1",
               KBUILD_BUILD_TIMESTAMP="2026-10-31 00:00:00 UTC")
    prefix = sdk / "bin/aarch64-buildroot-linux-gnu-"
    command = ["make", "-C", kernel, "O=" + str(build), "ARCH=arm64",
               "CROSS_COMPILE=" + str(prefix), "LOCALVERSION=", "DTC_FLAGS=-@"]
    dep.checked([*command, "olddefconfig"], env=env)
    shutil.copyfile(config, export / "configured.config")
    verify_config(config.read_text())
    dep.checked([*command, "-j" + str(jobs), "Image", DTB], env=env)
    release = subprocess.check_output([*map(str, command), "-s", "kernelrelease"], env=env, text=True).strip().splitlines()[-1]
    if release != RELEASE:
        raise ValueError("built kernel release differs")
    dtb = build / "arch/arm64/boot/dts" / DTB
    dep.checked([verifier, dtb])
    symbols = (build / "System.map").read_text()
    for name in ("fes_de25_owner_probe", "fes_de25_dma_create", "s10_probe", "of_fpga_region_probe",
                 "simple_pm_bus_probe"):
        if not re.search(r" [tT] " + name + r"(?:\.[^\s]+)?$", symbols, re.M):
            raise ValueError("required built-in symbol missing: " + name)
    files = {"Image": build / "arch/arm64/boot/Image", "kernel.config": config,
             "System.map": build / "System.map", "vmlinux.symvers": build / "vmlinux.symvers",
             "de25-owner.dtb": dtb}
    result = {"format": 1, "classification": "unbooted opt-in permanent-owner kernel preparation",
              "fes_revision": revision, "kernel": dep.selected_lock(), "release": release,
              "suppliers": profile, "candidate_enabled": True, "normal_runtime_admission": "closed",
              "hardware_acceptance": "not-run", "modules": "FPGA/owner built-in; no vendor modules reused",
              "compiler": subprocess.check_output([str(prefix) + "gcc", "--version"], text=True).splitlines()[0], "files": {}}
    for name, path in files.items():
        shutil.copyfile(path, export / name)
        result["files"][name] = {"sha256": dep.digest(export / name), "size": path.stat().st_size}
    (export / "config.diff").write_text("".join(difflib.unified_diff(vendor.read_text().splitlines(True),
        config.read_text().splitlines(True), fromfile="locked-vendor.config", tofile="owner-candidate.config")))
    write_json(export / "kernel-build.json", result)
    print("DE25_PERMANENT_OWNER_KERNEL_BUILD_PASS", flush=True)


def run_container(argv, runtime, name, output, cap):
    if subprocess.run([runtime, "container", "inspect", name], stdout=subprocess.DEVNULL,
                      stderr=subprocess.DEVNULL).returncode == 0:
        raise ValueError("owned container name already exists; inspect the existing job")
    started = time.monotonic()
    child = subprocess.Popen(list(map(str, argv)))
    while True:
        code = child.poll()
        state = "running" if code is None else ("passed" if code == 0 else "failed")
        record = {"state": state, "container": name, "pid": child.pid, "time_cap_seconds": cap,
                  "elapsed_seconds": round(time.monotonic() - started, 1),
                  "heartbeat_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(), "returncode": code}
        write_json(output / "build-job.json", record)
        print(json.dumps(record), flush=True)
        if code is not None:
            if code:
                raise ValueError("candidate kernel build failed")
            return
        try:
            child.wait(timeout=min(30, max(.01, cap - (time.monotonic() - started))))
        except subprocess.TimeoutExpired:
            if time.monotonic() - started < cap:
                continue
            subprocess.run([runtime, "stop", "--time", "10", name], timeout=30, check=True)
            child.wait(timeout=30)
            record.update(state="timed-out", returncode=124)
            write_json(output / "build-job.json", record)
            raise ValueError("candidate kernel build reached its time cap")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cache", type=Path, required=False)
    parser.add_argument("--image-output", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--jobs", type=int, default=8)
    parser.add_argument("--time-cap", type=int, default=7200)
    parser.add_argument("--container-runtime", default="docker")
    parser.add_argument("--inside", help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    if not 1 <= args.jobs <= 16 or not 60 <= args.time_cap <= 7200:
        parser.error("jobs must be 1..16 and time cap 60..7200 seconds")
    try:
        if args.inside:
            if ROOT != Path("/src") or not re.fullmatch(r"[a-f0-9]{40}", args.inside):
                raise ValueError("internal build requires isolated committed source")
            inside(args.jobs, args.inside)
            return 0
        if not all((args.cache, args.image_output, args.output)):
            parser.error("--cache, --image-output and a new --output are required")
        lock = dep.selected_lock()
        cache = dep.fetch_kernel(args.cache, lock)
        lane = dep.image_builder()
        revision, source = lane.snapshot()
        output, image = args.output.resolve(), args.image_output.resolve()
        if output == image or output in image.parents or image in output.parents:
            raise ValueError("kernel output and image inputs must be separate")
        if shutil.disk_usage(output.parent).free < 30 << 30:
            raise ValueError("kernel build requires at least 30 GiB free")
        output.mkdir(parents=True, exist_ok=False)
        if dep.digest(image / "aarch64-sdk.tar.gz") != lock["sdk_sha256"]:
            raise ValueError("SDK differs from locked producer")
        vendor_image = image / "vendor-boot/Image"
        if dep.digest(vendor_image) != lock["vendor_image_sha256"]:
            raise ValueError("vendor Image differs from lock")
        config = dep.config_from_image(vendor_image.read_bytes())
        if hashlib.sha256(config).hexdigest() != lock["vendor_config_sha256"]:
            raise ValueError("vendor configuration differs from lock")
        (output / "vendor.config").write_bytes(config)
        kernel = output / "kernel-input"
        dep.extract_kernel(cache, lock["revision"], kernel)
        volume = "fes-de25-owner-kernel-" + hashlib.sha256(str(output).encode()).hexdigest()[:16]
        builder = lane.ensure_builder(source, args.container_runtime, volume)
        if builder != lock["builder_image"]:
            raise ValueError("builder differs from locked producer")
        write_json(output / "inputs.json", {"fes_revision": revision, "kernel": lock,
            "source_sha256": {str(p.relative_to(source)): dep.digest(p) for directory in (PROFILE, dep.DRIVER)
                              for p in (source / directory).rglob("*") if p.is_file()},
            "runner_sha256": dep.digest(source / "scripts/de25_kernel_platform.py")})
        name = volume
        command = [args.container_runtime, "run", "--rm", "--name", name, "--network", "none",
                   "--platform", "linux/amd64", "--user", f"{os.getuid()}:{os.getgid()}", "--ulimit", "core=0:0",
                   "--volume", f"{source}:/src:ro", "--volume", f"{image}:/image:ro",
                   "--volume", f"{kernel}:/kernel-input:ro", "--volume", f"{volume}:/build",
                   "--volume", f"{output}:/export", builder, "python3", "/src/scripts/de25_kernel_platform.py",
                   "--inside", revision, "--jobs", args.jobs]
        run_container(command, args.container_runtime, name, output, args.time_cap)
        return 0
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as error:
        parser.exit(1, "de25-kernel-platform: " + str(error) + "\n")


if __name__ == "__main__":
    raise SystemExit(main())
