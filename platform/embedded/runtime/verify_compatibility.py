#!/usr/bin/env python3
"""Compare native Go contract checking with the existing C++ package checker.

This creates only synthetic retained archives and host executables. It never
constructs hardware or provides a physical programming profile.
"""
import argparse
import copy
import hashlib
import importlib.util
import itertools
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import time
import tomllib
import uuid

import yaml

ROOT = Path(__file__).resolve().parents[3]
TAMAGO = ROOT / "platform/embedded/tamago"
sys.path.insert(0, str(TAMAGO))
from admission_fixtures import prepare


def sha(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def manifest_bytes(d):
    rows = ["format = " + str(d["format"])]
    if not d.get("interfaces"):
        rows.append("interfaces = []")
    for name in ("core", "target", "payload", "abi", "build", "rom", "rom_map"):
        if name in d:
            rows.append("\n[" + name + "]")
            rows.extend(key + " = " + json.dumps(value) for key, value in d[name].items())
    for name in ("interfaces", "roms"):
        for table in d.get(name, []):
            rows.append("\n[[" + name + "]]")
            rows.extend(key + " = " + json.dumps(value) for key, value in table.items())
    return ("\n".join(rows) + "\n").encode()


def matrix(root):
    packages = root / "sources/mister-packages"
    corpus = packages / "testdata"
    bases = {version: tomllib.loads((corpus / f"core-bundle-v{version}/manifests/valid-basic.toml").read_text()) for version in (2, 3, 4)}
    rows = []
    definitions = {name: yaml.safe_load((packages / f"packages/abi/fes_{name}.yaml").read_text()) for name in ("simple_game", "simple_computer", "application", "computer")}
    video = "fes.video.fixed-720p60"
    defaults = {"simple_game": ["fes.gamepad", video], "simple_computer": ["fes.keyboard", video, "fes.media.blob"], "application": [video], "computer": [video]}

    def interface(ident, required=True, major=1, minor=0):
        return dict(id=ident, major=major, minor=minor, required=required)

    def add(name, d):
        assert name not in {n for n, _ in rows}
        rows.append((name, copy.deepcopy(d)))

    for name, definition in definitions.items():
        d = copy.deepcopy(bases[2])
        d["abi"] = dict(id=definition["id"], major=1, minor=0)
        d["interfaces"] = [interface(i) for i in defaults[name]]
        add(name + "/base", d)
        for key, value in (("major", 2), ("minor", 1), ("minor", 65535), ("id", "test.unknown")):
            changed = copy.deepcopy(d); changed["abi"][key] = value
            add(name + f"/abi-{key}-{value}", changed)
        changed = copy.deepcopy(d); changed["core"]["system"] = "test.system"
        add(name + "/system", changed)
        for profile in ("development-contained-v1", "test.unknown"):
            changed = copy.deepcopy(d); changed["target"]["programming_profile"] = profile
            add(name + "/profile-" + profile, changed)
        changed = copy.deepcopy(d); changed["target"].update(platform="de25_nano", device="A5EB013BB23BE4SCS")
        add(name + "/nano-target", changed)
        for removed in d["interfaces"]:
            changed = copy.deepcopy(d); changed["interfaces"] = [i for i in changed["interfaces"] if i["id"] != removed["id"]]
            add(name + "/missing-" + removed["id"], changed)
        optional = ["test.unknown", "fes.fabric.video.display-plane", "fes.fabric.video.raster-rgb888", "fes.fabric.video.native-pixels",
                    "fes.expansion.coleco-bus", "fes.expansion.apple2-bus", "fes.expansion.c64-bus", "fes.expansion.atari-st-bus", "fes.expansion.spectrum-bus"]
        declared = {i["id"]: (i["major"], i["minor"]) for i in definition["interfaces"]}
        declared.update({ident: (2 if ident == "fes.expansion.coleco-bus" else 1, 0) for ident in optional})
        for ident, (major, minor) in declared.items():
            for required in (False, True):
                for label, a, b in (("current", major, minor), ("new-major", major + 1, minor), ("new-minor", major, minor + 1)):
                    changed = copy.deepcopy(d)
                    changed["interfaces"] = [i for i in changed["interfaces"] if i["id"] != ident] + [interface(ident, required, a, b)]
                    add(name + f"/{ident}-{required}-{label}", changed)
        # Use the real shared structural ROM vectors. These are not runnable
        # bitstreams; the check compares contract admission, not FPGA contents.
        for version in (3, 4):
            changed = copy.deepcopy(bases[version]); changed["abi"] = copy.deepcopy(d["abi"]); changed["interfaces"] = copy.deepcopy(d["interfaces"])
            for role in (("firmware", "cartridge") if version == 3 else ("both",)):
                if version == 3: changed["rom"]["role"] = role
                add(name + f"/format-{version}-{role}", changed)
                for mailbox in ("fes.media.blob", "fes.media.blob-stream", "fes.firmware.blob"):
                    altered = copy.deepcopy(changed)
                    altered["interfaces"] = [i for i in altered["interfaces"] if i["id"] != mailbox] + [interface(mailbox, False)]
                    add(name + f"/format-{version}-{role}-optional-{mailbox}", altered)
        if name == "simple_game":
            groups = [("persistence", ["fes.persistence.words", "fes.pong.progress"])]
        elif name == "application":
            groups = [("menu", ["fes.memory.hps-ddr", "fes.video.menu-display", "fes.video.menu-blitter"]),
                      ("controllers", ["fes.gamepad", "fes.gamepad.ports", "fes.keypad.ports"]),
                      ("media", ["fes.media.blob", "fes.media.blob-stream"]),
                      ("plane", ["fes.memory.hps-ddr", "fes.video.session-display", "fes.fabric.video.display-plane"])]
        elif name == "computer":
            groups = [("spectrum", ["fes.media.spectrum-tape", "fes.media.spectrum-disk", "fes.memory.hps-ddr"]),
                      ("atari", ["fes.media.atari-st-floppy", "fes.media.atari-st-floppy-write", "fes.media.atari-st-floppy-geometry"]),
                      ("zx81", ["fes.media.zx81-tape", "fes.media.zx81-tape-write"]),
                      ("plane", ["fes.memory.hps-ddr", "fes.video.session-display", "fes.fabric.video.display-plane"])]
        else:
            groups = [("plane", ["fes.memory.hps-ddr", "fes.video.session-display", "fes.fabric.video.display-plane"])]
        for group, names in groups:
            for bits in itertools.product((False, True), repeat=len(names)):
                changed = copy.deepcopy(d)
                changed["interfaces"] += [interface(ident, major=2 if ident == "fes.media.spectrum-tape" else 1) for ident, enabled in zip(names, bits) if enabled]
                add(name + "/" + group + "-" + "".join(str(int(b)) for b in bits), changed)
        if name == "computer":
            # Shared ABI semantics remain separate from native driver admission.
            for minor in (0, 1):
                for group, names in (("ata", ["fes.media.ata-disk", "fes.memory.hps-ddr"]),
                                     ("pcxt", ["fes.media.pcxt-floppy", "fes.media.pcxt-floppy-write"]),
                                     ("persistence", ["fes.persistence.words", "fes.pc386.machine"]),
                                     ("rtc", ["fes.rtc.wall-clock"])):
                    for bits in itertools.product((False, True), repeat=len(names)):
                        changed = copy.deepcopy(d)
                        changed["abi"]["minor"] = minor
                        changed["interfaces"] += [interface(ident) for ident, enabled in zip(names, bits) if enabled]
                        add(name + f"/minor-{minor}/" + group + "-" + "".join(str(int(b)) for b in bits), changed)
        if name == "computer":
            for group, names in (("media", ["fes.media.apple2-floppy", "fes.media.c64-disk", "fes.media.atari-st-floppy", "fes.media.spectrum-tape", "fes.media.zx81-tape"]),
                                 ("bus", ["fes.expansion.apple2-bus", "fes.expansion.c64-bus", "fes.expansion.atari-st-bus", "fes.expansion.spectrum-bus"])):
                for pair in itertools.combinations(names, 2):
                    changed = copy.deepcopy(d); changed["interfaces"] += [interface(ident, group != "bus", major=2 if ident == "fes.media.spectrum-tape" else 1) for ident in pair]
                    add(name + "/two-" + group + "-" + "-".join(pair), changed)
        if name == "application":
            for version in (2, 4):
                changed = copy.deepcopy(bases[version]); changed["abi"] = d["abi"]; changed["core"]["id"] = "fes.coleco"
                if version == 4:
                    changed["roms"][0].update(source_size=8192, source_offset=0)
                    changed["roms"][1].update(source_size=16384, source_offset=8192)
                for socket in ("fes.fabric.video.raster-rgb888", "fes.fabric.video.native-pixels"):
                    for bus in (False, True):
                        altered = copy.deepcopy(changed); altered["interfaces"] = [interface(video), interface(socket, False)]
                        if bus: altered["interfaces"].append(interface("fes.expansion.coleco-bus", False, 2))
                        add(name + f"/coleco-v{version}-{socket}-bus-{bus}", altered)
    return rows


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / "out" / ("native-compatibility-" + uuid.uuid4().hex))
    args = parser.parse_args(); output = args.output.absolute()
    if ".." in output.parts or not output.is_relative_to(ROOT / "out") or output.exists() or any(p.is_symlink() for p in (output, *output.parents)):
        parser.error("output must be a fresh private directory under FES out/")
    output.mkdir(parents=True)
    signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(RuntimeError("bounded compatibility job terminated")))
    if shutil.disk_usage(ROOT).free < 30 * (1 << 30):
        raise RuntimeError("less than 30 GiB free for compatibility validation")
    deadline = time.monotonic() + 300
    heartbeat = time.monotonic() + 12
    paths = set()
    for folder in (TAMAGO / "runtimeadmission", TAMAGO / "admissioncheck", TAMAGO / "computer/generated",
                   ROOT / "platform/embedded/runtime", ROOT / "sources/libmister-runtime",
                   ROOT / "sources/FogCast/corepackage", ROOT / "sources/misteross/expansion",
                   ROOT / "sources/mister-packages/packages/abi"):
        paths.update(p for p in folder.rglob("*") if p.is_file() and p.suffix in (".go", ".py", ".cpp", ".hpp", ".h", ".yaml"))
    paths.update((TAMAGO / "admission_fixtures.py", TAMAGO / "package_fixtures.py", TAMAGO / "go.mod", TAMAGO / "go.sum"))
    paths.add(ROOT / "sources/misteross/scripts/core_package.py")
    before = {str(p.relative_to(ROOT)): sha(p) for p in paths}
    commands = []

    def run(name, argv, cwd=ROOT, extra=None):
        nonlocal heartbeat
        argv = [str(a) for a in argv]
        log = output / (name + ".log"); cap = min(deadline, time.monotonic() + 120)
        with log.open("xb") as stream:
            child = subprocess.Popen(argv, cwd=cwd, env=dict(os.environ, GOWORK="off", **(extra or {})),
                                     stdout=stream, stderr=subprocess.STDOUT, start_new_session=True)
            try:
                while child.poll() is None:
                    if time.monotonic() >= cap:
                        raise TimeoutError(name + " reached the bounded validation deadline")
                    if time.monotonic() >= heartbeat:
                        print("Native compatibility: running " + name, flush=True)
                        heartbeat = time.monotonic() + 12
                    time.sleep(.01)
            finally:
                if child.poll() is None:
                    os.killpg(child.pid, signal.SIGTERM)
                    try: child.wait(timeout=5)
                    except subprocess.TimeoutExpired: os.killpg(child.pid, signal.SIGKILL); child.wait(timeout=5)
        commands.append(dict(label=name, argv=argv, cwd=str(cwd), returncode=child.returncode, log_sha256=sha(log)))
        if child.returncode: raise RuntimeError(name + " failed; see " + str(log))
        return log.read_text()

    _, _, inputs = prepare(ROOT, output, lambda name, argv: run(name, argv))
    spec = importlib.util.spec_from_file_location("compatibility_archive", ROOT / "sources/misteross/scripts/core_package.py")
    producer = importlib.util.module_from_spec(spec); sys.modules[spec.name] = producer; spec.loader.exec_module(producer)
    cases = []
    corpus = ROOT / "sources/mister-packages/testdata"
    for n, (name, d) in enumerate(matrix(ROOT)):
        version = d["format"]; shared = corpus / f"core-bundle-v{version}"
        payload = (shared / "payloads/fes-fixture.rbf").read_bytes()
        mapping = (shared / "maps/valid-basic.json").read_bytes() if version != 2 else None
        if version == 4 and sum(r["source_size"] for r in d["roms"]) != 2048:
            total = sum(r["source_size"] for r in d["roms"])
            parsed = json.loads(mapping)
            parsed["source_size"] = total
            parsed["blocks"] = [dict(bel=f"compat-{i}", source_offset=i*1024,
                word_bits=[list(range(32*7605+i*10240+j*40,32*7605+i*10240+(j+1)*40)) for j in range(256)]) for i in range(total//1024)]
            mapping = (json.dumps(parsed, separators=(",", ":")) + "\n").encode()
            d["rom_map"].update(size=len(mapping), sha256=hashlib.sha256(mapping).hexdigest())
        manifest = manifest_bytes(d); folder = output / ("package-" + str(n)); folder.mkdir()
        members = [("manifest.toml", manifest), ("core.rbf", payload)]
        if mapping is not None: members.append(("rom-map.json", mapping))
        archive = bytearray()
        for filename, value in members:
            (folder / filename).write_bytes(value)
            archive.extend(producer._ustar_header(filename, len(value))); archive.extend(value); archive.extend(b"\0" * (-len(value) % 512))
        archive.extend(b"\0" * 1024)
        package_id = producer.package_identity(manifest, payload, mapping, format_version=version)
        decision = json.loads(run("case-" + str(n), [output / "admission-oracle", folder, package_id]))
        assert decision["package_id"] == package_id, (name, decision)
        filename = f"CASE{n:04d}.FCP"; (output / filename).write_bytes(archive)
        cases.append(dict(name=name, path=filename, package_id=package_id, code=decision["code"]))
    (output / "cases.json").write_text(json.dumps(cases, indent=2) + "\n")
    run("go-parity", ["go", "test", "-count=1", "-race", "./runtimeadmission"], cwd=TAMAGO,
        extra={"FES_COMPATIBILITY_CASES": str(output / "cases.json")})
    assert before == {str(p.relative_to(ROOT)): sha(p) for p in paths}, "selected source changed during validation"
    inputs.update(paths); inputs.update(corpus.rglob("*")); inputs.add(ROOT / "sources/misteross/scripts/core_package.py")
    result = dict(status="passed", classification="paired C++ and native Go contract checking; host-only, no physical programming", cases=len(cases),
                  source_commit=subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
                  working_tree_dirty=bool(subprocess.check_output(["git", "status", "--porcelain"], cwd=ROOT)),
                  source_sha256={str(p.relative_to(ROOT)): sha(p) for p in sorted(inputs) if p.is_file() and p.is_relative_to(ROOT)},
                  cases_sha256=sha(output / "cases.json"), oracle_sha256=sha(output / "admission-oracle"), commands=commands)
    (output / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print("PASS", len(cases), "paired C++ and native Go contract decisions; no hardware constructed")


if __name__ == "__main__":
    main()
