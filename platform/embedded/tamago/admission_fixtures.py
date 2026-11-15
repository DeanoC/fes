"""Generate trusted model policy from the production C++ compatibility checker.

The JSON is a runner-owned fixture, not signed admission or a physical backend.
"""
import gzip
import hashlib
import importlib.util
import json
from pathlib import Path
import shlex
import sys
import tarfile

from package_fixtures import prepare as package_inputs


def prepare(root, output, checked):
    directory, cases, sources = package_inputs(root, output)
    oracle = output / "admission-oracle"
    runtime = root / "sources/libmister-runtime"
    units = [root / "platform/embedded/runtime/admission_oracle.cpp"] + [
        runtime / "src" / name for name in (
            "native/core_package.cpp", "native/artifacts.cpp", "native/sha256.cpp",
            "native/diagnostic.cpp", "runtime.cpp")]
    objects = []
    for n, source in enumerate(units):
        obj, dep = output / f"oracle-{n}.o", output / f"oracle-{n}.d"
        checked(f"oracle-compile-{n}", ["c++", "-O2", "-std=c++14", "-D_FILE_OFFSET_BITS=64",
                "-ffunction-sections", "-fdata-sections", "-pthread",
                "-I" + str(runtime / "include"), "-I" + str(runtime / "src"),
                "-isystem", runtime / "third_party/toml11/include", "-MD", "-MF", dep,
                "-c", source, "-o", obj])
        # Capture all compiler-reported dependencies, including vendored headers.
        sources.update(Path(p) for p in shlex.split(dep.read_text().replace("\\\n", " ").split(":", 1)[1]))
        objects.append(obj)
    checked("oracle-link", ["c++", "-pthread", *objects, "-Wl,--gc-sections", "-o", oracle])
    capabilities = json.loads(checked("oracle-capabilities", [oracle, "--model-capabilities"]))
    capabilities["runtime_id"] = hashlib.sha256(oracle.read_bytes()).hexdigest()
    capabilities["decisions"] = []
    producer = root / "sources/misteross/scripts/core_package.py"
    spec = importlib.util.spec_from_file_location("fes_admission_fixture_headers", producer)
    module = importlib.util.module_from_spec(spec)
    sys.modules[spec.name] = module
    spec.loader.exec_module(module)
    basic = root / "sources/FogCast/corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml"
    payload = root / "sources/FogCast/corepackage/testdata/core-bundle-v2/payloads/fes-fixture.rbf"
    manifest = basic.read_text()
    more = [
        ("missing-mandatory-gamepad", manifest.replace('required = true', 'required = false', 1), "unsupported_interface"),
        ("newer-abi", manifest.replace('[abi]\nid = "fes.simple-game"\nmajor = 1\nminor = 0', '[abi]\nid = "fes.simple-game"\nmajor = 1\nminor = 1'), "unsupported_abi"),
        ("diagnostic-profile", manifest.replace('fes-gp-v1', 'development-contained-v1'), "unsupported_programming_profile"),
        ("unknown-required", manifest + '\n[[interfaces]]\nid = "test.unknown"\nmajor = 1\nminor = 0\nrequired = true\n', "unsupported_interface"),
        ("unknown-optional", manifest + '\n[[interfaces]]\nid = "test.unknown"\nmajor = 1\nminor = 0\nrequired = false\n', "none"),
        ("invalid-optional-persistence", manifest + '\n[[interfaces]]\nid = "fes.persistence.words"\nmajor = 1\nminor = 0\nrequired = false\n', "unsupported_interface"),
    ]
    sources.update((basic, payload, Path(__file__)))
    for name, text, expected in more:
        body = text.encode()
        data = bytearray()
        for member, value in (("manifest.toml", body), ("core.rbf", payload.read_bytes())):
            data.extend(module._ustar_header(member, len(value)))
            data.extend(value)
            data.extend(b"\0" * (-len(value) % 512))
        data.extend(b"\0" * 1024)
        filename = f"CASE{len(cases):03d}.FCP"
        (directory / filename).write_bytes(data)
        cases.append(dict(name="admission/" + name, path="FES/" + filename, valid=True,
                          package_id=module.package_identity(body, payload.read_bytes()), format=2, expected=expected))
    # Real synthetic RBF/map vectors exercise the existing linker, not the
    # structurally valid 12-byte conformance payloads.
    vectors = root / "sources/misteross/expansion/testdata/rom"
    inputs = {}
    for name in ("blank.rbf", "map.json", "ramp.rom"):
        path = vectors / (name + ".gz")
        sources.add(path)
        inputs[name] = gzip.decompress(path.read_bytes())
    sources.add(vectors / "oracle.json")
    golden = next(c for c in json.loads((vectors / "oracle.json").read_text())["cases"] if c["name"] == "ramp")
    base, mapping, rom = inputs["blank.rbf"], inputs["map.json"], inputs["ramp.rom"]
    linked_cases = []
    for n, (version, role) in enumerate(((3, "firmware"), (3, "cartridge"), (4, "both"))):
        text = manifest.replace("format = 2", f"format = {version}", 1)
        text = text.replace("size = 12", f"size = {len(base)}", 1).replace(hashlib.sha256(payload.read_bytes()).hexdigest(), hashlib.sha256(base).hexdigest())
        metadata = f'file = "rom-map.json"\nsize = {len(mapping)}\nsha256 = "{hashlib.sha256(mapping).hexdigest()}"\n'
        names = []
        if version == 3:
            text += f'\n[rom]\nid = "machine-rom"\nrole = "{role}"\nsource_size = {len(rom)}\n' + metadata
            names.append(("machine-rom", role, rom))
        else:
            text += f'\n[[roms]]\nid = "bios"\nrole = "firmware"\nsource_size = 1024\nsource_offset = 0\n\n[[roms]]\nid = "cart"\nrole = "cartridge"\nsource_size = {len(rom)-1024}\nsource_offset = 1024\n\n[rom_map]\n' + metadata
            names.extend((("bios", "firmware", rom[:1024]), ("cart", "cartridge", rom[1024:])))
        members = (("manifest.toml", text.encode()), ("core.rbf", base), ("rom-map.json", mapping))
        data = bytearray()
        for member, value in members:
            data.extend(module._ustar_header(member, len(value)))
            data.extend(value)
            data.extend(b"\0" * (-len(value) % 512))
        data.extend(b"\0" * 1024)
        filename = f"LINK{n:03d}.FCP"
        (directory / filename).write_bytes(data)
        source_cases = []
        for i, (ident, source_role, value) in enumerate(names):
            source_name = f"ROM{n}{i}.BIN"
            (directory / source_name).write_bytes(value)
            source_cases.append(dict(id=ident, role=source_role, path="FES/"+source_name))
        case = dict(name=f"linked/v{version}-{role}", path="FES/"+filename, valid=True,
                    package_id=module.package_identity(text.encode(), base, mapping, format_version=version),
                    format=version, sources=source_cases, programmed_sha256=golden["rbf_sha256"], programmed_size=golden["rbf_size"])
        linked_cases.append(case)
    (directory / "LINKS.JSON").write_text(json.dumps(linked_cases, indent=2)+"\n")
    admitted_cases = []
    decisions = {}
    for n, case in enumerate(cases + linked_cases):
        if not case["valid"]:
            continue
        private = output / f"oracle-package-{n}"
        private.mkdir(mode=0o700)
        with tarfile.open(directory / Path(case["path"]).name) as archive:
            for member in archive:
                if member.name not in ("manifest.toml", "core.rbf", "rom-map.json") or not member.isfile():
                    raise ValueError("unexpected canonical fixture member")
                (private / member.name).write_bytes(archive.extractfile(member).read())
        decision = json.loads(checked(f"oracle-package-{n}", [oracle, private, case["package_id"]]))
        if decision["package_id"] != case["package_id"]:
            raise ValueError("C++ oracle did not verify the fixture identity")
        decisions[case["package_id"]] = decision
        expected = case.get("expected", decision["code"])
        # Target/profile/ABI rejection precedes the model's raw-ROM guard.
        # Only an otherwise compatible raw ROM needs the missing-link result.
        if expected == "none" and case["format"] != 2:
            expected = "unsupported_abi"
        if case in linked_cases:
            if decision["code"] != "none":
                raise ValueError("real ROM-link fixture rejected by runtime compatibility")
        else:
            admitted_cases.append({**case, "expected": expected, "runtime_code": decision["code"]})
    capabilities["decisions"] = list(decisions.values())
    (directory / "RUNTIME.JSON").write_text(json.dumps(capabilities, indent=2) + "\n")
    (directory / "ADMIT.JSON").write_text(json.dumps(admitted_cases, indent=2) + "\n")
    # The package-inspection corpus is unchanged; only admission uses the extras.
    external = {str(p): hashlib.sha256(p.read_bytes()).hexdigest()
                for p in sources if not p.is_relative_to(root)}
    (output / "oracle-system-headers.json").write_text(json.dumps(external, indent=2) + "\n")
    return directory, admitted_cases, {p for p in sources if p.is_relative_to(root)}
