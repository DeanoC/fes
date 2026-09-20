#!/usr/bin/env python3
"""Run affected software tests; --plan-only prints the exact plan without running tests."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

import affected


def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args],
                                   env=dict(os.environ, GIT_OPTIONAL_LOCKS="0", GIT_NO_LAZY_FETCH="1"))


def revision(root, ref):
    return git(root, "rev-parse", "--verify", "--end-of-options", ref + "^{commit}").decode().strip()


def plan(root, base, head="HEAD", jobs=2):
    root = Path(root).resolve()
    if git(root, "rev-parse", "--show-toplevel").decode().strip() != str(root):
        raise ValueError("root must be the FES Git checkout root")
    if type(jobs) is not int or not 1 <= jobs <= 32:
        raise ValueError("jobs must be between 1 and 32")
    selected, current = revision(root, head), revision(root, "HEAD")
    new_branch = base == "0" * 40
    base_commit = None if new_branch else revision(root, base)
    ancestor = None if new_branch else git(root, "merge-base", base_commit, selected).decode().strip()
    paths = ["<new-branch>"] if new_branch else affected.changed_paths(root, base_commit, selected)
    local = []
    if selected == current:
        raw = (git(root, "diff", "--name-only", "--no-renames", "-z", "HEAD", "--") +
               git(root, "ls-files", "--others", "--exclude-standard", "-z"))
        local = sorted({path.decode("utf-8", "surrogateescape") for path in raw.split(b"\0") if path})
        paths += local
    impact = affected.plan(paths)
    commands = []
    def add(lane, label, cwd, argv, tools=(), files=()):
        commands.append({"lane": lane, "label": label, "cwd": cwd, "argv": argv,
                         "tools": sorted(set([argv[0], *tools])), "files": list(files)})
    for pattern in ("test_affected.py", "test_test_changed.py"):
        add("always", pattern, ".", [sys.executable, "-m", "unittest", "discover", "-s", "tests", "-p", pattern, "-v"],
            files=["tests/" + pattern])
    check = [sys.executable, "scripts/check_diff.py", "--base", base if new_branch else ancestor, "--head", selected]
    add("always", "committed whitespace", ".", check)
    if selected == current:
        add("always", "working whitespace", ".", ["git", "diff", "--check", "HEAD", "--"])
    if impact["lanes"]["parent"]:
        add("parent", "parent Python regressions", ".", [sys.executable, "-m", "unittest", "discover", "-s", "tests", "-v"], files=["tests"])
        add("parent", "working-tree generated consumer consistency", ".",
            [sys.executable, "-c", "import sys; from pathlib import Path; sys.path.insert(0, 'scripts'); from consistency import check; print(check(Path('.')))"],
            tools=["go"], files=["scripts/consistency.py"])
    if impact["lanes"]["host"]:
        host = affected.MODULE_ROOTS["host"]
        for suffix in ("", "/appliance"):
            add("host", "Go tests " + (suffix or "host"), host + suffix,
                ["go", "test", "-race", "./..."], tools=["cc"], files=["go.mod"])
        add("host", "host UI tests", host, ["make", "test-ui"], tools=["node", "sh", "rg"], files=["Makefile"])
    if impact["lanes"]["runtime"]:
        add("runtime", "runtime software tests", affected.MODULE_ROOTS["runtime"],
            ["make", "-j" + str(jobs), "test"], tools=["c++", "ar", "nm", "c++filt", "sh", "rg"], files=["Makefile"])
    if impact["lanes"]["contracts"]:
        add("contracts", "contract schemas and generated fixtures", affected.MODULE_ROOTS["contracts"],
            ["make", "test", "PYTHON=" + sys.executable], tools=["go"], files=["Makefile"])
    if impact["lanes"]["fpga"]:
        fpga = affected.MODULE_ROOTS["fpga"]
        for pattern in ("test_build_fes_*.py", "test_functional_identity.py", "test_export_core_package.py",
                        "test_compiler_read_audit.py",
                        "test_legacy_source.py", "test_source_repository.py",
                        "test_core_package.py", "test_search_placer_qor.py"):
            add("fpga", pattern, fpga,
                [sys.executable, "-m", "unittest", "discover", "-s", "tests", "-p", pattern, "-v"],
                tools=["strace"] if pattern == "test_compiler_read_audit.py" else [],
                files=["tests/" + pattern])
        for core in impact["cores"]:
            add("fpga", core + " RTL simulation", fpga,
                ["make", "sim-fes-" + core, "PYTHON=" + sys.executable],
                tools=["verilator", "c++"], files=["Makefile"])
            if core in ("sg1000", "sms"):
                add("fpga", core + " OSS RTL simulation", fpga,
                    ["make", "sim-fes-" + core + "-oss", "PYTHON=" + sys.executable],
                    tools=["verilator", "c++"], files=["Makefile"])
    return {"format": 1, "root": str(root), "base": base_commit, "head": selected,
            "checkout_head": current, "merge_base": ancestor, "local_paths": local,
            "impact": impact, "commands": commands,
            "limits": [*impact["not_run"], "platform/image packaging suite", "required browser integration suite",
                       "committed-source/release validation (make check remains separate)"],
            "status": "planned"}


def preflight(result):
    missing = []
    for command in result["commands"]:
        cwd = Path(result["root"]) / command["cwd"]
        if not cwd.is_dir():
            missing.append("directory " + str(cwd))
        for name in command["files"]:
            if not any(cwd.glob(name)):
                missing.append("input " + str(cwd / name))
        for executable in command["tools"]:
            if shutil.which(executable) is None:
                missing.append("command " + executable)
    if any(command['lane'] == 'contracts' for command in result['commands']):
        for module in ('jsonschema', 'rfc3986_validator'):
            if importlib.util.find_spec(module) is None:
                missing.append('Python module ' + module + '; activate a test virtual environment and run: '
                               'python -m pip install -r sources/mister-packages/requirements-test.txt')
    if missing:
        raise ValueError("missing prerequisites: " + "; ".join(sorted(set(missing))))


def execute(result):
    if result["head"] != result["checkout_head"]:
        raise ValueError("execution requires --head to be checkout HEAD; use --plan-only for another revision")
    if revision(result["root"], "HEAD") != result["head"]:
        raise ValueError("checkout HEAD changed after planning")
    preflight(result)
    environment = dict(os.environ)
    # These are native software tests even when the shell was used to cross-build.
    for key in ("GOOS", "GOARCH", "GOARM"):
        environment.pop(key, None)
    result["results"] = []
    for command in result["commands"]:
        completed = subprocess.run(command["argv"], cwd=Path(result["root"]) / command["cwd"],
                                   env=environment, stdout=sys.stderr, stderr=sys.stderr)
        result["results"].append({"label": command["label"], "lane": command["lane"], "returncode": completed.returncode})
        if completed.returncode:
            result["status"] = "failed"
            result["not_started"] = [step["label"] for step in result["commands"][len(result["results"]):]]
            return False
    result["status"] = "passed"
    return True


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", required=True, help="comparison ref; changes are relative to its merge base")
    parser.add_argument("--head", default="HEAD")
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--jobs", type=int, default=2)
    parser.add_argument("--plan-only", action="store_true")
    args = parser.parse_args(argv)
    try:
        result = plan(args.root, args.base, args.head, args.jobs)
        success = True if args.plan_only else execute(result)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print(json.dumps({"format": 1, "status": "error", "error": str(error)}))
        return 2
    print(json.dumps(result, indent=2))
    return 0 if success else 1


if __name__ == "__main__":
    raise SystemExit(main())
