"""Generate trusted model policy from the production C++ compatibility checker.

The JSON is a runner-owned fixture, not signed admission or a physical backend.
"""
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
    admitted_cases = []
    decisions = {}
    for n, case in enumerate(cases):
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
        expected = case.get("expected", "unsupported_abi" if case["format"] != 2 or case["name"].endswith("valid-unknown-abi") else "none")
        admitted_cases.append({**case, "expected": expected, "runtime_code": decision["code"]})
    capabilities["decisions"] = list(decisions.values())
    (directory / "RUNTIME.JSON").write_text(json.dumps(capabilities, indent=2) + "\n")
    (directory / "ADMIT.JSON").write_text(json.dumps(admitted_cases, indent=2) + "\n")
    # The package-inspection corpus is unchanged; only admission uses the extras.
    external = {str(p): hashlib.sha256(p.read_bytes()).hexdigest()
                for p in sources if not p.is_relative_to(root)}
    (output / "oracle-system-headers.json").write_text(json.dumps(external, indent=2) + "\n")
    return directory, admitted_cases, {p for p in sources if p.is_relative_to(root)}
