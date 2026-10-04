#!/usr/bin/env python3
"""Independently compare sealed native Coleco compositions; no kit operations."""
import argparse
import hashlib
import io
import json
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tomllib
from types import SimpleNamespace

parser = argparse.ArgumentParser()
parser.add_argument("--source", type=Path, required=True)
parser.add_argument("--output", type=Path, required=True)
parser.add_argument("--parts-cli", type=Path, required=True)
parser.add_argument("--expected-revision", required=True, help="exact selected source commit; artifacts must name this original revision")
parser.add_argument("--shell", type=Path, help="frozen native shell output; default: SOURCE/build/fes-coleco-native-video")
parser.add_argument("--package", type=Path, help="sealed .fcore; default: SOURCE/build/packages/<derived-package-id>.fcore")
parser.add_argument("--direct", type=Path, help="explicit Direct producer archive; otherwise discover one exact-shell match")
parser.add_argument("--scanlines", type=Path, help="explicit Scanlines producer archive; otherwise discover one exact-shell match")
parser.add_argument("--sgm", type=Path, help="explicit SGM producer archive; otherwise discover one exact-shell match")
args = parser.parse_args()
source, output, cli = args.source.resolve(), args.output.resolve(), args.parts_cli.resolve()
sys.dont_write_bytecode = True
sys.path.insert(0, str(source))
from scripts import build_fes_coleco_socket_v2 as shell_producer
from scripts import build_video_part as video_producer, build_coleco_sgm as sgm
from scripts import native_video_clock, native_video_parts, coleco_expansion
from scripts.core_package import read_package, package_identity
from scripts.cyclonev_rbf import CramRect, rbf_load, rbf_save, overlay_cram, classify_cram_diff, _iter_cram_diffs
from scripts.export_core_package import build_identity, verify_record_source_at_revision
from scripts.fes_build_common import _cell_counts, validate_timing_resources
from scripts.functional_execution import execution_digest

REVISION = args.expected_revision
shell = args.shell.resolve() if args.shell else source / "build/fes-coleco-native-video"
video_rect, cpu_rect = CramRect(*native_video_parts.CRAM), CramRect(*sgm.CRAM_REGION)
observed = {}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def require(condition, message):
    if not condition:
        raise ValueError(message)


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def read(path):
    path = Path(path)
    require(path.is_file() and not path.is_symlink(), "not a plain evidence file: " + str(path))
    data = path.read_bytes()
    metadata = {"sha256": sha(data), "size": len(data)}
    require(str(path) not in observed or observed[str(path)] == metadata, "evidence changed while reading: " + str(path))
    observed[str(path)] = metadata
    return data


def info(path):
    read(path)
    return {"path": str(path), **observed[str(path)]}


def store(name, data):
    path = output / name
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("xb") as stream:
        stream.write(data)
    return info(path)


def git(*arguments):
    return subprocess.check_output(["git", "-C", str(source), *arguments], text=True).strip()


def discover_part(profile, explicit):
    """Choose one complete producer publication bound to this source and shell."""
    native = profile != "sgm"
    if explicit:
        summaries = [explicit.resolve().parent / "build-summary.json"]
    else:
        directory = source / ("build/video-parts/" + profile if native else "build/coleco-sgm")
        summaries = sorted(directory.glob("*/build-summary.json"))
    matches = []
    for path in summaries:
        summary = json.loads(read(path))
        manifest = summary.get("manifest", {})
        if (manifest.get("revision") != REVISION or manifest.get("shell_package_id") != PACKAGE_ID
                or manifest.get("shell_build_id") != BUILD_ID
                or manifest.get("shell_sha256") != sha(package.payload_bytes)):
            continue
        expected_slot = native_video_parts.INTERFACE if native else "fes.expansion.coleco-bus"
        expected_map = native_video_parts.MAP if native else "fes.coleco-bus.socket/2"
        if manifest.get("slot") != expected_slot or manifest.get("map") != expected_map:
            continue
        if native and summary.get("recipe", {}).get("variant") != profile:
            continue
        part_id = summary.get("part_id" if native else "expansion_id")
        require(isinstance(part_id, str) and re.fullmatch(r"[0-9a-f]{64}", part_id), "invalid discovered part ID")
        require(path.parent.name == manifest.get("recipe_sha256"), "producer directory differs from recipe identity")
        archive = explicit.resolve() if explicit else path.parent / (part_id + ".tar")
        require(archive.is_file() and not archive.is_symlink(), "matched producer publication is incomplete: " + str(archive))
        matches.append({"archive": archive, "part_id": part_id, "recipe_sha256": manifest["recipe_sha256"]})
    require(len(matches) == 1, profile + " requires exactly one complete current-source, exact-shell publication; found " + str(len(matches)))
    return matches[0]


def load_archive(path, part_id, expected_map, expected_slot):
    raw = read(path)
    with tarfile.open(fileobj=io.BytesIO(raw), mode="r:") as archive:
        members = archive.getmembers()
        require(len(members) == 2 and {m.name for m in members} == {"manifest.json", "cart.rbf"}
                and all(m.isfile() and not m.pax_headers for m in members), "unexpected part archive members")
        encoded = archive.extractfile("manifest.json").read()
        cart = archive.extractfile("cart.rbf").read()
    manifest = json.loads(encoded)
    require(encoded == canonical(manifest), "noncanonical part manifest")
    require(sha(b"fes-expansion-v1\0" + encoded) == part_id, "part identity changed")
    require(manifest["revision"] == REVISION and manifest["shell_package_id"] == PACKAGE_ID
            and manifest["shell_build_id"] == BUILD_ID and manifest["shell_sha256"] == sha(package.payload_bytes),
            "part is not bound to the designated source and shell")
    require(manifest["map"] == expected_map and manifest["slot"] == expected_slot, "part role/map changed")
    require(manifest["cart_sha256"] == sha(cart) and manifest["cart_size"] == len(cart), "cart digest or size changed")
    require(cart == read(path.parent / "cart.rbf"), "archive differs from compiler cart bytes")
    return manifest, cart


def routed_evidence(directory):
    log = read(directory / "route.log").decode()
    require("Info: Program finished normally." in log and "unrouted" not in log.lower()
            and not re.search(r"^\s*(?:ERROR|FATAL)\b", log, re.M | re.I), "incomplete route")
    backend = shell_producer.factory._require_gpu_backend(log)
    timing = json.loads(read(directory / "timing.json"))
    sgm.validate_cart_timing(timing)
    measured = {name: shell_producer.factory._frequency_row(timing["fmax"], requested, name, name)
                for name, requested in sgm.REQUIRED_CLOCKS_MHZ.items()}
    factory = shell_producer.factory
    resources = validate_timing_resources(timing.get("utilization"), factory.ORDINARY_RESOURCES |
        set(factory.REQUIRED_RESOURCES) | factory.FORBIDDEN_RESOURCES | factory.REQUIRED_ZERO_RESOURCES)
    return {"route": {"complete": True, "gpu_backend": backend}, "timing": json.loads(json.dumps(measured)),
            "resources": resources}


require(re.fullmatch(r"[0-9a-f]{40}", REVISION), "expected revision must be a full lowercase source commit")
require(git("rev-parse", "HEAD") == REVISION and not git("status", "--porcelain"), "selected source is not the clean build revision")
require(not (output / "build.json").exists(), "proof has already been published")
require(cpu_rect.y1 <= video_rect.y0, "CPU and video physical regions overlap")
shell_manifest, shell_payload = read(shell / "manifest.toml"), read(shell / "core.rbf")
shell_descriptor = tomllib.loads(shell_manifest.decode())
PACKAGE_ID = package_identity(shell_manifest, shell_payload)
BUILD_ID = shell_descriptor["build"]["id"]
package_path = args.package.resolve() if args.package else source / "build/packages" / (PACKAGE_ID + ".fcore")
package = read_package(package_path)
require(package.package_id == PACKAGE_ID and package.fields["build"]["id"] == BUILD_ID
        and package.fields["build"]["revision"] == REVISION, "designated package identity changed")
require(package.manifest_bytes == shell_manifest and package.payload_bytes == shell_payload,
        "sealed package differs from the frozen shell")
require(video_producer.package_profile(package) is native_video_parts, "package is not the native shell")
selected_parts = {profile: discover_part(profile, getattr(args, profile)) for profile in ("direct", "scanlines", "sgm")}
VIDEO_IDS = {profile: selected_parts[profile]["part_id"] for profile in ("direct", "scanlines")}
SGM_ID = selected_parts["sgm"]["part_id"]
cli_version = subprocess.check_output(["go", "version", "-m", str(cli)])
require(("vcs.revision=" + REVISION).encode() in cli_version and b"vcs.modified=false" in cli_version,
        "Go linker binary is not built from the clean selected revision")
cli_version_info = store("go-build-info.txt", cli_version)
cli_info, verifier_info = info(cli), info(Path(__file__).resolve())
record = read(shell / "build-inputs.json")
record_fields = verify_record_source_at_revision(source, record)
require(build_identity(record) == BUILD_ID and record_fields["revision"] == REVISION, "shell source identity changed")
summary = json.loads(read(shell / "build-summary.json"))
shell_summary = summary
require(execution_digest(shell_summary["execution"]) == record_fields["parameters"]["execution_sha256"],
        "shell execution evidence differs from its build identity")
shell_evidence = shell_producer.factory.validate_build_evidence(shell, source)
for key in ("status", "timing", "resources", "synthesis_cells", "rbf"):
    require(summary[key] == shell_evidence[key], "shell " + key + " evidence changed")
require(shell_producer.manifest(record, summary, record_fields["repository"], REVISION,
                               record_fields["tools"], native_video=True) == package.manifest_bytes,
        "shell manifest differs from original producer evidence")
coleco_expansion.validate_routed_shell(shell / "routed.json", version=2)
native_video_parts.validate_boundary(json.loads(read(shell / "routed.json"))["modules"]["top"], routed=True)
for filename in ("socket.qsf", "synth.json", "timing.json", "nextpnr.log"):
    info(shell / filename)
base = rbf_load(package.payload_bytes)
print("Verified designated shell source, physical boundaries and timing", flush=True)

parts, decoded, previews = {}, {}, {}
for profile in ("direct", "scanlines", "sgm"):
    native = profile != "sgm"
    archive = selected_parts[profile]["archive"]
    directory = archive.parent
    part_id = selected_parts[profile]["part_id"]
    manifest, cart = load_archive(archive, part_id,
        native_video_parts.MAP if native else "fes.coleco-bus.socket/2",
        native_video_parts.INTERFACE if native else "fes.expansion.coleco-bus")
    summary = json.loads(read(directory / "build-summary.json"))
    require(summary["manifest"] == manifest and manifest["recipe_sha256"] == sha(canonical(summary["recipe"])),
            "part recipe/manifest differs from build evidence")
    require(summary["recipe"]["tools"] == record_fields["tools"], "part compiler differs from shell compiler")
    for name, expected in summary["recipe"]["inputs"].items():
        path = shell / name.removeprefix("shell/") if name.startswith("shell/") else source / name
        require(sha(read(path)) == expected, "original part source closure differs: " + name)
    evidence = routed_evidence(directory)
    cells = json.loads(read(directory / "cart.json"))
    evidence["synthesis_cells"] = _cell_counts(cells)
    require(not any(evidence["synthesis_cells"].get(name, 0) for name in
                    shell_producer.factory.FORBIDDEN_RESOURCES | set(shell_producer.factory.REQUIRED_RESOURCES)),
            "part owns a forbidden resource")
    if native:
        original = read(directory / "cart-synth.json")
        normalized, clock_proof = native_video_clock.normalize_native_clock_inputs(
            json.loads(original, object_pairs_hook=native_video_clock._object))
        prepared = (json.dumps(normalized, sort_keys=True, separators=(",", ":")) + "\n").encode()
        require(prepared == read(directory / "cart.json"), "native clock normalization changed other cart bytes")
        clock_proof.update(synth_sha256=sha(original), prepared_sha256=sha(prepared))
        require(clock_proof == summary["native_clock_boundary"], "native clock receipt changed")
        clocks = video_producer.validate_clocks(json.loads(read(directory / "cart-routed.json")), layout=native_video_parts)
        require(clocks == summary["checked_clock_pins"] == clock_proof["clock_pins"], "native clock pins changed")
        require(clock_proof["ram_blocks"] == 48 and sum(evidence["synthesis_cells"].get(name, 0)
                for name in ("MISTRAL_M10K", "MISTRAL_M10K_TDP")) == 48, "native RAM ownership changed")
        for key in ("route", "timing", "resources", "synthesis_cells"):
            require(summary[key] == evidence[key], "native " + key + " summary differs")
        require(summary["cram_policy"] == "strict-rectangle-v1" and summary["preview_matches_routed_cram"] is True,
                "native producer policy changed")
        evidence.update(native_clock_boundary=clock_proof, checked_clock_pins=clocks)
    else:
        clocks = video_producer.validate_clocks(json.loads(read(directory / "cart-routed.json")),
                                               layout=SimpleNamespace(CLOCK="system_clock.clocks[0]"))
        require(clocks > 0, "SGM sequential state did not survive routing")
        evidence.update(checked_clock_pins=clocks, imported_clock="system_clock.clocks[0]")
    placed = rbf_load(cart)
    require(placed.header == base.header, "cart changed ORAM/PRAM header")
    rect = video_rect if native else cpu_rect
    changes = classify_cram_diff(base, placed, rect, include_outside_coordinates=True, ignore_ecc_columns=not native)
    strict = classify_cram_diff(base, placed, rect, include_outside_coordinates=True, ignore_ecc_columns=False)
    require(changes["bits_outside_slot"] == 0 and changes == summary["cram_diff"], "part containment changed")
    require(strict["bits_outside_slot"] == 0, "observed route has an outside-fence decoded bit")
    containment = json.loads(read(directory / "cram-diff.json"))
    require(containment["archive_published"] is True and containment["cram_diff"] == changes,
            "part publication containment differs")
    preview = overlay_cram(base, placed, rect)
    require(preview.cram == placed.cram, "compiler route differs from complete contained preview")
    preview_bytes = rbf_save(preview, compressed=True)
    require(preview_bytes == read(directory / "linked.rbf"), "producer compiler preview differs from Python recomposition")
    decoded[profile], previews[profile] = placed, preview
    parts[profile] = {"part_id": part_id, "archive": info(archive), "manifest": manifest,
                      "build_summary": info(directory / "build-summary.json"), "timing_report": info(directory / "timing.json"),
                      "compiler_preview": info(directory / "linked.rbf"), "evidence": evidence,
                      "cram_diff": changes, "observed_strict_cram_diff": strict,
                      "admission_policy": "strict-rectangle-v1" if native else "legacy-columns-v1",
                      "compiler_preview_matches_recomposed_rbf": True,
                      "compiler_preview_matches_routed_cram": True,
                      "legacy_excluded_column_bits": sum(int(strict["column_bits"].get(str(column), 0)) for column in base.die.ecc_columns)}
    print("Verified " + profile + " source binding, route, clocks and containment", flush=True)

compositions = []
for profile in ("direct", "scanlines"):
    for with_sgm in (False, True):
        name = profile + ("-sgm" if with_sgm else "")
        target = output / "cases" / name
        target.mkdir(parents=True)
        transport = target / "go.parts.tar"
        command = [str(cli), "-package", str(package_path), "-video", parts[profile]["archive"]["path"]]
        if with_sgm:
            command += ["-expansion", parts["sgm"]["archive"]["path"]]
        command += ["-out", str(transport)]
        result = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False)
        store("cases/" + name + "/go.stdout.json", result.stdout)
        store("cases/" + name + "/go.stderr.txt", result.stderr)
        require(result.returncode == 0, "Go parts composition failed: " + result.stderr.decode(errors="replace"))
        composition = json.loads(result.stdout)
        with tarfile.open(fileobj=io.BytesIO(read(transport)), mode="r:") as archive:
            require(archive.extractfile("package.tar").read() == read(package_path), "Go transport changed source package")
            require(json.loads(archive.extractfile("parts.json").read()) == composition, "Go transport identity differs")
            go_rbf = archive.extractfile("linked.rbf").read()
        go_file = store("cases/" + name + "/go-linked.rbf", go_rbf)
        cpu = previews["sgm"] if with_sgm else base
        python = overlay_cram(cpu, decoded[profile], video_rect)
        preview = overlay_cram(previews[profile], previews["sgm"], cpu_rect) if with_sgm else previews[profile]
        require(python.header == preview.header and python.cram == preview.cram, "compiler-preview combination differs")
        python_bytes = rbf_save(python, compressed=True)
        python_file = store("cases/" + name + "/python-linked.rbf", python_bytes)
        require(python_bytes == go_rbf, "Go and Python merged RBF bytes differ")
        actual = rbf_load(go_rbf)
        require(actual.header == base.header and actual.cram == python.cram, "Go and Python decoded CRAM differ")
        require(composition["package_id"] == PACKAGE_ID and composition["layout"] == native_video_parts.LAYOUT
                and composition["shell_sha256"] == sha(package.payload_bytes)
                and composition["payload_sha256"] == sha(go_rbf) and composition["payload_size"] == len(go_rbf),
                "Go composition identity differs from actual bytes")
        expected_parts = ([{"role": "expansion", "part_id": SGM_ID}] if with_sgm else []) + [
                          {"role": "video", "part_id": VIDEO_IDS[profile]}]
        require(composition["parts"] == expected_parts, "Go composition selected different parts")
        changes = {"native_video": 0, "cpu_expansion": 0, "outside_declared_regions": 0}
        for x, y in _iter_cram_diffs(base, actual):
            if video_rect.contains(x, y):
                changes["native_video"] += 1
            elif with_sgm and cpu_rect.contains(x, y):
                changes["cpu_expansion"] += 1
            else:
                changes["outside_declared_regions"] += 1
        require(changes["outside_declared_regions"] == 0
                and changes["native_video"] == parts[profile]["observed_strict_cram_diff"]["bits_inside_slot"]
                and changes["cpu_expansion"] == (parts["sgm"]["observed_strict_cram_diff"]["bits_inside_slot"] if with_sgm else 0),
                "merged CRAM lost or changed a routed bit")
        if not with_sgm:
            require(go_rbf == read(Path(parts[profile]["compiler_preview"]["path"]))
                    and go_rbf == read(Path(parts[profile]["archive"]["path"]).parent / "cart.rbf"),
                    "native Go composition differs from exact compiler preview/cart")
        compositions.append({"case": name, "command": command, "exit_code": result.returncode,
            "composition": composition, "transport": info(transport), "go_payload": go_file, "python_payload": python_file,
            "rbf_bytes_equal": True, "decoded_cram_equal": True, "cram_sha256": sha(actual.cram),
            "header_preserved": True, "matches_combined_compiler_previews": True,
            "matches_exact_native_compiler_rbf": not with_sgm, "strict_region_changes": changes})
        print("Verified Go/Python full RBF and decoded CRAM: " + name, flush=True)

require(git("rev-parse", "HEAD") == REVISION and not git("status", "--porcelain"), "selected source changed during proof")
for path, expected in observed.items():
    data = Path(path).read_bytes()
    require({"sha256": sha(data), "size": len(data)} == expected, "evidence bytes changed during proof: " + path)
result = {"format": 1, "classification": "exact-artifact host build and composition evidence; no hardware acceptance",
    "invocation": {"argv": list(sys.orig_argv), "cwd": str(Path.cwd())},
    "discovered_artifacts": {"shell_directory": str(shell), "package_archive": str(package_path),
        "parts": {profile: {key: str(value) if isinstance(value, Path) else value for key, value in selected.items()}
                  for profile, selected in selected_parts.items()}},
    "source": {"revision": REVISION, "directory": str(source), "clean_before_and_after": True,
               "repository": record_fields["repository"], "source_path": record_fields.get("source_path"),
               "fogcast_tree": git("rev-parse", "HEAD:sources/FogCast"),
               "expansion_tree": git("rev-parse", "HEAD:sources/misteross/expansion")},
    "verifier": verifier_info, "go_linker": cli_info, "go_build_info": cli_version_info,
    "shell": {"package_id": PACKAGE_ID, "build_id": BUILD_ID, "archive": info(package_path),
              "payload": info(shell / "core.rbf"), "build_record": info(shell / "build-inputs.json"),
              "build_summary": info(shell / "build-summary.json"), "source_revision": record_fields["revision"],
              "timing": shell_evidence["timing"], "resources": shell_evidence["resources"],
              "synthesis_cells": shell_evidence["synthesis_cells"], "tools": record_fields["tools"],
              "execution": {"sha256": execution_digest(shell_summary["execution"]),
                            **{key: shell_summary["execution"][key] for key in ("version", "gpu_device", "system", "machine", "kernel")}},
              "route": shell_summary["route"]},
    "regions": {"native_video": list(native_video_parts.CRAM), "cpu_expansion": list(sgm.CRAM_REGION), "disjoint": True},
    "parts": parts, "compositions": compositions, "observed_files": observed,
    "limitations": ["Static timing is separately routed shell+video and shell+SGM evidence; the merged video+SGM image is not re-routed here.",
                    "SGM admission retains its existing legacy column policy. These particular SGM bytes additionally have zero strict outside-region differences.",
                    "These checks compose and compare the recorded artifacts on the host; they do not program a kit or qualify HDMI/audio behavior."]}
store("build.json", (json.dumps(result, sort_keys=True, indent=2) + "\n").encode())
print("All four exact-artifact composition checks passed: " + str(output / "build.json"), flush=True)
