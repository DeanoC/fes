#!/usr/bin/env python3
"""Admit a DE10 Linux MENU/playable set exercised with the release binaries."""
import json
import os
from pathlib import Path
import sys
import tempfile

# The protected job invokes adapters in its frozen checkout.
sys.path.insert(0, str(Path.cwd() / "scripts"))
from nightly_qualification import bootstrap_admission, digest, encoded, read, write
from core_catalog import publish


def admit(selection, store):
    release = selection["release"]
    board = release["board"]
    if board != "de10-nano" or release["cores"] != ["fes.menu", "fes.zx81"]:
        raise ValueError("no playable release driver configured for this scope")
    software = store.parent / "software" / release["software_sha256"]
    menu, playable = selection["packages"]
    # Reuse the image assembler's exact ARM-runtime/MENU check.
    scope = dict(release, cores=["fes.menu"])
    bootstrap_admission(store, dict(release=scope, packages=[menu]), software)
    expected = read(software / "artifacts.json")["binaries"]
    receipt = read(store / playable["prepared"]).get("package_id")
    if receipt != playable["package_id"]:
        raise ValueError("playable selection differs from the producer receipt")
    proof = read((store / playable["prepared"]).parent / "qualification.json")["receipt"]
    raw = (store / "evidence" / proof["evidence_sha256"]).read_bytes()
    if digest(raw) != proof["evidence_sha256"]:
        raise ValueError("damaged playable kit evidence")
    evidence = json.loads(raw)
    if (evidence["request"]["package_id"] != playable["package_id"] or
            evidence["smoke"]["package_id"] != playable["package_id"] or
            evidence["request_sha256"] != proof["request_sha256"] or evidence["restored"] is not True):
        raise ValueError("playable kit evidence does not identify the selected package")
    lines = (evidence.get("software") or "").splitlines()
    observed = {name: value for value, name in (s.split() for s in lines if len(s.split()) == 2)}
    required = dict(expected, **{"active-" + k: expected[k] for k in ("mister-runtime", "fogcast-tenfoot")})
    menu_args = ["--menu-package", "/usr/share/mister-runtime/core-packages/" + menu["package_id"],
                 menu["package_id"]]
    if any(observed.get(name) != sha for name, sha in required.items()) or lines[-3:] != menu_args:
        raise SystemExit(4)  # A release-matched kit run is still needed.
    with tempfile.TemporaryDirectory() as tmp:
        catalog = publish(Path.cwd(), Path("config/core-library.toml"),
                          {playable["core"]: store / playable["prepared"]}, Path(tmp) / "catalog", board=board)
    report = encoded(dict(selection=selection, kit_evidence=proof["evidence_sha256"],
                          catalog_sha256=catalog["catalog_sha256"]))
    identity = digest(report)
    write(store / "evidence" / identity, report)
    return dict(verdict="PASS", request_sha256=digest(encoded(selection)), evidence_sha256=identity)


if __name__ == "__main__":
    print(json.dumps(admit(json.load(sys.stdin), Path(os.environ["FES_NIGHTLY_STORE"]))))
