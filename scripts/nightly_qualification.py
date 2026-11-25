#!/usr/bin/env python3
"""Qualify a single frozen main checkout and publish exact release selections."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile

import bundle
import core_dev
from core_dev_accept import candidate_arguments
import recipes


ROOT = Path(__file__).resolve().parents[1]


def encoded(value):
    return (json.dumps(value, sort_keys=True, separators=(",", ":")) + "\n").encode()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def read(path):
    return json.loads(path.read_bytes())


def write(path, data):
    """Replace only after the complete file is on disk, including across a crash."""
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as stream:
        name = Path(stream.name)
        try:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
            name.replace(path)
            sync_directory(path.parent)
        finally:
            name.unlink(missing_ok=True)


def sync_directory(path):
    fd = os.open(path, os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def git(*args):
    return subprocess.check_output(["git", "-C", ROOT, *args], text=True).strip()


def hex_digest(value):
    if not isinstance(value, str) or not re.fullmatch("[0-9a-f]{64}", value):
        raise ValueError("expected a SHA-256 identity")
    return value


def validate_release(release):
    if set(release) != {"board", "software_sha256", "cores"}:
        raise ValueError("release needs board, software_sha256 and ordered cores")
    hex_digest(release["software_sha256"])
    cores = release["cores"]
    if not isinstance(cores, list) or "fes.menu" not in cores or len(cores) != len(set(cores)):
        raise ValueError("release needs distinct cores including board MENU")
    for core in cores:
        recipes.recipe_for(core, board=release["board"])


class AwaitingEvidence(ValueError):
    pass


class IncompatibleSelection(ValueError):
    pass


class NoCompatibleSet(ValueError):
    pass


def record_verdict(store, verdict_path, evidence_path):
    """Import a trusted worker's verdict together with its retained evidence."""
    verdict = read(verdict_path)
    binding = hex_digest(verdict["request_sha256"])
    request = read(store / "requests" / binding)["request"]
    if digest(encoded(request)) != binding:
        raise ValueError("damaged qualification request")
    if verdict.get("verdict") not in ("PASS", "FAIL", "INCOMPATIBLE"):
        raise ValueError("invalid worker verdict")
    identity = hex_digest(verdict["evidence_sha256"])
    with tempfile.TemporaryDirectory(dir=store / "evidence") as tmp:
        copy = Path(tmp) / identity
        core_dev.snapshot(evidence_path, copy, expected=identity)
        with copy.open("rb") as stream:
            os.fsync(stream.fileno())
        copy.replace(store / "evidence" / identity)
        sync_directory(store / "evidence")
    write(store / "verdicts" / binding, encoded(verdict))


def verify_evidence(store, receipt):
    evidence = store / "evidence" / hex_digest(receipt.get("evidence_sha256"))
    core_dev.snapshot(evidence, expected=evidence.name)


def gate(store, executable, request, **locations):
    """Spool exact requests; synchronous farm drivers can answer inline."""
    binding = digest(encoded(request))
    write(store / "requests" / binding, encoded(dict(request=request, locations=locations)))
    if executable:
        result = subprocess.run([executable], input=encoded(request), stdout=subprocess.PIPE,
                                timeout=4 * 3600, check=True,
                                env=dict(os.environ, FES_NIGHTLY_STORE=str(store), **locations))
        receipt = json.loads(result.stdout)
    else:
        answer = store / "verdicts" / binding
        if not answer.is_file():
            raise AwaitingEvidence("awaiting farm verdict for " + binding)
        receipt = read(answer)
    if receipt.get("request_sha256") != binding:
        raise ValueError("farm verdict names a different request")
    verify_evidence(store, receipt)
    if receipt.get("verdict") == "INCOMPATIBLE" and "release" in request:
        raise IncompatibleSelection("farm rejected this release selection")
    if receipt.get("verdict") != "PASS":
        raise ValueError("farm gate failed for " + binding)
    return receipt


def tree_identity(root, flush=False):
    files = {}
    for path in sorted(root.rglob("*"), reverse=True):
        if path.is_symlink():
            raise ValueError("linked qualification object")
        if path.is_dir():
            if flush:
                sync_directory(path)
            continue
        if not path.is_file():
            raise ValueError("special file in qualification object")
        with path.open("rb") as stream:
            files[path.relative_to(root).as_posix()] = hashlib.file_digest(stream, "sha256").hexdigest()
            if flush:
                os.fsync(stream.fileno())
    if flush:
        sync_directory(root)
    return digest(encoded(files))


def cell_key(source, recipe):
    record = json.loads(bundle.canonical_package_record(
        source, env=recipes.producer_environment(recipe=recipe), recipe=recipe, selection_only=True))
    # The canonical reader already validated all identity projections. Recording
    # identity includes the commit; design, toolchain and evidence do not.
    identities = dict(record["identities"])
    del identities["record"]
    inputs = {"producer": identities, "product_options": recipe.producer_arguments,
              "profiles": recipe.video_profiles, "route": recipe.pnr, "quartus": recipe.quartus}
    if recipe.video_profiles:
        # Independent companion producers are outside the shell's source closure.
        inputs["companions"] = git("rev-parse", "HEAD:sources/misteross")
    return digest(encoded(inputs))


def materialize_menu(root, receipt):
    """Stage the two verified MENU files in image_artifacts.pack's input layout."""
    package = root / "menu-package"
    package.mkdir()
    expected = {"manifest.toml": receipt["selection"]["manifest_sha256"],
                "core.rbf": receipt["selection"]["payload_sha256"]}
    with tarfile.open(root / "core.fcore", "r:") as archive:
        for name, expected_sha in expected.items():
            member = archive.getmember(name)
            if not member.isfile() or not 0 < member.size <= core_dev.MAX_ARCHIVE_BYTES:
                raise ValueError("invalid MENU archive member")
            with archive.extractfile(member) as source:
                data = source.read()
            if digest(data) != expected_sha:
                raise ValueError("MENU member differs from its verified selection")
            path = package / name
            path.write_bytes(data)
            path.chmod(0o444)
    package.chmod(0o555)


def artifact_identity(prepared):
    return (prepared["package_id"], prepared["archive"]["sha256"],
            prepared.get("video_parts", {}).get("sha256"))


def qualify(store, sha, recipe, accept, key, previous):
    # Keep the prepared bytes for an offline HIL worker and retry an identical
    # input without moving the evidence request to the next day's main SHA.
    slot = digest(encoded([recipe.board, recipe.core_id, key]))
    candidate = store / "candidates" / slot
    if not candidate.exists():
        with tempfile.TemporaryDirectory(dir=candidate.parent) as tmp:
            root = Path(tmp) / "result"
            result = core_dev.prepare(recipe.core_id, root, board=recipe.board)
            if result["sources"]["misteross"] != sha or result["core_id"] != recipe.core_id:
                raise ValueError("producer selected a different core or revision")
            if recipe.core_id == "fes.menu":
                materialize_menu(root, result)
            # core_dev also stages an intermediate copy of linked video parts.
            intermediate = root / "resolved-video-parts"
            if intermediate.exists():
                core_dev.build._remove_sealed_tree(intermediate)
            (root / "simulations.json").write_bytes((store / "simulations" / sha).read_bytes())
            tree_identity(root, flush=True)
            root.rename(candidate)
            sync_directory(candidate.parent)
    prepared = candidate / "prepared.json"
    provenance = {}
    candidate_arguments(prepared, provenance)
    result = read(prepared)
    if result["core_id"] != recipe.core_id:
        raise ValueError("prepared candidate names a different core")
    if previous:
        prior = store / "objects" / hex_digest(previous)
        if artifact_identity(read(prior / "prepared.json")) == artifact_identity(result):
            if tree_identity(prior) != previous:
                raise ValueError("damaged previous qualification")
            verify_evidence(store, read(prior / "qualification.json")["receipt"])
            return previous
    request = dict(main=result["sources"]["misteross"], board=recipe.board, core=recipe.core_id,
                   package_id=result["package_id"], prepared_sha256=provenance["prepared_sha256"],
                   archive_sha256=provenance["archive_sha256"], inputs=key,
                   simulations_sha256=digest((candidate / "simulations.json").read_bytes()))
    receipt = gate(store, accept, request, FES_NIGHTLY_PREPARED=str(prepared))
    allowed = ["A", "B"] if recipe.board == "de10-nano" else ["TRWU8J8G"]
    if recipe.board == "de10-nano" and recipe.core_id == "fes.pc386":
        allowed.append("N/A")
    if receipt.get("kit") not in allowed or (
            receipt["kit"] != "N/A" and receipt.get("restored") is not True):
        raise ValueError("missing designated-kit acceptance and restore")
    candidate_arguments(prepared)
    if digest(prepared.read_bytes()) != request["prepared_sha256"]:
        raise ValueError("prepared identity changed during acceptance")
    with tempfile.TemporaryDirectory(dir=store / "objects") as tmp:
        root = Path(tmp) / "result"
        shutil.copytree(candidate, root, symlinks=True)
        (root / "qualification.json").write_bytes(encoded(dict(request=request, receipt=receipt)))
        identity = tree_identity(root, flush=True)
        target = store / "objects" / identity
        if target.exists():
            if tree_identity(target) != identity:
                raise ValueError("damaged retained qualification object")
        else:
            root.rename(target)
            sync_directory(target.parent)
        return identity


def selection(store, release, objects):
    rows = []
    for core in release["cores"]:
        identity = hex_digest(objects[core])
        root = store / "objects" / identity
        if not root.is_dir() or tree_identity(root) != identity:
            raise ValueError("missing or corrupt qualified object " + identity)
        prepared = root / "prepared.json"
        provenance = {}
        candidate_arguments(prepared, provenance)
        qualification = read(root / "qualification.json")
        q = qualification["request"]
        verify_evidence(store, qualification["receipt"])
        if (q["board"], q["core"], q["package_id"], q["prepared_sha256"]) != (
                release["board"], core, provenance["package_id"], provenance["prepared_sha256"]):
            raise ValueError("retained qualification and package identities differ")
        rows.append(dict(core=core, object=identity, package_id=q["package_id"],
                         main=q["main"], prepared=str(prepared.relative_to(store))))
    return dict(format=1, release=release, packages=rows)


def bootstrap_admission(store, selection, bootstrap):
    """Use the Linux image packer's admission check on the selected MENU bytes."""
    import image_artifacts
    release = selection["release"]
    if release["cores"] != ["fes.menu"]:
        raise ValueError("bootstrap admission covers MENU only")
    manifest_path = bootstrap / "artifacts.json"
    core_dev.snapshot(manifest_path, expected=release["software_sha256"])
    manifest, _ = image_artifacts.load(bootstrap, board=release["board"])
    menu = (store / selection["packages"][0]["prepared"]).parent / "menu-package"
    try:
        image_artifacts.check_menu_admission(bootstrap / "bin/mister-runtime", menu, release["board"])
    except ValueError as error:
        raise IncompatibleSelection(str(error)) from error
    core_dev.snapshot(manifest_path, expected=release["software_sha256"])
    core_dev.snapshot(bootstrap / "bin/mister-runtime", expected=manifest["binaries"]["mister-runtime"])
    report = encoded(dict(check="linux-bootstrap-menu", selection=selection,
                          runtime_sha256=manifest["binaries"]["mister-runtime"]))
    identity = digest(report)
    write(store / "evidence" / identity, report)
    return dict(verdict="PASS", request_sha256=digest(encoded(selection)), evidence_sha256=identity)


def release_set(store, release, candidates, admit, bootstrap=None):
    """Try complete qualified snapshots from newest to oldest for this board."""
    waiting = []
    attempted = set()
    for candidate in candidates:
        if not all(core in candidate for core in release["cores"]):
            continue
        key = tuple(candidate[core] for core in release["cores"])
        if key in attempted:
            continue
        attempted.add(key)
        lock = selection(store, release, candidate)
        try:
            if bootstrap is None:
                lock["admission"] = gate(store, admit, lock)
            else:
                lock["admission"] = bootstrap_admission(store, lock, Path(bootstrap))
        except IncompatibleSelection:
            continue
        except AwaitingEvidence as error:
            waiting.append(str(error))
            continue
        except subprocess.CalledProcessError as error:
            if error.returncode == 3:  # Verified incompatibility. Infrastructure errors propagate.
                continue
            raise
        raw = encoded(lock)
        identity = digest(raw)
        path = store / "sets" / identity
        if path.exists():
            load_lock(store, identity)
        else:
            write(path, raw)
        return identity
    if waiting:
        raise AwaitingEvidence("; ".join(waiting))
    raise NoCompatibleSet("no complete compatible qualified set")


def load_lock(store, identity):
    raw = (store / "sets" / hex_digest(identity)).read_bytes()
    if digest(raw) != identity:
        raise ValueError("corrupt immutable release set")
    return json.loads(raw)


def record_simulations(store, sha, receipt):
    path = store / "simulations" / sha
    value = read(receipt or path)
    if (value.get("main") != sha or value.get("full_simulations_and_producers_sha") != sha or
            value.get("pcxt_com") != "success" or
            not re.fullmatch(r"https://github\.com/DeanoC/fes/actions/runs/[0-9]+/attempts/[0-9]+",
                             value.get("run_url", ""))):
        raise ValueError("missing full simulation evidence for the frozen main SHA")
    if not path.exists():
        write(path, encoded(value))


def batch(store, sha, config, simulations=None):
    for key in ("accept", "admit"):
        if key not in config:
            continue
        path = Path(config[key])
        if not path.is_absolute() or not path.is_file() or not os.access(path, os.X_OK):
            raise ValueError("configure the trusted farm executable: " + key)
    if not isinstance(config["releases"], dict) or not config["releases"]:
        raise ValueError("no release contracts configured")
    for release in config["releases"].values():
        validate_release(release)
    if (not re.fullmatch("[0-9a-f]{40}", sha) or git("rev-parse", "HEAD") != sha or
            git("status", "--porcelain", "--untracked-files=normal")):
        raise ValueError("qualification requires a clean checkout of the frozen SHA")
    subprocess.run(["git", "-C", ROOT, "merge-base", "--is-ancestor", sha, "origin/main"], check=True)
    if store.is_relative_to(ROOT):
        raise ValueError("keep the durable qualification store outside the checkout")
    for name in ("sets", "objects", "candidates", "requests", "verdicts", "evidence", "simulations"):
        (store / name).mkdir(parents=True, exist_ok=True)
    with (store / "lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        record_simulations(store, sha, simulations)
        state_path = store / "state.json"
        state = read(state_path) if state_path.exists() else dict(cells={}, releases={})
        if state.get("main"):
            subprocess.run(["git", "-C", ROOT, "merge-base", "--is-ancestor", state["main"], sha], check=True)
        state["main"] = sha
        source = ROOT / "sources/misteross"
        failed = False
        registry = {board: recipes.load_recipes(board=board) for board in recipes.BOARDS}
        for board, registered in registry.items():
            for core, recipe in registered.items():
                name = board + "/" + core
                cell = state["cells"].setdefault(name, {})
                try:
                    key = cell_key(source, recipe)
                    if key == cell.get("key") and "pending" not in cell:
                        continue
                    identity = qualify(store, sha, recipe, config.get("accept"), key, cell.get("object"))
                    state["cells"][name] = dict(key=key, object=identity, main=sha,
                                                previous_main=cell.get("main"))
                    print(f"{name}: qualified {identity}", flush=True)
                except (ValueError, OSError, RuntimeError, subprocess.SubprocessError) as error:
                    cell["pending"] = dict(main=sha, error=str(error))
                    print(f"{name}: {error}", file=sys.stderr, flush=True)
                    failed = True
                write(state_path, encoded(state))
        boards = state.setdefault("boards", {})
        for board in recipes.BOARDS:
            current = {}
            for core in registry[board]:
                cell = state["cells"].get(board + "/" + core, {})
                if "object" in cell:
                    current[core] = cell["object"]
            history = boards.setdefault(board, [])
            if current and (not history or current != history[-1]):
                history.append(current)
        for name, release in config["releases"].items():
            bootstrap = config.get("linux_bootstrap_artifacts", {}).get(name)
            track = state["releases"].setdefault(name, {"history": []})
            try:
                identity = release_set(store, release, reversed(boards[release["board"]]),
                                       config.get("admit"), bootstrap)
                if not track["history"] or track["history"][-1] != identity:
                    track["history"].append(identity)
                track["current"] = identity
                print(f"{name}: {store / 'sets' / identity}", flush=True)
                track.pop("pending", None)
            except (ValueError, OSError, RuntimeError, subprocess.SubprocessError) as error:
                track["pending"] = dict(main=sha, release=release, error=str(error),
                                        incompatible=isinstance(error, NoCompatibleSet))
                print(f"{name}: {error}", file=sys.stderr, flush=True)
                failed = True
            write(state_path, encoded(state))
        return 1 if failed else 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--store", type=Path, required=True)
    commands = parser.add_subparsers(dest="command", required=True)
    run = commands.add_parser("run")
    run.add_argument("--sha", required=True)
    run.add_argument("--simulations", type=Path, help="trusted nightly CI receipt; retries use the stored receipt")
    run.add_argument("--config", type=Path, required=True)
    record = commands.add_parser("record")
    record.add_argument("--verdict", required=True, type=Path)
    record.add_argument("--evidence", required=True, type=Path)
    select = commands.add_parser("select")
    select.add_argument("--release", required=True)
    select.add_argument("--software-sha256", required=True)
    select.add_argument("--catalog-output", type=Path)
    args = parser.parse_args()
    store = args.store.resolve()
    if args.command == "run":
        return batch(store, args.sha, read(args.config), args.simulations)
    if args.command == "record":
        record_verdict(store, args.verdict, args.evidence)
        return 0
    track = read(store / "state.json")["releases"][args.release]
    if "current" not in track:
        raise ValueError("release has no published qualified set")
    identity = track["current"]
    lock = load_lock(store, identity)
    if lock["release"]["software_sha256"] != hex_digest(args.software_sha256):
        raise ValueError("selection qualified for different release software")
    pending = track.get("pending")
    if pending and pending["release"]["software_sha256"] == args.software_sha256 and (
            pending["incompatible"] or pending["release"] != lock["release"]):
        raise ValueError("release selection unavailable: " + pending["error"])
    verify_evidence(store, lock["admission"])
    verified = selection(store, lock["release"], {row["core"]: row["object"] for row in lock["packages"]})
    if verified["packages"] != lock["packages"]:
        raise ValueError("qualified packages differ from release selection")
    if args.catalog_output:
        from core_catalog import publish
        prepared = {row["core"]: store / row["prepared"] for row in verified["packages"]
                    if row["core"] != "fes.menu"}
        publish(ROOT, ROOT / "config/core-library.toml", prepared, args.catalog_output,
                board=lock["release"]["board"])
    print(store / "sets" / identity)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (KeyError, ValueError, OSError, subprocess.SubprocessError) as error:
        sys.exit("nightly qualification: " + str(error))
