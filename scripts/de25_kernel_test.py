#!/usr/bin/env python3
"""Build and boot DE25 DMA ownership tests on an isolated AArch64 QEMU kernel."""
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
import tomllib

ROOT = Path(__file__).resolve().parents[1]
DRIVER = "image/kernel/de25-dma"
KERNEL_DRIVER = "drivers/misc/fes-de25-dma"
REQUIRED_CASES = frozenset((
    "permanent_owner_checks_real_supplier_links",
    "permanent_owner_probe_refuses_unbound_device",
    "permanent_owner_rejects_topology_overrides", "permanent_owner_rejects_ancestor_mutation",
    "permanent_owner_blocks_configuration_before_first_submit",
    "permanent_owner_retains_uncertain_closed_client",
    "permanent_owner_never_adopts_unknown_configuration",
    "owner_recovers_drain_after_last_client_close",
    "owner_recovery_cannot_use_reset_as_drain",
    "owner_reference_release_does_not_revoke_client",
    "guard_real_access_and_bounds", "guard_claim_rejects_wrong_or_existing_identity",
    "guard_dummy_zero_timeout_is_not_success", "guard_post_access_fault_and_reset_reject_results",
    "guard_busy_or_unknown_status_prevents_access", "guarded_transport_constructs_real_dma_grant",
    "backend_lease_required_before_access", "unconfigured_engine_has_one_allocation_lease",
    "constructor_failure_releases_backend_lease", "uncertain_dma_retains_lease_until_storage_free",
    "binding_stays_closed", "rejects_bad_publish_before_effects",
    "kernel_owns_descriptor_and_drain_lifetime", "failed_start_is_held_until_drain",
    "unreadable_epoch_retains_exposed_storage",
    "coherent_mapping_uapi_and_revoke", "contiguous_aliases_are_revoked", "rejects_invalid_mapping_shapes",
    "refused_mapping_releases_new_mm_reference",
    "revoke_catches_inflight_mmap_link",
    "acceptance_is_not_display_and_slots_alternate",
    "ordered_descriptor_precedes_first_submit",
    "stale_generation_and_reset_cannot_prove_drain",
    "faults_never_release_pending_slot",
    "snapshot_handshake_is_bounded_and_consistent",
    "sequence_and_generation_never_wrap",
    "poll_identity_rejects_before_register_effects",
    "mismatched_completion_never_releases_slot",
))


def digest(path):
    value = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(8 << 20), b""):
            value.update(block)
    return value.hexdigest()


def selected_lock(root=ROOT):
    result = tomllib.loads((Path(root) / DRIVER / "linux.lock.toml").read_text())
    if (result.get("format") != 1 or not re.fullmatch(r"[a-f0-9]{40}", result["revision"]) or
            result["repository"] != "https://github.com/terasic/linux-socfpga.git" or
            any(not re.fullmatch(r"[a-f0-9]{64}", result[name]) for name in
                ("vendor_image_sha256", "vendor_config_sha256", "sdk_sha256")) or
            not re.fullmatch(r"sha256:[a-f0-9]{64}", result["builder_image"])):
        raise ValueError("invalid DE25 kernel/test dependency lock")
    return result


def checked(argv, **kwargs):
    print("+ " + " ".join(map(str, argv)), flush=True)
    return subprocess.run(list(map(str, argv)), check=True, **kwargs)


def image_builder():
    lane = ROOT / "image/de25-nano"
    sys.path.insert(0, str(lane))
    spec = importlib.util.spec_from_file_location("de25_kernel_image_builder", lane / "build.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def config_from_image(data):
    start, end = b"IKCFG_ST", b"IKCFG_ED"
    if data.count(start) != 1 or data.count(end) != 1:
        raise ValueError("kernel must contain one bounded IKCONFIG record")
    first, last = data.index(start) + len(start), data.index(end)
    if not first < last or last - first > 1 << 20:
        raise ValueError("invalid IKCONFIG extent")
    # Bound decompression before accepting a configuration from an input Image.
    import zlib
    decoder = zlib.decompressobj(16 + zlib.MAX_WBITS)
    try:
        config = decoder.decompress(data[first:last], (1 << 20) + 1)
    except zlib.error as error:
        raise ValueError("invalid IKCONFIG compression") from error
    if len(config) > 1 << 20 or not decoder.eof or decoder.unused_data:
        raise ValueError("invalid or oversized IKCONFIG payload")
    return config


def passing_cases(report):
    """Require real, passing cases from the driver's KUnit suite."""
    suites = report.get("sub_groups", [])
    found = [suite for suite in suites if suite.get("name") == "fes-de25-dma"]
    if len(found) != 1:
        raise ValueError("KUnit result lacks the unique DE25 DMA suite")
    cases = found[0].get("test_cases", [])
    names = [case.get("name") for case in cases]
    if (set(names) != REQUIRED_CASES or len(set(names)) != len(names) or
            any(case.get("status") != "PASS" for case in cases)):
        raise ValueError("KUnit DMA cases are missing, duplicate, skipped or failed")
    return names


def check_kernel_log(text):
    if re.search(r"\b(?:WARNING:|BUG:|Oops:|Kernel panic|Internal error:)", text):
        raise ValueError("kernel test log contains a warning, oops or panic")


def fetch_kernel(cache, lock):
    cache = Path(cache).resolve()
    if not cache.exists():
        checked(["git", "init", "--bare", cache])
    if subprocess.check_output(["git", "--git-dir", cache, "rev-parse", "--is-bare-repository"], text=True).strip() != "true":
        raise ValueError("kernel cache must be an isolated bare repository")
    exists = subprocess.run(["git", "--git-dir", cache, "cat-file", "-e", lock["revision"] + "^{commit}"],
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
    if not exists:
        checked(["git", "--git-dir", cache, "fetch", "--depth", "1", lock["repository"], lock["revision"]])
    actual = subprocess.check_output(["git", "--git-dir", cache, "rev-parse", lock["revision"] + "^{commit}"], text=True).strip()
    if actual != lock["revision"]:
        raise ValueError("kernel cache does not contain the pinned commit")
    return cache


def extract_kernel(cache, revision, destination):
    destination.mkdir()
    archive = subprocess.Popen(["git", "--git-dir", str(cache), "archive", revision], stdout=subprocess.PIPE)
    try:
        unpacked = subprocess.run(["tar", "--no-same-owner", "-xf", "-", "-C", str(destination)], stdin=archive.stdout)
        archive.stdout.close()
        if archive.wait() or unpacked.returncode:
            raise ValueError("pinned kernel extraction failed")
    finally:
        if archive.poll() is None:
            archive.terminate()
            archive.wait()


def inside(jobs, revision):
    lock = selected_lock()
    export = Path("/export")
    work = Path("/build")
    sdk, kernel = work / "sdk", work / "linux"
    sdk.mkdir()
    checked(["tar", "-xzf", "/image/aarch64-sdk.tar.gz", "--strip-components=1", "-C", sdk])
    checked([sdk / "relocate-sdk.sh"])
    prefix = sdk / "bin/aarch64-buildroot-linux-gnu-"
    checked(["cp", "-a", "/kernel-input", kernel])
    target = kernel / KERNEL_DRIVER
    shutil.copytree(ROOT / DRIVER, target)
    integration = {
        "drivers/misc/Kconfig": '\nsource "drivers/misc/fes-de25-dma/Kconfig"\n',
        "drivers/misc/Makefile": '\nobj-$(CONFIG_FES_DE25_DMA) += fes-de25-dma/\n',
    }
    for relative, addition in integration.items():
        path = kernel / relative
        source = path.read_text()
        if "fes-de25-dma" in source:
            raise ValueError("kernel source already contains the diagnostic driver")
        path.write_text(source + addition)
    configuration = target / ".kunitconfig"
    build = work / "kunit"
    command = [sys.executable, kernel / "tools/testing/kunit/kunit.py", "run",
               "--arch=arm64", "--cross_compile=" + str(prefix),
               "--build_dir=" + str(build), "--kunitconfig=" + str(configuration),
               "--jobs=" + str(jobs), "--timeout=180", "--json=" + str(export / "kunit.json"),
               "fes-de25-dma*"]
    env = dict(os.environ, LC_ALL="C", TZ="UTC", KBUILD_BUILD_USER="fes", KBUILD_BUILD_HOST="de25-kunit")
    for name in ("MAKEFLAGS", "MFLAGS", "MAKELEVEL", "MAKEOVERRIDES", "GNUMAKEFLAGS", "KCONFIG_CONFIG"):
        env.pop(name, None)
    with (export / "kunit-run.log").open("w") as log:
        checked(command, cwd=kernel, env=env, stdout=log, stderr=subprocess.STDOUT, timeout=2400)
    names = passing_cases(json.loads((export / "kunit.json").read_text()))
    check_kernel_log((build / "test.log").read_text())
    for name, source in (("kunit-Image", build / "arch/arm64/boot/Image"),
                         ("kunit.config", build / ".config"),
                         ("kunit-raw.log", build / "test.log"),
                         ("de25_dma.o", build / KERNEL_DRIVER / "de25_dma.o"),
                         ("de25_dma_test.o", build / KERNEL_DRIVER / "de25_dma_test.o")):
        shutil.copy2(source, export / name)
    result = {"classification": "AArch64 QEMU kernel DMA ownership tests", "fes_revision": revision,
              "kernel_revision": lock["revision"], "cases": names, "command": list(map(str, command)),
              "compiler": subprocess.check_output([str(prefix) + "gcc", "--version"], text=True).splitlines()[0],
              "qemu": subprocess.check_output(["qemu-system-aarch64", "--version"], text=True).splitlines()[0],
              "hardware_access": "not-performed", "dma_admission": "not-qualified",
              "module_loadability_on_vendor_kernel": "not-tested",
              "not_modeled": ["DE25 HPS", "FPGA", "SMMU stream binding", "firewall", "physical DDR visibility"],
              "files": {p.name: digest(p) for p in export.iterdir() if p.is_file()}}
    (export / "checks.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("fetch", "run"))
    parser.add_argument("--cache", type=Path, default=ROOT / "out/de25-kernel-cache.git")
    parser.add_argument("--image-output", type=Path, default=ROOT / "out/de25-nano-linux-cxx")
    parser.add_argument("--output", type=Path)
    parser.add_argument("--jobs", type=int, default=8)
    parser.add_argument("--container-runtime", default="docker")
    parser.add_argument("--inside", help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    if not 1 <= args.jobs <= 16:
        parser.error("jobs must be between 1 and 16")
    try:
        if args.inside:
            if ROOT != Path("/src") or not re.fullmatch(r"[a-f0-9]{40}", args.inside):
                raise ValueError("internal action requires the isolated source mount")
            inside(args.jobs, args.inside)
            return 0
        lock = selected_lock()
        cache = fetch_kernel(args.cache, lock)
        if args.action == "fetch":
            return 0
        if not args.output:
            parser.error("run requires a new --output directory")
        lane = image_builder()
        revision, source = lane.snapshot()
        output, image = args.output.resolve(), args.image_output.resolve()
        if output == image or output in image.parents or image in output.parents:
            raise ValueError("kernel test output and image input must be separate")
        output.mkdir(parents=True, exist_ok=False)
        if digest(image / "aarch64-sdk.tar.gz") != lock["sdk_sha256"]:
            raise ValueError("SDK archive differs from the locked producer")
        vendor_image = image / "vendor-boot/Image"
        if digest(vendor_image) != lock["vendor_image_sha256"]:
            raise ValueError("vendor kernel Image differs from the lock")
        config = config_from_image(vendor_image.read_bytes())
        if hashlib.sha256(config).hexdigest() != lock["vendor_config_sha256"]:
            raise ValueError("embedded vendor configuration differs from the lock")
        (output / "vendor.config").write_bytes(config)
        kernel = output / "kernel-input"
        extract_kernel(cache, lock["revision"], kernel)
        volume = "fes-de25-kunit-" + hashlib.sha256(str(output).encode()).hexdigest()[:16]
        builder = lane.ensure_builder(source, args.container_runtime, volume)
        if builder != lock["builder_image"]:
            raise ValueError("kernel test builder differs from the SDK producer")
        inputs = {"format": 1, "fes_revision": revision, "kernel": lock,
                  "source_sha256": {str(p.relative_to(source)): digest(p) for p in (source / DRIVER).rglob("*") if p.is_file()},
                  "runner_sha256": digest(source / "scripts/de25_kernel_test.py")}
        (output / "inputs.json").write_text(json.dumps(inputs, indent=2, sort_keys=True) + "\n")
        checked([args.container_runtime, "run", "--rm", "--network", "none", "--platform", "linux/amd64",
                 "--user", f"{os.getuid()}:{os.getgid()}", "--ulimit", "core=0:0",
                 "--volume", f"{source}:/src:ro", "--volume", f"{image}:/image:ro",
                 "--volume", f"{kernel}:/kernel-input:ro", "--volume", f"{volume}:/build",
                 "--volume", f"{output}:/export", "--env", "LC_ALL=C", "--env", "TZ=UTC", builder,
                 "python3", "/src/scripts/de25_kernel_test.py", "run", "--inside", revision, "--jobs", args.jobs])
        print("Kernel DMA evidence: " + str(output / "checks.json"))
        return 0
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
        parser.exit(1, "de25-kernel-test: " + str(error) + "\n")


if __name__ == "__main__":
    raise SystemExit(main())
