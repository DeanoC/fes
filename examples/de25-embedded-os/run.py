#!/usr/bin/env python3
"""Build and observe the isolated host, Zephyr or bare-metal protocol probe."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

APP = Path(__file__).resolve().parent
ROOT = APP.parents[1]
HEADER = ROOT / "sources/libmister-runtime/src/native/generated/fes_gp.hpp"


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def capture(argv, *, env=None, timeout=120):
    result = subprocess.run([str(x) for x in argv], stdout=subprocess.PIPE,
                            stderr=subprocess.STDOUT, env=env, timeout=timeout)
    return result.returncode, result.stdout.decode(errors="replace")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("mode", choices=("host", "zephyr", "baremetal"))
    parser.add_argument("--output", type=Path)
    parser.add_argument("--sdk", type=Path)
    parser.add_argument("--zephyr-base", type=Path)
    parser.add_argument("--python", type=Path, default=Path(sys.executable))
    parser.add_argument("--cxx", default="g++")
    parser.add_argument("--qemu", type=Path)
    args = parser.parse_args()
    output = (args.output or ROOT / "out/de25-embedded-os" / args.mode).resolve()
    # Keep artifacts out of checked-in source and refuse to replace a prior run.
    if output.exists():
        parser.error("output already exists; choose a fresh --output directory")
    if not output.is_relative_to(ROOT / "out"):
        parser.error("output must be beneath this worktree's out/ directory")
    if args.mode != "host" and args.sdk is None:
        parser.error("--sdk is required for AArch64 builds")
    if args.mode == "zephyr" and args.zephyr_base is None:
        parser.error("--zephyr-base is required for Zephyr")
    output.mkdir(parents=True)
    source_files = sorted(p for p in APP.rglob("*") if p.is_file() and
                          (p.suffix in (".cpp", ".hpp", ".S", ".ld", ".py", ".toml", ".conf")
                           or p.name == "CMakeLists.txt")) + [HEADER]
    record = {
        "classification": "host-only; simulated FPGA endpoint",
        "mode": args.mode,
        "fes_base": capture(["git", "-C", ROOT, "rev-parse", "HEAD"])[1].strip(),
        "source_sha256": {str(p.relative_to(ROOT)): sha(p) for p in source_files},
        "status": "incomplete",
        "commands": [],
    }

    def save():
        (output / "result.json").write_text(json.dumps(record, indent=2) + "\n")

    def checked(label, argv, **kwargs):
        record["commands"].append([str(x) for x in argv])
        save()
        code, log = capture(argv, **kwargs)
        (output / (label + ".log")).write_text(log)
        if code:
            raise RuntimeError(f"{label} failed ({code}); see {output / (label + '.log')}")
        return log

    save()
    try:
        sources = [APP / "src" / name for name in ("probe.cpp", "selftest.cpp")]
        if args.mode == "host":
            compiler = args.cxx
            record["compiler"] = checked("compiler", [compiler, "--version"])
            elf = output / "probe"
            checked("build", [compiler, "-std=c++14", "-O2", "-Wall", "-Wextra",
                    "-Werror", "-pedantic", "-fno-exceptions", "-fno-rtti",
                    "-I" + str(HEADER.parents[2]), *sources, APP / "src/main.cpp",
                    "-o", elf])
            log = checked("console", [elf], timeout=10)
        else:
            sdk = args.sdk.resolve()
            compiler = sdk / "aarch64-zephyr-elf/bin/aarch64-zephyr-elf-g++"
            readelf = sdk / "aarch64-zephyr-elf/bin/aarch64-zephyr-elf-readelf"
            record["sdk_version"] = (sdk / "sdk_version").read_text().strip()
            record["compiler"] = checked("compiler", [compiler, "--version"])
            if args.mode == "baremetal":
                elf = output / "probe.elf"
                checked("build", [compiler, "-std=c++14", "-O2", "-Wall", "-Wextra",
                        "-Werror", "-ffreestanding", "-fno-exceptions", "-fno-rtti",
                        "-fno-stack-protector", "-fno-threadsafe-statics", "-nostdlib",
                        "-fno-unwind-tables", "-fno-asynchronous-unwind-tables",
                        "-fno-use-cxa-atexit", "-mgeneral-regs-only", "-mstrict-align",
                        "-I" + str(HEADER.parents[2]),
                        "-I" + str(APP / "src"), *sources,
                        APP / "baremetal/startup.S", APP / "baremetal/main.cpp",
                        APP / "baremetal/memory.cpp",
                        "-Wl,-T," + str(APP / "baremetal/linker.ld"),
                        "-Wl,--build-id=none", "-o", elf])
            else:
                zephyr = args.zephyr_base.resolve()
                record["zephyr_commit"] = checked("zephyr-version",
                    ["git", "-C", zephyr, "rev-parse", "HEAD"]).strip()
                env = dict(os.environ, ZEPHYR_BASE=str(zephyr),
                           ZEPHYR_SDK_INSTALL_DIR=str(sdk),
                           ZEPHYR_TOOLCHAIN_VARIANT="zephyr")
                build = output / "build"
                checked("configure", ["cmake", "-S", APP, "-B", build, "-GNinja",
                        "-DBOARD=qemu_cortex_a53", "-DPython3_EXECUTABLE=" + str(args.python.absolute()),
                        "-DZEPHYR_MODULES="], env=env)
                checked("build", ["cmake", "--build", build, "--parallel", "4"], env=env)
                elf = build / "zephyr/zephyr.elf"
                record["config_sha256"] = sha(build / "zephyr/.config")
            headers = checked("elf", [readelf, "-h", "-l", elf])
            if "AArch64" not in headers or "INTERP" in headers:
                raise RuntimeError("expected AArch64 image without an ELF interpreter")
            if args.mode == "baremetal":
                nm = sdk / "aarch64-zephyr-elf/bin/aarch64-zephyr-elf-nm"
                if checked("undefined-symbols", [nm, "-u", elf]).strip() or "DYNAMIC" in headers:
                    raise RuntimeError("bare-metal image has runtime linkage dependencies")
                size = sdk / "aarch64-zephyr-elf/bin/aarch64-zephyr-elf-size"
                record["linked_size"] = checked("size", [size, elf])
            qemu = args.qemu or sdk / "sysroots/x86_64-pokysdk-linux/usr/bin/qemu-system-aarch64"
            record["qemu"] = checked("qemu-version", [qemu, "--version"])
            argv = [str(qemu), "-machine", "virt,gic-version=3", "-cpu", "cortex-a53",
                    "-m", "128M", "-smp", "1", "-nographic", "-net", "none",
                    "-kernel", str(elf)]
            record["commands"].append(argv)
            # Both images park after reporting. Timeout stops only this owned emulator.
            try:
                code, log = capture(argv, timeout=10)
                record["emulator_exit"] = code
            except subprocess.TimeoutExpired as exc:
                log = (exc.stdout or b"").decode(errors="replace")
                record["emulator_exit"] = "stopped after 10-second observation window"
            (output / "console.log").write_text(log)
            if isinstance(record["emulator_exit"], int) and record["emulator_exit"] != 0:
                raise RuntimeError(f"emulator exited unexpectedly ({record['emulator_exit']})")
        prefix = "FES_BAREMETAL_PROBE" if args.mode == "baremetal" else "FES_EMBEDDED_PROBE"
        marker = re.search(prefix + r" PASS tests=(\d+) transport=emulated", log)
        if not marker or int(marker[1]) < 16 or re.search(r"\bFAIL\b|FATAL|ASSERTION", log):
            raise RuntimeError("probe did not complete cleanly; inspect console.log")
        if any(sha(ROOT / path) != digest for path, digest in record["source_sha256"].items()):
            raise RuntimeError("source changed during this run; repeat after edits finish")
        record.update(status="passed", tests=int(marker[1]),
                      artifact_sha256=sha(elf), artifact_bytes=elf.stat().st_size)
        print(log, end="")
        print(f"Evidence: {output / 'result.json'}")
    except (OSError, RuntimeError, subprocess.SubprocessError) as exc:
        record.update(status="failed", error=str(exc))
        print(exc, file=sys.stderr)
        return 1
    finally:
        save()
    return 0


if __name__ == "__main__":
    sys.exit(main())
