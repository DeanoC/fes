#!/usr/bin/env python3
"""Build and run the native Go bare-metal probe or tenfoot QEMU fixture.

No dependency is downloaded by this runner. Populate the module cache using
the explicit commands in README.md before running it.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import struct
import subprocess
import sys
import time

APP = Path(__file__).resolve().parent
ROOT = APP.parents[2]


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def capture(command, env, *, timeout=180):
    result = subprocess.run([str(x) for x in command], cwd=APP, env=env,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                            timeout=timeout)
    return result.returncode, result.stdout.decode(errors="replace")


def objects(stream):
    decoder = json.JSONDecoder()
    position = 0
    while position < len(stream):
        while position < len(stream) and stream[position].isspace():
            position += 1
        if position == len(stream):
            break
        value, position = decoder.raw_decode(stream, position)
        yield value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("program", choices=("probe", "tenfoot"))
    parser.add_argument("--compiler", required=True, type=Path)
    parser.add_argument("--qemu", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--module-cache", type=Path,
                        default=ROOT / "out/embedded-base-tools/tamago/mod")
    parser.add_argument("--timeout", type=float, default=60)
    args = parser.parse_args()
    output = args.output.resolve()
    if output.exists():
        parser.error("output exists; choose a new directory to retain earlier evidence")
    if not output.is_relative_to(ROOT / "out"):
        parser.error("output must be beneath this worktree's out/ directory")
    if args.timeout <= 0 or args.timeout > 300:
        parser.error("timeout must be in (0, 300] seconds")
    output.mkdir(parents=True)
    compiler = args.compiler.resolve()
    qemu = args.qemu.resolve()
    env = dict(os.environ, GOOS="tamago", GOARCH="arm64", CGO_ENABLED="0",
               GOOSPKG="github.com/usbarmory/tamago", GOTOOLCHAIN="local",
               GOENV="off", GOWORK="off", GOPROXY="off", GOSUMDB="off",
               GOMODCACHE=str(args.module_cache.resolve()),
               GOCACHE=str(ROOT / "out/embedded-base-tools/tamago/cache"))
    record = {"status": "incomplete", "program": args.program,
              "classification": "QEMU-only native Go bare metal; not DE25 hardware",
              "commands": [], "environment": {k: env[k] for k in (
                  "GOOS", "GOARCH", "CGO_ENABLED", "GOOSPKG", "GOTOOLCHAIN",
                  "GOENV", "GOWORK", "GOPROXY", "GOSUMDB", "GOMODCACHE", "GOCACHE")}}

    def save():
        (output / "result.json").write_text(json.dumps(record, indent=2) + "\n")

    def checked(name, command):
        record["commands"].append([str(x) for x in command])
        save()
        code, text = capture(command, env)
        (output / (name + ".log")).write_text(text)
        if code:
            raise RuntimeError(f"{name} exited {code}; inspect {output / (name + '.log')}")
        return text

    try:
        record["compiler_sha256"] = sha(compiler)
        record["compiler_version"] = checked("compiler", [compiler, "version"]).strip()
        if "go1.26.5 " not in record["compiler_version"]:
            raise RuntimeError("this experiment pins the TamaGo Go 1.26.5 compiler")
        record["qemu_sha256"] = sha(qemu)
        record["qemu_version"] = checked("qemu", [qemu, "--version"]).strip()
        deps = checked("packages", [compiler, "list", "-deps", "-json", "-mod=readonly",
                                    "./cmd/" + args.program])
        files = set([APP / "go.mod", APP / "go.sum", APP / "run.py", APP / "toolchain.lock.toml"])
        modules = {}
        for package in objects(deps):
            if module := package.get("Module"):
                modules[module["Path"]] = module
            directory = Path(package["Dir"])
            if directory.is_relative_to(ROOT):
                for field in ("GoFiles", "CgoFiles", "CFiles", "HFiles", "SFiles", "EmbedFiles"):
                    files.update(directory / f for f in package.get(field, []))
        record["modules"] = modules
        record["source_sha256"] = {str(p.relative_to(ROOT)): sha(p) for p in sorted(files)}
        elf = output / (args.program + ".elf")
        checked("build", [compiler, "build", "-mod=readonly", "-trimpath",
                           "-ldflags", "-T 0x40010000 -R 0x1000",
                           "-o", elf, "./cmd/" + args.program])
        record["elf_sha256"] = sha(elf)
        record["elf_bytes"] = elf.stat().st_size
        data = elf.read_bytes()
        if data[:6] != b"\x7fELF\x02\x01" or struct.unpack_from("<H", data, 18)[0] != 183:
            raise RuntimeError("expected a little-endian AArch64 ELF")
        phoff = struct.unpack_from("<Q", data, 32)[0]
        phsize, phcount = struct.unpack_from("<HH", data, 54)
        if any(struct.unpack_from("<I", data, phoff + n*phsize)[0] == 3 for n in range(phcount)):
            raise RuntimeError("bare-metal image unexpectedly has an ELF interpreter")
        record["elf_machine"] = "AArch64"
        record["elf_interpreter"] = None
        checked("image-go-version", [compiler, "version", "-m", elf])
        # QEMU default non-secure direct kernel entry is EL1. EL2 is unsupported.
        command = [str(qemu), "-machine", "virt,gic-version=3", "-cpu", "cortex-a53",
                   "-m", "256M", "-smp", "1", "-nographic", "-monitor", "none",
                   "-net", "none", "-kernel", str(elf)]
        record["commands"].append(command)
        save()
        terminal = b"FES_NATIVE_GO_PROBE PASS" if args.program == "probe" else b"FES_NATIVE_TENFOOT PASS"
        log = bytearray()
        started = time.monotonic()
        process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(process.stdout, selectors.EVENT_READ)
                while time.monotonic() - started < args.timeout:
                    for key, _ in selector.select(timeout=0.1):
                        chunk = os.read(key.fd, 65536)
                        if not chunk:
                            selector.unregister(key.fileobj)
                            break
                        log.extend(chunk)
                    if terminal in log or process.poll() is not None:
                        break
        finally:
            if process.poll() is None:
                process.terminate()
            try:
                tail, _ = process.communicate(timeout=2)
            except subprocess.TimeoutExpired:
                process.kill()
                tail, _ = process.communicate()
            log.extend(tail)
        text = log.decode(errors="replace")
        (output / "console.log").write_text(text)
        record["elapsed_seconds"] = time.monotonic() - started
        if terminal not in log or re.search(r"panic:|\bFAIL\b|fatal error:", text):
            raise RuntimeError("guest did not complete; inspect console.log")
        record["termination"] = "runner stopped its emulator after guest completion marker"
        if args.program == "tenfoot":
            frames = []
            pattern = (r"FES_FRAME_BEGIN stage=(\d+) focus=(\d+) bytes=(\d+) sha256=([0-9a-f]{64})\r?\n"
                       r"([A-Za-z0-9+/=\r\n]+)\r?\nFES_FRAME_END")
            for match in re.finditer(pattern, text):
                stage, focus, size, digest, data = match.groups()
                png = base64.b64decode("".join(data.split()), validate=True)
                if len(png) != int(size) or hashlib.sha256(png).hexdigest() != digest:
                    raise RuntimeError("frame size/hash mismatch")
                if not png.startswith(b"\x89PNG\r\n\x1a\n"):
                    raise RuntimeError("frame is not PNG")
                name = "frame-" + stage + ".png"
                (output / name).write_bytes(png)
                frames.append({"path": name, "stage": int(stage), "focus": int(focus),
                               "bytes": len(png), "sha256": digest})
            if ([f["stage"] for f in frames] != [1, 2, 3] or
                    [f["focus"] for f in frames] != [0, 1, 0]):
                raise RuntimeError("missing frame/navigation evidence")
            record["frames"] = frames
        if any(sha(ROOT / p) != h for p, h in record["source_sha256"].items()):
            raise RuntimeError("source changed during build/run; repeat when edits finish")
        record["status"] = "passed"
        print(terminal.decode())
        print(output / "result.json")
    except (OSError, RuntimeError, ValueError, subprocess.SubprocessError) as exc:
        record.update(status="failed", error=str(exc))
        print(exc, file=sys.stderr)
        return 1
    finally:
        save()
    return 0


if __name__ == "__main__":
    sys.exit(main())
