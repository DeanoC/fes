#!/usr/bin/env python3
"""Accept a digest-bound request from the protected qualification store on chip."""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import tarfile
import tomllib

BASE = Path.home() / ".local/share/fes-qualification"
FARM = "fes-build-3090"
STORE = "/home/deano/.local/share/fes-qualification/store"
QUEUE = Path.home() / "lander-hilq"
REPO = Path.home() / "fes"
sys.path.insert(0, str(REPO / "scripts"))
from core_dev_accept import candidate_arguments


def canonical(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()


def sha(data):
    return hashlib.sha256(data).hexdigest()


def fetch(relative):
    return subprocess.check_output(["ssh", "-oBatchMode=yes", FARM, "cat", STORE + "/" + relative])


def stage_hil_check(head, policy_head, hil, root):
    if not hil["check"]:
        return None
    if hil["check"] != "scripts/pcxt_dos_hil_acceptance.py":
        raise ValueError("no capture adapter for registered HIL check " + hil["check"])
    # Run the checker with the CGA font lock and bytes from the same main tree.
    firmware = "sources/misteross/cores/fes-pcxt/firmware/"
    for name in (hil["check"], firmware + "hardware-dos.json", firmware + "cga-font-8x8.bin"):
        dest = root / name
        dest.parent.mkdir(parents=True, exist_ok=True)
        revision = policy_head if name == hil["check"] else head
        dest.write_bytes(subprocess.check_output(["git", "-C", REPO, "show", revision + ":" + name]))
    checker = root / hil["check"]
    if sha(checker.read_bytes()) != hil["sha256"]:
        raise ValueError("registered HIL checker differs from request")
    return checker


def accept(binding):
    if not re.fullmatch("[a-f0-9]{64}", binding):
        raise ValueError("invalid request digest")
    req = json.loads(fetch("requests/" + binding))["request"]
    if sha(canonical(req)) != binding:
        raise ValueError("request digest mismatch")
    head, board, core = req["main"], req["board"], req["core"]
    if not re.fullmatch("[a-f0-9]{40}", head) or board not in ("de10-nano", "de25-nano"):
        raise ValueError("invalid main SHA or board")
    subprocess.run(["git", "-C", REPO, "fetch", "-q", "origin", "main"], check=True)
    subprocess.run(["git", "-C", REPO, "merge-base", "--is-ancestor", head, "origin/main"], check=True)
    folder = BASE / "runs" / binding
    folder.mkdir(parents=True, exist_ok=True)
    with (folder / "lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        report = folder / "pass.json"
        if report.exists():
            return report.read_bytes()
        if len(list(folder.glob("*/failed"))) >= 2:
            raise ValueError("two failed kit attempts require operator investigation")
        slot = sha(canonical([board, core, req["inputs"]]))
        build = folder / ("main-" + head[:8])
        if not build.exists():
            with tempfile.TemporaryDirectory(dir=folder) as staging:
                copy = Path(staging) / "candidate"
                subprocess.run(["rsync", "-a", "--no-links", FARM + ":" + STORE + "/candidates/" + slot + "/", copy], check=True)
                copy.rename(build)
        provenance = {}
        candidate_arguments(build / "prepared.json", provenance)
        prepared = json.loads((build / "prepared.json").read_bytes())
        if (prepared["sources"]["misteross"] != head or
                any(req[k] != provenance[k] for k in ("package_id", "prepared_sha256", "archive_sha256")) or
                req["core"] != prepared["core_id"] or
                sha((build / "simulations.json").read_bytes()) != req["simulations_sha256"]):
            raise ValueError("kit inputs differ from the nightly request")
        archive = build / (req["package_id"] + ".fcore")
        with tarfile.open(build / "core.fcore", "r:") as tar:
            manifest = tomllib.loads(tar.extractfile("manifest.toml").read(65536).decode())
        if (manifest["target"]["platform"], manifest["core"]["id"]) != (board.replace("-", "_"), core):
            raise ValueError("sealed core targets a different cell")
        simulation = json.loads((build / "simulations.json").read_bytes())
        url = re.fullmatch(r"https://github\.com/DeanoC/fes/actions/runs/([0-9]+)/attempts/([0-9]+)",
                           simulation["run_url"])
        if not url or simulation.get("main") != head:
            raise ValueError("missing main nightly simulation receipt")
        path = f"repos/DeanoC/fes/actions/runs/{url[1]}/attempts/{url[2]}"
        workflow = json.loads(subprocess.check_output(["gh", "api", path]))
        if (workflow["head_sha"], workflow["head_branch"], workflow["path"]) != (
                head, "main", ".github/workflows/nightly.yml") or workflow["event"] not in ("schedule", "workflow_dispatch"):
            raise ValueError("source does not match the main nightly workflow")
        jobs = subprocess.check_output(["gh", "api", "--paginate", path + "/jobs?per_page=100",
                                       "--jq", '.jobs[] | select(.name=="qualification") | .status'])
        if jobs.strip() not in (b"in_progress", b"completed"):
            raise ValueError("main nightly has not reached hardware qualification")
        if board == "de10-nano" and core == "fes.pc386" and req["hil"]["kits"] == ["N/A"] and not req["hil"]["check"]:
            return canonical(dict(request=req, request_sha256=binding, kit="N/A", restored=False,
                                  smoke=dict(verdict="PASS", disposition="DE10 pc386 has no designated kit"),
                                  simulations=simulation))
        if not archive.exists():
            os.link(build / "core.fcore", archive)
        if not archive.samefile(build / "core.fcore"):
            raise ValueError("staged archive differs from prepared input")
        run = Path(tempfile.mkdtemp(dir=folder))
        line = f"DONE {archive} {req['archive_sha256']}\n"
        (build / "build.result").write_text(line)
        if board == "de25-nano":
            (build / "core-build.result").write_text(line)
        checker = stage_hil_check(head, req.get("policy_main", head), req["hil"], run / "checker")
        for kit_name in req["hil"]["kits"]:
            kit = "D25" if kit_name == "TRWU8J8G" else kit_name
            if kit not in (["D25"] if board == "de25-nano" else ["B", "A"]):
                raise ValueError("registered kit incompatible with board")
            ledger = "de25-TRWU8J8G-STATUS.md" if kit == "D25" else f"kit{kit}-STATUS.md"
            entries = (Path.home() / "kit-status" / ledger).read_text().splitlines()
            status = next((s for s in reversed(entries) if s.startswith("## ")), "")
            if f"KIT {kit} FREE" not in status:
                continue
            evidence = run / kit
            evidence.mkdir()
            env = dict(os.environ, HIL_KIND="core", LW_REPO="DeanoC/fes", LW_PR="main", LW_HEAD=head)
            worker = QUEUE / ("hil-de25-watch.sh" if kit == "D25" else "hil-core.sh")
            if checker:
                env["C_SERIES"] = "6:8"
                env["FES_NIGHTLY_CHECKER"] = str(checker)
            if kit != "D25":
                # Keep HILQ's baseline, locks, host isolation and restore. Substitute
                # only the run-local smoke entry point to observe release identities.
                overlay = evidence / "hilq"
                overlay.mkdir()
                for name in ("current.json", "kits", "host-golden", "hil_host.py", "hil_preflight.py"):
                    (overlay / name).symlink_to(QUEUE / name)
                (overlay / "hilcore.py").symlink_to(Path(__file__).with_name("nightly-de10-smoke.py").resolve())
                env["HILQ_HOME"] = str(overlay)
                env["FES_NIGHTLY_CORE"] = core
            with (evidence / "worker.log").open("wb") as log:
                result = subprocess.run([worker, kit, "main", head, build, evidence], env=env,
                                        stdout=log, stderr=subprocess.STDOUT)
            verdict = (evidence / "verdict").read_text().strip()
            if verdict == "NOTSTARTED":
                continue
            if result.returncode or verdict != "PASS":
                (run / "failed").touch()
                raise ValueError(f"kit {kit} failed, see {evidence}")
            filename = "menu-result.json" if kit == "D25" and core == "fes.menu" else "result.json"
            smoke = json.loads((evidence / "out" / filename).read_bytes())
            if (smoke["verdict"], smoke["kit"], smoke["head"], smoke["package_id"]) != (
                    "PASS", kit, head, req["package_id"]):
                raise ValueError(f"kit result identity mismatch in {evidence}")
            if checker:
                smoke["hil_check"] = json.loads((evidence / "out/hil-check.json").read_bytes())
                smoke["hil_check_sha256"] = req["hil"]["sha256"]
            observed = evidence / "out/release-software.txt"
            raw = canonical(dict(software=observed.read_text() if observed.exists() else None, request_sha256=binding, request=req, smoke=smoke,
                                 kit="TRWU8J8G" if kit == "D25" else kit,
                                 restored=True, evidence_dir=str(evidence)))
            tmp = run / "pass.json"
            tmp.write_bytes(raw)
            tmp.replace(report)
            return raw
        raise ValueError("designated kits unavailable")


if __name__ == "__main__":
    os.umask(0o077)
    if sys.argv[1:] == ["--check"]:
        # Exercise both SSH directions and farm dependencies without claiming a kit.
        subprocess.run(["ssh", "-oBatchMode=yes", FARM, "test", "-d", STORE], check=True)
        for name in ("hil-core.sh", "hil-de25-watch.sh", "kits/kssh"):
            if not os.access(QUEUE / name, os.X_OK):
                sys.exit("HILQ executable missing: " + name)
        subprocess.run(["gh", "api", "--silent", "repos/DeanoC/fes/actions/workflows/nightly.yml"], check=True)
        print("Powerboat can read the runner store, access nightly CI and invoke both HILQ drivers")
    else:
        sys.stdout.buffer.write(accept(sys.argv[1]))
