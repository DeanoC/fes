"""The Quartus lease (scripts/quartus_tools.py) and the rule that every Quartus
invocation in the repository takes it.

The static half scans every tracked script for a leased Quartus program that
could run without the lease; each rule has a constructed counterexample here
that the scan must catch. The runtime half checks the slots, the inheritance
from flock(1) and from a leasing parent, and the audit hook that refuses an
unleased licensed Quartus program.
"""
import ast
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import textwrap
import time
import tomllib
import unittest
from unittest import mock

MISTEROSS = Path(__file__).resolve().parents[1]
REPO = MISTEROSS.parents[1]
sys.path.insert(0, str(MISTEROSS))

from scripts import quartus_tools as qt  # noqa: E402

# Quartus programs that need a slot. quartus_pfg, quartus_cpf and quartus_pgm
# convert or program an existing bitstream in seconds and a few hundred MB, so
# they are not leased (the same set quartus_tools.QUARTUS_PROGRAM leaves out).
LEASED = re.compile(r"\bquartus_(sh|map|syn|fit|sta|asm|eda|cdb|ipgenerate|npp|pow|drc|si|tan|dse|stp)\b(?!_)")
# A call to any locate_quartus: the repository's wrappers all delegate to
# quartus_tools.locate_quartus (checked below), which takes the lease.
LOCATES = re.compile(r"\blocate_quartus\(")
LAUNCH = re.compile(r"subprocess\.|\bPopen\(|os\.exec|os\.system\(|os\.spawn|os\.posix_spawn")
PROBE = re.compile(r"^\s*(--version|-v|--help|-h)\b")


# ---------------------------------------------------------------------------
# Static rule
# ---------------------------------------------------------------------------

def _module_level(tree: ast.AST):
    """The nodes that run when the module is imported: not function bodies,
    whose imports run only if the function is called."""
    pending = [tree]
    while pending:
        node = pending.pop()
        yield node
        for child in ast.iter_child_nodes(node):
            if not isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef, ast.Lambda)):
                pending.append(child)


def _imports(path: Path, roots: tuple[Path, ...]) -> set[Path]:
    """In-repo modules importing path imports, or loads by a literal file path."""
    try:
        tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
    except (SyntaxError, UnicodeDecodeError):
        return set()

    def module(dotted: str, base: tuple[Path, ...]) -> list[Path]:
        parts = dotted.split(".")
        found = []
        for root in base:
            for candidate in (root.joinpath(*parts).with_suffix(".py"), root.joinpath(*parts, "__init__.py")):
                if candidate.is_file():
                    found.append(candidate)
        return found

    # A module that loads files by path (importlib's spec_from_file_location,
    # runpy.run_path) may load any .py path literal it names outside a
    # docstring. A module that loads nothing by path imports only by name.
    loads_by_path = any(isinstance(node, ast.Attribute) and node.attr in ("spec_from_file_location", "run_path")
                        or isinstance(node, ast.Name) and node.id in ("spec_from_file_location", "run_path")
                        for node in ast.walk(tree))
    docstrings = {id(body[0].value) for body in
                  (getattr(node, "body", None) for node in ast.walk(tree))
                  if isinstance(body, list) and body and isinstance(body[0], ast.Expr)
                  and isinstance(body[0].value, ast.Constant)}
    result = set()
    for node in _module_level(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                result.update(module(alias.name, roots))
        elif isinstance(node, ast.ImportFrom):
            base = roots if node.level == 0 else (path.parents[node.level - 1],)
            if node.module:
                result.update(module(node.module, base))
            for alias in node.names:
                result.update(module(f"{node.module}.{alias.name}" if node.module else alias.name, base))
        elif (loads_by_path and isinstance(node, ast.Constant) and id(node) not in docstrings
              and isinstance(node.value, str) and node.value.endswith(".py")
              and len(node.value) < 200 and "\n" not in node.value):
            literal = node.value
            candidates = ([root / literal for root in roots] if "/" in literal
                          else [path.parent / literal, path.parent.parent / literal])
            result.update(candidate for candidate in candidates if candidate.is_file())
    return {candidate.resolve() for candidate in result}


# Files the rule cannot change without moving pinned bytes, each run only in a
# leased process. Every entry's pin is checked by PinnedTests, so the list
# cannot become a place to park an unleased script.
PINNED = {
    # Design (or packaging) inputs of registered DE25 cores: editing one moves
    # those cores' BUILD_ID (or package identity). Each runs quartus_sta on a
    # project its producer just compiled, in the producer's process, which
    # imported quartus_tools and holds the lease from locate_quartus. Run
    # standalone, review_audio_clocks.py's CLI goes under quartus_jobs.py run.
    "sources/misteross/boards/de25nano/producers/review_audio_clocks.py": "registered",
    "sources/misteross/boards/de25nano/scanout/async_inputs.py": "registered",
    "sources/misteross/boards/de25nano/scanout/output_reset.py": "registered",
    "sources/misteross/boards/de25nano/scanout/video_outputs.py": "registered",
    # Its sha256 is bound by the committed DE25 combined-proof receipt
    # (tests/fixtures/de25-combined-proof.json); editing it voids that proof.
    # Run it under quartus_jobs.py run --edition pro.
    "sources/misteross/boards/de25nano/link-proof/build.py": "receipt",
    # Names quartus_sh and the rest only to hash them; the only program it
    # starts is quartus_pfg, which takes no slot.
    "sources/misteross/prototypes/de25_board_control/seal.py": "hash-only",
}
RECEIPT = "sources/misteross/tests/fixtures/de25-combined-proof.json"


def unleased_quartus(root: Path, files: list[str], misteross: Path | None = None) -> list[str]:
    """Each tracked script that could run a leased Quartus program unleased.

    A Python module that starts processes and names a leased Quartus program
    must import scripts.quartus_tools, directly or through the in-repo modules
    it imports or loads by path: that import installs the audit hook which
    refuses any licensed Quartus program without a lease, and locate_quartus
    takes the lease. A shell script may run a leased Quartus program only as a
    `--version` probe or under `quartus_jobs.py run`.

    Out of scope: tests (they run stand-in programs) and docs/validation/
    (frozen evidence of past runs, never rerun as tooling)."""
    misteross = misteross or root / "sources/misteross"
    roots = (misteross, root)
    tools = (misteross / "scripts/quartus_tools.py").resolve()
    failures = []

    def leased(path: Path) -> bool:
        pending, seen = [path.resolve()], set()
        while pending:
            current = pending.pop()
            if current == tools:
                return True
            if current in seen:
                continue
            seen.add(current)
            pending.extend(_imports(current, roots))
        return False

    for name in files:
        if name.startswith("docs/validation/") or "/tests/" in f"/{name}":
            continue
        base = name.rsplit("/", 1)[-1]
        if base.startswith("test_") or base.endswith("_test.py") or name.endswith("scripts/quartus_tools.py"):
            continue
        path = root / name
        if not name.endswith((".py", ".sh")) or not path.is_file():
            continue
        text = path.read_text(encoding="utf-8", errors="replace")
        if not LEASED.search(text):
            continue
        if name.endswith(".py"):
            if name in PINNED and root == REPO:
                continue
            if LAUNCH.search(text) and not LOCATES.search(text) and not leased(path):
                failures.append(f"{name}: starts processes and names a Quartus program, "
                                "but never imports scripts.quartus_tools")
            continue
        for number, line in enumerate(text.splitlines(), 1):
            code = line.split("#", 1)[0]
            for match in LEASED.finditer(code):
                before, after = code[:match.start()], code[match.end():]
                if PROBE.match(after.lstrip('"\'')) or "quartus_jobs.py run" in before:
                    continue
                # A program name inside a quoted message or path test is not a run.
                if re.search(r"(\[|test|-[xfe]\s|echo|fail|grep)\s*[\"']?[^\"']*$", before):
                    continue
                failures.append(f"{name}:{number}: runs {match.group(0)} outside quartus_jobs.py run")
    return failures


class StaticRuleTests(unittest.TestCase):
    def test_every_quartus_invocation_in_the_repository_takes_the_lease(self):
        files = subprocess.check_output(["git", "-C", str(REPO), "ls-files"], text=True).split()
        self.assertEqual(unleased_quartus(REPO, files), [])

    def test_every_locate_quartus_delegates_to_the_leasing_one(self):
        # The rule accepts a call to any locate_quartus, so each definition
        # outside quartus_tools must call quartus_tools.locate_quartus.
        listed = subprocess.check_output(["git", "-C", str(REPO), "grep", "-l", "def locate_quartus", "--",
                                          ":!docs/validation"], text=True).split()
        self.assertIn("sources/misteross/scripts/quartus_tools.py", listed)
        for name in listed:
            if name.endswith("scripts/quartus_tools.py"):
                continue
            tree = ast.parse((REPO / name).read_text())
            for node in ast.walk(tree):
                if isinstance(node, ast.FunctionDef) and node.name == "locate_quartus":
                    calls = [n for n in ast.walk(node) if isinstance(n, ast.Call)
                             and isinstance(n.func, ast.Attribute) and n.func.attr == "locate_quartus"
                             and isinstance(n.func.value, ast.Name) and n.func.value.id == "quartus_tools"]
                    self.assertTrue(calls, f"{name}: locate_quartus does not call quartus_tools.locate_quartus")

    def scan(self, layout: dict[str, str]) -> list[str]:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "sources/misteross/scripts").mkdir(parents=True)
            (root / "sources/misteross/scripts/quartus_tools.py").write_text("")
            for name, text in layout.items():
                (root / name).parent.mkdir(parents=True, exist_ok=True)
                (root / name).write_text(textwrap.dedent(text))
            return unleased_quartus(root, list(layout))

    # The shape the repository actually uses: the command is a list built from
    # a located program and handed to a local run() helper.
    UNLEASED_MODULE = """
        import subprocess
        def run(command, cwd):
            return subprocess.run(command, cwd=cwd, check=True)
        def fit(tool, project):
            run([str(tool.with_name("quartus_fit")), project], project)
        """

    def test_counterexample_an_unleased_fit_helper_is_caught(self):
        failures = self.scan({"sources/misteross/boards/x/build.py": self.UNLEASED_MODULE})
        self.assertEqual(len(failures), 1)
        self.assertIn("boards/x/build.py", failures[0])

    def test_counterexample_own_discovery_without_locate_quartus_is_caught(self):
        failures = self.scan({"sources/misteross/scripts/probe.py": """
            import os, subprocess
            def compile_project(project):
                root = os.environ["QUARTUS_ROOTDIR"]
                subprocess.run([root + "/bin/quartus_sh", "--flow", "compile", "top"], cwd=project)
            """})
        self.assertEqual(len(failures), 1)

    def test_a_module_that_imports_the_lease_passes(self):
        self.assertEqual(self.scan({"sources/misteross/boards/x/build.py":
                                    "from scripts.quartus_tools import locate_quartus\n"
                                    + textwrap.dedent(self.UNLEASED_MODULE)}), [])

    def test_a_receiver_loaded_through_a_leasing_module_passes(self):
        self.assertEqual(self.scan({
            "sources/misteross/boards/x/helper.py": "from scripts import quartus_tools\n",
            "sources/misteross/boards/x/build.py":
                "import importlib.util\nspec = importlib.util.spec_from_file_location('h', 'helper.py')\n"
                + textwrap.dedent(self.UNLEASED_MODULE)}), [])

    def test_counterexample_naming_a_leasing_module_in_a_string_is_not_loading_it(self):
        failures = self.scan({
            "sources/misteross/boards/x/helper.py": "from scripts import quartus_tools\n",
            "sources/misteross/boards/x/build.py":
                "INPUTS = ('helper.py', 'scripts/quartus_tools.py')\n" + textwrap.dedent(self.UNLEASED_MODULE)})
        self.assertEqual(len(failures), 1)

    def test_a_cpf_conversion_alone_is_not_a_leased_program(self):
        self.assertEqual(self.scan({"sources/misteross/scripts/convert.py": """
            import subprocess
            subprocess.run(["quartus_cpf", "-c", "a.sof", "a.rbf"])
            """}), [])

    def test_counterexample_a_shell_compile_is_caught_and_a_version_probe_is_not(self):
        failures = self.scan({"scripts/job.sh": """
            #!/bin/bash
            "$QUARTUS_ROOTDIR/bin/quartus_sh" --version > version.txt
            "$QUARTUS_ROOTDIR/bin/quartus_sh" --flow compile top
            python3 scripts/quartus_jobs.py run --edition pro -- quartus_sh --flow compile top
            """})
        self.assertEqual(len(failures), 1)
        self.assertIn("scripts/job.sh:4", failures[0])


def registered_closure() -> set[Path]:
    """Every file a registered producer imports or loads by path, at any depth
    and from inside functions too (what its source closure can reach)."""
    recipes = tomllib.loads((REPO / "config/core-recipes.toml").read_text())
    producers = set()

    def collect(value):
        if isinstance(value, dict):
            for key, item in value.items():
                if key == "producer_script" and isinstance(item, str):
                    producers.add((MISTEROSS / item).resolve())
                collect(item)
        elif isinstance(value, list):
            for item in value:
                collect(item)

    collect(recipes)
    roots = (MISTEROSS, REPO)
    seen, pending = set(), [p for p in producers if p.is_file()]
    while pending:
        current = pending.pop()
        if current in seen:
            continue
        seen.add(current)
        tree = ast.parse(current.read_text(encoding="utf-8"))
        for node in ast.walk(tree):
            if isinstance(node, ast.Constant) and isinstance(node.value, str) and node.value.endswith(".py") \
                    and "\n" not in node.value and len(node.value) < 200:
                for candidate in ([r / node.value for r in roots] if "/" in node.value
                                  else [current.parent / node.value, current.parent.parent / node.value]):
                    if candidate.is_file():
                        pending.append(candidate.resolve())
        with mock.patch(f"{__name__}._module_level", ast.walk):
            pending.extend(_imports(current, roots))
    return seen


class PinnedTests(unittest.TestCase):
    def test_each_pinned_entry_is_still_pinned_and_still_needs_the_entry(self):
        closure = registered_closure()
        receipt = (REPO / RECEIPT).read_text()
        files = subprocess.check_output(["git", "-C", str(REPO), "ls-files"], text=True).split()
        for name, pin in PINNED.items():
            path = REPO / name
            if pin == "registered":
                self.assertIn(path.resolve(), closure, f"{name} is no longer in a registered closure")
            elif pin == "receipt":
                self.assertIn(hashlib.sha256(path.read_bytes()).hexdigest(), receipt, name)
            else:
                for line in path.read_text().splitlines():
                    if LEASED.search(line):
                        self.assertNotRegex(line, r"subprocess|\brun\(|Popen|with_name\([\"']", name)
            # A stale entry: the file would pass without it.
            with mock.patch.dict(PINNED, clear=True):
                self.assertTrue([f for f in unleased_quartus(REPO, [name]) if f.startswith(name)],
                                f"{name} passes the rule now; delete its PINNED entry")
        self.assertTrue(set(PINNED) <= set(files))


# ---------------------------------------------------------------------------
# Runtime lease
# ---------------------------------------------------------------------------

def child(code: str, pools: dict, **kwargs) -> subprocess.CompletedProcess:
    """Run code in a fresh interpreter whose lease pools are pools."""
    prelude = (f"import sys; sys.path.insert(0, {str(MISTEROSS)!r})\n"
               "from scripts import quartus_tools as qt\n"
               f"qt.LEASE_POOLS = {{}}; qt.DEFAULT_POOL = {pools!r}\n")
    return subprocess.run([sys.executable, "-c", prelude + textwrap.dedent(code)],
                          capture_output=True, text=True, timeout=60, **kwargs)


class LeaseTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        root = Path(self.directory.name)
        self.pools = {"pro": (str(root / "pro-1.lock"), str(root / "legacy-pro.lock")),
                      "lite": (str(root / "lite-1.lock"),)}
        patches = [mock.patch.object(qt, "LEASE_POOLS", {}), mock.patch.object(qt, "DEFAULT_POOL", self.pools),
                   mock.patch.dict(qt._process_held, clear=True)]
        for patch in patches:
            patch.start()
            self.addCleanup(patch.stop)

    def unrelated(self, code: str) -> Path:
        """Start code in a process that is not this one's descendant (setsid -f
        reparents it), as another owner's job would be. Returns its output."""
        output = Path(self.directory.name) / f"unrelated-{time.monotonic_ns()}.out"
        prelude = (f"import sys; sys.path.insert(0, {str(MISTEROSS)!r})\n"
                   "from scripts import quartus_tools as qt\n"
                   f"qt.LEASE_POOLS = {{}}; qt.DEFAULT_POOL = {self.pools!r}\n")
        script = Path(self.directory.name) / f"{output.stem}.py"
        script.write_text(prelude + textwrap.dedent(code))
        subprocess.run(["setsid", "-f", "sh", "-c", f'exec {shlex.quote(sys.executable)} '
                        f'{shlex.quote(str(script))} > {shlex.quote(str(output))} 2>&1'], check=True)
        return output

    def wait_for(self, output: Path, text: str, seconds: float = 20) -> str:
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            if output.is_file() and text in output.read_text():
                return output.read_text()
            time.sleep(0.05)
        self.fail(f"{text!r} not in {output.read_text() if output.is_file() else 'no output'}")

    def test_slots_are_taken_in_order_reentrantly_and_released(self):
        with qt.quartus_lease("pro") as first:
            self.assertEqual(first, self.pools["pro"][0])
            with qt.quartus_lease("pro") as again:
                self.assertEqual(again, first)
            other = self.unrelated("with qt.quartus_lease('pro') as path: print('took', path, flush=True)\n")
            self.assertIn(f"took {self.pools['pro'][1]}", self.wait_for(other, "took"))
            self.assertEqual(qt.held_quartus_lease("pro"), first)
        self.assertIsNone(qt.held_quartus_lease("pro"))
        self.assertEqual(qt.flock_holders(self.pools["pro"][0]), set())

    def test_a_full_pool_waits_and_slot_files_are_never_replaced(self):
        with qt.quartus_lease("lite"):
            inode = os.stat(self.pools["lite"][0]).st_ino
            waiter = self.unrelated("print('waiting', flush=True)\n"
                                    "with qt.quartus_lease('lite'): print('took', flush=True)\n")
            self.wait_for(waiter, "waiting")
            time.sleep(3)
            self.assertNotIn("took", waiter.read_text())
        self.wait_for(waiter, "took")
        self.assertEqual(os.stat(self.pools["lite"][0]).st_ino, inode)

    def test_a_child_of_a_leasing_process_holds_the_lease(self):
        with qt.quartus_lease("pro") as path:
            result = child("print(qt.held_quartus_lease('pro'))", self.pools)
        self.assertEqual(result.stdout.strip(), path, result.stderr)

    def test_flock1_around_a_job_is_its_lease_and_never_deadlocks(self):
        # Lander's seal-remote.sh shape: flock <legacy> bash <job> runs a
        # producer, which locates Quartus and must not wait on its own parent.
        legacy = self.pools["pro"][1]
        code = ("import sys; sys.path.insert(0, %r)\n"
                "from scripts import quartus_tools as qt\n"
                "qt.LEASE_POOLS = {}; qt.DEFAULT_POOL = {'pro': (%r,), 'lite': ()}\n"
                "with qt.quartus_lease('pro') as path: print(path)\n") % (str(MISTEROSS), legacy)
        result = subprocess.run(["flock", legacy, "bash", "-c", shlex.join([sys.executable, "-c", code])],
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.stdout.strip(), legacy, result.stderr)
        # Also when the descriptor did not survive (a close_fds launch): the
        # flock(1) process is still an ancestor holding the inode.
        closing = ("import subprocess, sys; raise SystemExit(subprocess.run([sys.executable, '-c', %r],"
                   " close_fds=True).returncode)") % code
        result = subprocess.run(["flock", "-o", legacy, sys.executable, "-c", closing],
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.stdout.strip(), legacy, result.stderr)

    def test_parallel_threads_each_hold_their_own_slot(self):
        import threading
        took, release = [], threading.Event()

        def seed():
            with qt.quartus_lease("pro") as path:
                took.append(path)
                release.wait(10)

        threads = [threading.Thread(target=seed) for _ in range(2)]
        for thread in threads:
            thread.start()
        deadline = time.monotonic() + 10
        while len(took) < 2 and time.monotonic() < deadline:
            time.sleep(0.05)
        self.assertEqual(sorted(took), sorted(self.pools["pro"]))
        # The main thread took no lease of its own.
        self.assertIsNone(qt.held_quartus_lease("pro"))
        release.set()
        for thread in threads:
            thread.join()

    def test_a_thread_never_mistakes_another_threads_descriptor_for_an_inherited_slot(self):
        # Codex P1: while one thread opens (or closes) its slot descriptors,
        # another thread's inherited-descriptor scan must not flock them.
        import threading
        pool = (self.pools["lite"][0],)
        stop, wrongly = threading.Event(), []

        def churn():
            while not stop.is_set():
                with qt.quartus_lease("lite"):
                    pass

        def scan():
            while not stop.is_set():
                if qt._inherited_slot(pool) is not None:
                    wrongly.append(1)

        threads = [threading.Thread(target=churn), threading.Thread(target=scan)]
        for thread in threads:
            thread.start()
        time.sleep(2)
        stop.set()
        for thread in threads:
            thread.join()
        self.assertEqual(wrongly, [])

    def test_an_unrelated_holder_is_not_this_process_lease(self):
        holder = subprocess.Popen(["flock", self.pools["lite"][0], "sleep", "30"])
        self.addCleanup(holder.wait)
        self.addCleanup(holder.terminate)
        deadline = time.monotonic() + 10
        while not qt.flock_holders(self.pools["lite"][0]) and time.monotonic() < deadline:
            time.sleep(0.05)
        self.assertTrue(qt.flock_holders(self.pools["lite"][0]))
        self.assertIsNone(qt.held_quartus_lease("lite"))


class LaunchGuardTests(unittest.TestCase):
    """The audit hook, against a stand-in installation registered as licensed."""

    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        root = Path(self.directory.name)
        self.bin = root / "quartus" / "bin"
        self.bin.mkdir(parents=True)
        for name in ("quartus_sh", "quartus_fit", "quartus_sta", "quartus_cpf"):
            program = self.bin / name
            program.write_text(f"#!/bin/sh\necho {name} \"$@\"\n")
            program.chmod(0o755)
        digest = hashlib.sha256((self.bin / "quartus_sh").read_bytes()).hexdigest()
        pools = {"pro": (str(root / "pro.lock"),), "lite": (str(root / "lite.lock"),)}
        for patch in (mock.patch.dict(qt.LICENSED_QUARTUS_SH, {digest: "lite"}),
                      mock.patch.object(qt, "LEASE_POOLS", {}), mock.patch.object(qt, "DEFAULT_POOL", pools),
                      mock.patch.dict(qt._process_held, clear=True), mock.patch.dict(qt._licensed_dirs, clear=True)):
            patch.start()
            self.addCleanup(patch.stop)

    def test_counterexample_an_unleased_licensed_fit_is_refused(self):
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run([str(self.bin / "quartus_fit"), "top"], capture_output=True)

    def test_wrappers_and_shell_strings_are_seen_through(self):
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["/bin/sh", "run_logged.sh", "log", str(self.bin / "quartus_sh"), "--flow", "compile"])
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(f"{self.bin}/quartus_sta -t gate.tcl", shell=True)
        with self.assertRaises(qt.QuartusLeaseError):
            os.system(f"{self.bin}/quartus_sta -t gate.tcl")

    def test_a_leased_fit_a_version_probe_and_a_conversion_run(self):
        self.assertEqual(subprocess.run([str(self.bin / "quartus_sh"), "--version"],
                                        capture_output=True).returncode, 0)
        self.assertEqual(subprocess.run([str(self.bin / "quartus_cpf"), "-c"], capture_output=True).returncode, 0)
        with qt.quartus_lease("lite"):
            self.assertEqual(subprocess.run([str(self.bin / "quartus_fit"), "top"],
                                            capture_output=True).returncode, 0)
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run([str(self.bin / "quartus_fit"), "top"], capture_output=True)

    def test_a_stand_in_installation_is_never_leased(self):
        with mock.patch.dict(qt.LICENSED_QUARTUS_SH, clear=True), mock.patch.dict(qt._licensed_dirs, clear=True):
            self.assertEqual(subprocess.run([str(self.bin / "quartus_fit"), "top"],
                                            capture_output=True).returncode, 0)

    def test_locate_quartus_takes_no_slot_for_a_stand_in_that_authenticates(self):
        # A test fixture whose lock names its stand-in's digest (or that
        # patches the digest check) authenticates, but is not a licensed
        # installation: it must not take a build host's slot. Before the fix a
        # full test run held Powerboat's /tmp/fes-quartus-pro-1.lock for its
        # whole duration.
        repo = Path(self.directory.name)
        (self.bin / "quartus_sh").write_text(
            "#!/bin/sh\necho 'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition'\n")
        digest = hashlib.sha256((self.bin / "quartus_sh").read_bytes()).hexdigest()
        lock = qt.QuartusLock("cores/x/quartus.lock", "lite",
                              "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition", "cyclonev", digest)
        with mock.patch.dict(qt.LICENSED_QUARTUS_SH, clear=True), mock.patch.dict(qt._licensed_dirs, clear=True):
            qt.locate_quartus(str(repo / "quartus"), repo, lock)
        self.assertIsNone(qt.held_quartus_lease("lite"))
        self.assertEqual(qt.flock_holders(qt.DEFAULT_POOL["lite"][0]), set())

    def test_locate_quartus_takes_the_lease_unless_one_is_held(self):
        repo = Path(self.directory.name)
        (self.bin / "quartus_sh").write_text(
            "#!/bin/sh\necho 'Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition'\n")
        digest = hashlib.sha256((self.bin / "quartus_sh").read_bytes()).hexdigest()
        qt.LICENSED_QUARTUS_SH[digest] = "lite"
        lock = qt.QuartusLock("cores/x/quartus.lock", "lite",
                              "Version 17.0.2 Build 602 07/19/2017 SJ Lite Edition", "cyclonev", digest)
        with qt.quartus_lease("lite") as path:
            qt.locate_quartus(str(repo / "quartus"), repo, lock)
            self.assertEqual(qt.held_quartus_lease("lite"), path)
        self.assertIsNone(qt.held_quartus_lease("lite"))
        qt.locate_quartus(str(repo / "quartus"), repo, lock)
        self.assertEqual(qt.held_quartus_lease("lite"), qt.DEFAULT_POOL["lite"][0])
        qt._process_held.pop("lite")[1].close()


class JobsTests(unittest.TestCase):
    """scripts/quartus_jobs.py: the run wrapper, the picker and the remote program."""

    def setUp(self):
        from scripts import quartus_jobs
        self.jobs = quartus_jobs
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        root = Path(self.directory.name)
        self.pools = {"pro": (str(root / "pro.lock"),), "lite": (str(root / "lite.lock"),)}
        # The temp directories live on /tmp, which a build host may keep under
        # the 30 GiB run refuses; the disk rule has its own test.
        plenty = shutil.disk_usage("/")._replace(free=500 * 1024 ** 3)
        for patch in (mock.patch.object(qt, "LEASE_POOLS", {}), mock.patch.object(qt, "DEFAULT_POOL", self.pools),
                      mock.patch.dict(qt._process_held, clear=True),
                      mock.patch.object(quartus_jobs.shutil, "disk_usage", return_value=plenty)):
            patch.start()
            self.addCleanup(patch.stop)

    def test_run_holds_the_lease_for_the_job_and_records_its_peak(self):
        run_dir = Path(self.directory.name) / "run"
        run_dir.mkdir()
        code = ("import sys; sys.path.insert(0, %r)\n"
                "from scripts import quartus_tools as qt\n"
                "qt.LEASE_POOLS = {}; qt.DEFAULT_POOL = %r\n"
                "block = bytearray(64 * 1024 * 1024)\n"
                "import time; time.sleep(1.5)\n"
                "raise SystemExit(0 if qt.held_quartus_lease('lite') else 3)\n") % (str(MISTEROSS), self.pools)
        with mock.patch.dict(os.environ, {"FES_QUARTUS_RUN_DIR": str(run_dir)}):
            self.assertEqual(self.jobs.run("lite", "test", [sys.executable, "-c", code], sample_seconds=0.2), 0)
        record = json.loads((run_dir / "peak-rss.json").read_text())
        self.assertEqual(record["slot"], self.pools["lite"][0])
        self.assertGreater(record["peak_rss_kib"], 64 * 1024)
        self.assertIsNone(qt.held_quartus_lease("lite"))

    def test_the_peak_counts_descendants_in_their_own_sessions(self):
        # The counterexample for a process-group sum: the memory is held by a
        # grandchild in a new session, as a producer's Quartus stage is.
        run_dir = Path(self.directory.name) / "run2"
        run_dir.mkdir()
        grandchild = "block = bytearray(96 * 1024 * 1024); import time; time.sleep(1.5)"
        code = ("import subprocess, sys; raise SystemExit(subprocess.run([sys.executable, '-c', %r],"
                " start_new_session=True).returncode)") % grandchild
        with mock.patch.dict(os.environ, {"FES_QUARTUS_RUN_DIR": str(run_dir)}):
            self.assertEqual(self.jobs.run("lite", "test", [sys.executable, "-c", code], sample_seconds=0.2), 0)
        self.assertGreater(json.loads((run_dir / "peak-rss.json").read_text())["peak_rss_kib"], 96 * 1024)

    def test_the_slot_is_held_until_a_backgrounded_process_exits(self):
        # Codex P1: the launcher exits at once and leaves its compile running.
        started = time.monotonic()
        marker = Path(self.directory.name) / "done"
        self.assertEqual(self.jobs.run("lite", "", ["sh", "-c", f"(sleep 2; touch {marker}) & exit 0"],
                                       sample_seconds=0.2), 0)
        self.assertTrue(marker.exists(), "the slot was released before the background process finished")
        self.assertGreaterEqual(time.monotonic() - started, 2)

    def test_the_time_cap_stops_every_process_of_the_job(self):
        survivor = Path(self.directory.name) / "survivor"
        with mock.patch.object(self.jobs, "KILL_GRACE_SECONDS", 1):
            code = self.jobs.run("lite", "", ["sh", "-c", f"(sleep 30; touch {survivor}) & sleep 30"],
                                 sample_seconds=0.2, cap_seconds=1)
        self.assertEqual(code, 124)
        time.sleep(0.5)
        self.assertEqual(self.jobs.job_processes("none"), {})
        self.assertFalse(survivor.exists())

    def test_ssh_origins_are_not_credentials(self):
        for url in ("git@github.com:DeanoC/fes.git", "ssh://git@github.com/DeanoC/fes.git",
                    "https://github.com/DeanoC/fes", "https://github.com/DeanoC/fes.git"):
            self.assertFalse(self.jobs.url_has_credentials(url), url)
        for url in ("https://x-access-token:abc@github.com/DeanoC/fes", "https://token@github.com/DeanoC/fes"):
            self.assertTrue(self.jobs.url_has_credentials(url), url)

    def test_run_refuses_a_low_disk_before_starting(self):
        marker = Path(self.directory.name) / "started"
        low = shutil.disk_usage("/")._replace(free=12 * 1024 ** 3)
        with mock.patch.object(self.jobs.shutil, "disk_usage", return_value=low):
            self.assertEqual(self.jobs.run("lite", "", ["touch", str(marker)], sample_seconds=0.1), 75)
        self.assertFalse(marker.exists())
        self.assertEqual(qt.flock_holders(self.pools["lite"][0]), set())

    def test_a_quiet_job_gets_a_heartbeat(self):
        run_dir = Path(self.directory.name) / "quiet"
        run_dir.mkdir()
        with mock.patch.object(self.jobs, "HEARTBEAT_SECONDS", 0.5), \
                mock.patch.dict(os.environ, {"FES_QUARTUS_RUN_DIR": str(run_dir)}):
            self.assertEqual(self.jobs.run("lite", "", ["sleep", "1.5"], sample_seconds=0.2), 0)
        self.assertIn("quartus_jobs heartbeat:", (run_dir / "heartbeat").read_text())

    def test_the_remote_program_refuses_a_low_data_volume(self):
        script, _ = self.jobs.remote_script(
            commit="d" * 40, host="powerboat", home="/home/u", edition="lite", label="",
            command=["true"], stamp="s", origin="https://github.com/DeanoC/fes")
        root = Path(self.directory.name)
        fake = root / "bin"
        fake.mkdir()
        (fake / "df").write_text("#!/bin/sh\necho Avail; echo 12G\n")
        (fake / "df").chmod(0o755)
        script = script.replace(self.jobs.PROJECTS, str(root / "projects"))
        result = subprocess.run(["bash", "-c", script], env=dict(os.environ, PATH=f"{fake}:{os.environ['PATH']}"),
                                capture_output=True, text=True, stdin=subprocess.DEVNULL)
        self.assertEqual(result.returncode, 75, result.stderr)
        self.assertIn("refusing", result.stderr)
        self.assertEqual(list((root / "projects/runs").iterdir()), [])

    def test_run_returns_the_job_exit_code(self):
        self.assertEqual(self.jobs.run("pro", "", ["sh", "-c", "exit 7"], sample_seconds=0.1), 7)

    def probes(self, chip_free, chip_kib, boat_free, boat_kib):
        return {"chip-30x0-ai-dev": {"host": "chip-30x0-ai-dev", "free": chip_free, "available_kib": chip_kib},
                "powerboat": {"host": "powerboat", "free": boat_free, "available_kib": boat_kib}}

    def pick(self, edition, probes):
        with mock.patch.object(self.jobs, "probe", lambda host, _edition: probes[host]):
            return self.jobs.pick(edition)[0]

    def test_pick_prefers_a_free_slot_with_the_memory_for_a_job(self):
        gib = 1024 ** 2
        self.assertEqual(self.pick("lite", self.probes(0, 20 * gib, 1, 30 * gib)), "powerboat")
        self.assertEqual(self.pick("lite", self.probes(1, 20 * gib, 0, 40 * gib)), "chip-30x0-ai-dev")
        # A free slot without the memory for one job does not count as free.
        self.assertEqual(self.pick("lite", self.probes(1, 2 * gib, 1, 30 * gib)), "powerboat")
        # Pro prefers chip when both are free; otherwise whichever is free.
        self.assertEqual(self.pick("pro", self.probes(1, 15 * gib, 2, 40 * gib)), "chip-30x0-ai-dev")
        self.assertEqual(self.pick("pro", self.probes(0, 15 * gib, 1, 40 * gib)), "powerboat")
        # Nothing free: wait where the most memory is.
        self.assertEqual(self.pick("pro", self.probes(0, 15 * gib, 0, 40 * gib)), "powerboat")

    def test_the_remote_program_runs_from_data_under_the_lease(self):
        script, run_dir = self.jobs.remote_script(
            commit="a" * 40, host="chip-30x0-ai-dev", home="/home/u", edition="pro", label="x87 fit",
            command=["python3", "scripts/fit_i386_device.py", "--output", "{run}/fit", "--cache-root={cache}"],
            stamp="20261121T000000Z", origin="https://github.com/DeanoC/fes")
        self.assertRegex(run_dir, r"^/data/projects/fes-quartus/runs/aaaaaaaaaaaa-20261121T000000Z-x87-fit-[0-9a-f]{6}$")
        self.assertNotEqual(self.jobs.remote_script(
            commit="a" * 40, host="chip-30x0-ai-dev", home="/home/u", edition="pro", label="x87 fit",
            command=["true"], stamp="20261121T000000Z", origin="https://github.com/DeanoC/fes")[1], run_dir)
        self.assertIn(f"mkdir {run_dir}\n", script)
        self.assertLess(script.index("df --output=avail"), script.index(f"mkdir {run_dir}"))
        self.assertNotIn("/tmp", script)
        self.assertIn(f"{run_dir}/fit", script)
        self.assertIn("--cache-root=/home/u/fes-toolchain-cache", script)
        self.assertIn("export QUARTUS_ROOTDIR=/data/altera_pro/26.3/quartus", script)
        self.assertIn("scripts/quartus_jobs.py run --edition pro --label x87-fit --cap-minutes 240 --", script)
        self.assertIn(f"worktree add -q --detach {run_dir}/src " + "a" * 40, script)
        self.assertEqual(subprocess.run(["bash", "-n", "-c", script]).returncode, 0)
        lite, _ = self.jobs.remote_script(commit="b" * 40, host="powerboat", home="/home/u", edition="lite",
                                         label="", command=["true"], stamp="s", origin="https://example/x")
        self.assertIn("export QUARTUS_ROOTDIR=/home/u/intelFPGA_lite/17.0/quartus", lite)

    def test_remote_refuses_an_unpushed_commit(self):
        with mock.patch.object(self.jobs.subprocess, "run",
                               return_value=subprocess.CompletedProcess([], 0, "", "")), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="c" * 40 + "\n"):
            with self.assertRaises(SystemExit):
                self.jobs.resolve_pushed("HEAD")


class PoolTests(unittest.TestCase):
    def test_every_committed_quartus_lock_names_a_licensed_digest(self):
        names = subprocess.check_output(["git", "-C", str(REPO), "ls-files", "*quartus.lock"], text=True).split()
        self.assertTrue(names)
        for name in names:
            quartus = tomllib.loads((REPO / name).read_text())["quartus"]
            self.assertEqual(qt.LICENSED_QUARTUS_SH.get(quartus["quartus_sh_sha256"]),
                             qt.edition_class(quartus["edition"]), name)

    def test_each_host_pool_fits_its_ram_budget(self):
        for host, pools in qt.LEASE_POOLS.items():
            ram, reserve = qt.HOST_RAM_GIB[host]
            # A slot shared by both editions (chip's legacy lock) is one job.
            peak = {}
            for edition in ("lite", "pro"):
                for path in pools[edition]:
                    peak[path] = max(peak.get(path, 0), qt.PEAK_RSS_GIB[edition])
            self.assertLessEqual(sum(peak.values()), ram - reserve, host)

    def test_every_pool_ends_with_its_legacy_lander_lock(self):
        self.assertEqual(qt.LEASE_POOLS["chip-30x0-ai-dev"]["pro"][-1], "/tmp/lander-quartus-3090.lock")
        self.assertEqual(qt.LEASE_POOLS["chip-30x0-ai-dev"]["lite"][-1], "/tmp/lander-quartus-3090.lock")
        self.assertEqual(qt.LEASE_POOLS["powerboat"]["pro"][-1], "/tmp/lander-quartus-powerboat-pro.lock")
        self.assertEqual(qt.LEASE_POOLS["powerboat"]["lite"][-1], "/tmp/lander-quartus-lite.lock")


if __name__ == "__main__":
    unittest.main()
