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
import signal
import subprocess
import sys
import tempfile
import textwrap
import threading
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
    """The nodes that certainly run when the module is imported: not function
    bodies, and not the branches of if/while/for/match (`if TYPE_CHECKING:`,
    `if False:`) or anything in a try but its finally, which may not."""
    pending = [tree]
    while pending:
        node = pending.pop()
        yield node
        if isinstance(node, (ast.Try, getattr(ast, "TryStar", ast.Try))):
            # Only finally runs for certain: any statement of the body may
            # raise before a later one, and handlers and else depend on it.
            pending.extend(node.finalbody)
            continue
        for child in ast.iter_child_nodes(node):
            if isinstance(child, (ast.FunctionDef, ast.AsyncFunctionDef, ast.Lambda,
                                  ast.If, ast.While, ast.For, ast.AsyncFor, ast.Match, ast.IfExp)):
                continue
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
    # Builds the installation's quartus_<name> paths only to run ldd on them
    # and record their libraries (execution identity); it never runs them.
    "sources/misteross/cores/fes-pc386/tools/pc386_execution.py": "hash-only",
}
RECEIPT = "sources/misteross/tests/fixtures/de25-combined-proof.json"


SHELL_SEPARATORS = {";", "&&", "||", "|", "&", "(", ")", ";;", "|&", "{", "}", "!"}
NOT_RUNS = {"echo", "printf", "fail", "grep", "[", "test", "[["}
SUBSTITUTION = re.compile(r"(\$\(|`)")


def _shell_words(text: str) -> list[str]:
    """Words and separators as the shell splits them (quotes and # comments
    honoured; ;&|<>() split words)."""
    lexer = shlex.shlex(text, posix=True, punctuation_chars=True)
    lexer.whitespace_split = True
    try:
        return list(lexer)
    except ValueError:
        return text.split()


def shell_runs(text: str):
    """(line, reason) for each leased Quartus program a shell script runs
    outside `quartus_jobs.py run`.

    Lines are split into simple commands by a real shell tokenizer (quotes,
    comments and separators as the shell sees them). In a command, a word
    naming a leased program is a run unless it is a --version/--help probe,
    the operand of a test command, an argument of echo/printf/fail/grep, or
    after the `--` of a `quartus_jobs.py run`. A command substitution that
    names one is always a run, wherever it appears."""
    logical, start = "", 0
    for number, line in enumerate(text.splitlines(), 1):
        if not logical:
            start = number
        if line.endswith("\\"):
            logical += line[:-1] + " "
            continue
        logical += line
        current, logical = logical, ""
        words = _shell_words(current)
        commands, command = [], []
        for word in words:
            if word in SHELL_SEPARATORS:
                commands.append(command)
                command = []
            else:
                command.append(word)
        commands.append(command)
        for command in commands:
            head = next((w for w in command if "=" not in w.split("/")[0]), "")
            for index, word in enumerate(command):
                # A quoted payload (sh -c '...', bash -c, eval, ssh host '...')
                # is shell code: scan it as such, unless it is only a message.
                # Any argument naming one is re-read as shell code, whitespace or
                # not (sh -c 'quartus_sh>log'); a word that does not split is
                # its own command there, so the recursion ends.
                if index > 0 and os.path.basename(head) not in NOT_RUNS and LEASED.search(word) \
                        and not SUBSTITUTION.search(word) and _shell_words(word) != [word]:
                    for _, why in shell_runs(word):
                        yield start, why
                    continue
                for inner in SUBSTITUTION.split(word)[1:]:
                    if LEASED.search(inner):
                        yield start, f"runs {LEASED.search(inner).group(0)} in a command substitution"
                # A leased program put in a variable (tool=quartus_sh) can run
                # under any name later: the assignment itself is refused.
                # Anywhere in the value: tool='quartus_sh --flow compile top'.
                assigned = re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=(.*)", word, re.S)
                if assigned and LEASED.search(assigned.group(2)):
                    yield start, f"assigns {LEASED.search(assigned.group(2)).group(0)} to a variable"
                    continue
                if SUBSTITUTION.search(word) or not LEASED.search(os.path.basename(word)) \
                        or not LEASED.fullmatch(os.path.basename(word)):
                    continue
                if index + 1 < len(command) and PROBE.match(command[index + 1]):
                    continue
                # printf -v NAME stores its output in a variable: an assignment.
                if os.path.basename(head) == "printf" and "-v" in command[:index]:
                    yield start, f"stores {os.path.basename(word)} in a variable with printf -v"
                    continue
                if os.path.basename(head) in NOT_RUNS and index > 0:
                    continue
                if "--" in command[:index] and any(w.endswith("quartus_jobs.py") for w in command[:index]) \
                        and "run" in command[:command.index("--")]:
                    continue
                yield start, f"runs {os.path.basename(word)} outside quartus_jobs.py run"


LEASED_NAMES = ("sh", "map", "syn", "fit", "sta", "asm", "eda", "cdb", "ipgenerate", "npp", "pow", "drc",
                "si", "tan", "dse", "stp")


def _names_a_program_prefix(value: str) -> bool:
    """A string ending in a proper prefix of a leased program's name
    ('quartus_', '.../quartus_f'), which code can complete at run time."""
    match = re.search(r"(?:^|[/\s])quartus_([a-z]*)$", value)
    return bool(match) and any(name.startswith(match.group(1)) and name != match.group(1) for name in LEASED_NAMES)


def python_names_leased(text: str) -> bool:
    """A leased program name in the source, or in a string constant after
    Python joins adjacent literals ("quartus_" "fit")."""
    if LEASED.search(text):
        return True
    try:
        tree = ast.parse(text)
    except SyntaxError:
        return False
    # A part of a name built at run time ('quartus_' + 'fit', f'quartus_{x}')
    # counts as naming one.
    return any(isinstance(node, ast.Constant) and isinstance(node.value, str)
               and (LEASED.search(node.value) or _names_a_program_prefix(node.value))
               for node in ast.walk(tree))


def python_launches(text: str) -> bool:
    """Whether a module can start a process, by its bindings, not spelling:
    any import of subprocess or asyncio's subprocess API, and any os
    exec/spawn/system/popen use, however aliased."""
    if LAUNCH.search(text):
        return True
    try:
        tree = ast.parse(text)
    except SyntaxError:
        return True
    os_launch = re.compile(r"(exec|spawn|posix_spawn|system|popen|fork)")
    for node in ast.walk(tree):
        if isinstance(node, ast.Import) and any(a.name.split(".")[0] in ("subprocess", "pty") for a in node.names):
            return True
        if isinstance(node, ast.ImportFrom) and node.module:
            if node.module.split(".")[0] in ("subprocess", "pty") or node.module.startswith("asyncio"):
                return True
            if node.module == "os" and any(os_launch.match(a.name) for a in node.names):
                return True
        if isinstance(node, ast.Attribute) and os_launch.match(node.attr) and \
                isinstance(node.value, ast.Name):
            return True
        if isinstance(node, ast.Call) and isinstance(node.func, ast.Name) and node.func.id == "__import__":
            return True
    return False


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
        if not path.is_file() or path.is_symlink():
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        first = text.split("\n", 1)[0]
        is_python = name.endswith(".py") or (first.startswith("#!") and "python" in first)
        is_shell = name.endswith((".sh", ".bash")) or (first.startswith("#!") and re.search(r"\b(ba|da|z)?sh\b", first))
        if is_python:
            if name in PINNED and root == REPO:
                continue
            if python_names_leased(text) and python_launches(text) and not LOCATES.search(text) \
                    and not leased(path):
                failures.append(f"{name}: starts processes and names a Quartus program, "
                                "but never imports scripts.quartus_tools")
        elif is_shell:
            # No raw-text prefilter: the shell joins quoted fragments
            # (quartus_""sh), so only its own tokenizer can tell.
            failures.extend(f"{name}:{number}: {why}" for number, why in shell_runs(text))
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

    def test_counterexample_a_conditional_leasing_import_does_not_count(self):
        # Codex P1 at 502d9f161: an import under if False / TYPE_CHECKING
        # never installs the hook.
        for guard in ("if False:", "if TYPE_CHECKING:"):
            failures = self.scan({"sources/misteross/scripts/x.py":
                                  f"import subprocess\n{guard}\n    from scripts import quartus_tools\n"
                                  "subprocess.run(['quartus_fit', 'top'])\n"})
            self.assertEqual(len(failures), 1, guard)
        # Codex P1 at 3e1144c04: an except handler (or else) may never run.
        for handler in ("try:\n    pass\nexcept Exception:\n    from scripts import quartus_tools\n",
                        "try:\n    pass\nexcept Exception:\n    pass\nelse:\n    from scripts import quartus_tools\n"):
            failures = self.scan({"sources/misteross/scripts/y.py":
                                  "import subprocess\n" + handler + "subprocess.run(['quartus_fit', 'top'])\n"})
            self.assertEqual(len(failures), 1, handler)
        # Codex P1 at 1406e08bc: a try body may raise before the import.
        self.assertEqual(len(self.scan({"sources/misteross/scripts/z.py":
                                        "import subprocess\ntry:\n    raise RuntimeError()\n"
                                        "    from scripts import quartus_tools\nexcept RuntimeError:\n    pass\n"
                                        "subprocess.run(['quartus_fit', 'top'])\n"})), 1)
        self.assertEqual(self.scan({"sources/misteross/scripts/w.py":
                                    "import subprocess\ntry:\n    pass\nfinally:\n"
                                    "    from scripts import quartus_tools\nsubprocess.run(['quartus_fit', 'top'])\n"}), [])

    def test_counterexample_naming_a_leasing_module_in_a_string_is_not_loading_it(self):
        failures = self.scan({
            "sources/misteross/boards/x/helper.py": "from scripts import quartus_tools\n",
            "sources/misteross/boards/x/build.py":
                "INPUTS = ('helper.py', 'scripts/quartus_tools.py')\n" + textwrap.dedent(self.UNLEASED_MODULE)})
        self.assertEqual(len(failures), 1)

    def test_counterexample_aliased_launches_and_split_names_are_caught(self):
        # Codex P1s at 7bfc1b435: import subprocess as s; s.run(...), a
        # directly imported run, and "quartus_" "fit" joined by Python.
        failures = self.scan({
            "sources/misteross/scripts/a.py": "import subprocess as s\ns.run(['quartus_fit', 'top'])\n",
            "sources/misteross/scripts/b.py": "from subprocess import run as go\ngo(['quartus_fit', 'top'])\n",
            "sources/misteross/scripts/c.py": "import subprocess as s\ns.run(['quartus_' 'fit', 'top'])\n",
            "sources/misteross/scripts/f.py": "import subprocess\nsubprocess.run(['quartus_' + 'fit', 'top'])\n",
            "scripts/d.sh": "#!/bin/sh\nquartus_\"\"sh --flow compile top\n",
            "scripts/e": "#!/bin/bash\nquartus_sh --flow compile top\n",
        })
        self.assertEqual(sorted(f.split(":")[0] for f in failures),
                         ["scripts/d.sh", "scripts/e", "sources/misteross/scripts/a.py",
                          "sources/misteross/scripts/b.py", "sources/misteross/scripts/c.py",
                          "sources/misteross/scripts/f.py"])

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
            [ -x "$Q/quartus_sh" ] || fail "quartus_sh missing"
            [ -x $Q/quartus_sh ] && $Q/quartus_sh --flow compile top
            test -f /opt/latest/quartus_fit && /opt/latest/quartus_fit top
            echo "running quartus_sh now"; quartus_sh --flow compile top
            echo "$(quartus_sh --flow compile top)"
            grep "`quartus_fit top`" file
            [ -x "$(quartus_sh --flow compile top)" ]
            echo "#"; quartus_sh --flow compile top
            echo "quartus_jobs.py run"; quartus_sh --flow compile top
            timeout 600 \\
              quartus_sh --flow compile top  # a continued line
            # quartus_sh --flow compile top   (a comment)
            sh -c 'quartus_sh --flow compile top'
            eval "quartus_fit top"
            ssh chip 'cd /data/x && quartus_sh --flow compile top'
            bash -c 'quartus_sh --version'
            sh -c 'quartus_sh>/tmp/log'
            sh -c 'quartus_sh&'
            eval 'quartus_fit>/tmp/x'
            tool=quartus_sh
            export Q=$ROOT/bin/quartus_fit
            tool='quartus_sh --flow compile top'; $tool
            cmd+=("$Q/quartus_sta" -t gate.tcl)
            printf -v tool %s quartus_sh; "$tool" --flow compile top
            """})
        self.assertEqual([f.split(": ")[0] for f in failures],
                         ["scripts/job.sh:4", "scripts/job.sh:7", "scripts/job.sh:8", "scripts/job.sh:9",
                          "scripts/job.sh:10", "scripts/job.sh:11", "scripts/job.sh:12", "scripts/job.sh:13",
                          "scripts/job.sh:14", "scripts/job.sh:15", "scripts/job.sh:18", "scripts/job.sh:19",
                          "scripts/job.sh:20", "scripts/job.sh:22", "scripts/job.sh:23", "scripts/job.sh:24",
                          "scripts/job.sh:25", "scripts/job.sh:26", "scripts/job.sh:27", "scripts/job.sh:28",
                          "scripts/job.sh:29"])


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

    def test_parallel_seeds_under_an_ancestor_lease_each_count(self):
        # Codex P1 at 9b3488e06: under flock(1) or a `run` wrapper, every seed
        # thread reused the ancestor's one slot. The first claims it; the
        # second takes its own.
        legacy = self.pools["pro"][1]
        code = textwrap.dedent(f"""
            import sys, threading; sys.path.insert(0, {str(MISTEROSS)!r})
            from scripts import quartus_tools as qt
            qt.LEASE_POOLS = {{}}; qt.DEFAULT_POOL = {self.pools!r}
            took, both = [], threading.Barrier(2)
            def seed():
                with qt.quartus_lease('pro') as path:
                    took.append(path)
                    both.wait(10)
            threads = [threading.Thread(target=seed) for _ in range(2)]
            [t.start() for t in threads]; [t.join() for t in threads]
            print(sorted(took))
            """)
        result = subprocess.run(["flock", legacy, sys.executable, "-c", code],
                                capture_output=True, text=True, timeout=60)
        self.assertEqual(result.stdout.strip(), str(sorted(self.pools["pro"])), result.stderr)

    def test_seeds_under_a_one_slot_ancestor_take_turns(self):
        # Codex P1 at 3af7e3562: chip's Pro pool is the one legacy slot. Under
        # a wrapper holding it, the second seed waited on the physical lock
        # forever. It now waits for the first seed's claim instead.
        legacy = self.pools["pro"][1]
        code = textwrap.dedent(f"""
            import sys, threading, time; sys.path.insert(0, {str(MISTEROSS)!r})
            from scripts import quartus_tools as qt
            qt.LEASE_POOLS = {{}}; qt.DEFAULT_POOL = {{'pro': ({legacy!r},), 'lite': ()}}
            took = []
            def seed():
                with qt.quartus_lease('pro') as path:
                    took.append(path)
                    time.sleep(0.5)
            threads = [threading.Thread(target=seed) for _ in range(2)]
            [t.start() for t in threads]; [t.join() for t in threads]
            print(len(took), sorted(set(took)))
            """)
        result = subprocess.run(["flock", legacy, sys.executable, "-c", code],
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.stdout.strip(), f"2 [{legacy!r}]", result.stderr)

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
        # Codex P1 at 9d516bdbf: the program only in the child's environment.
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["sh", "-c", "$Q --flow compile top"], env=dict(os.environ, Q=str(self.bin / "quartus_sh")))
        # Codex P1 at 61760d878: the program only in the inherited environment.
        with mock.patch.dict(os.environ, {"Q": str(self.bin / "quartus_sh")}):
            with self.assertRaises(qt.QuartusLeaseError):
                subprocess.run(["sh", "-c", "$Q --flow compile top"])
            with self.assertRaises(qt.QuartusLeaseError):
                os.system("$Q --flow compile top")
        # Codex P1 at c39608e8c: a --help in another environment value.
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["sh", "-c", "$Q --flow compile top"],
                           env={"Q": str(self.bin / "quartus_sh"), "X": "--help", "PATH": "/usr/bin:/bin"})
        # ...a relative path resolved against the child's cwd...
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["./quartus_fit", "top"], cwd=str(self.bin))
        # Codex P1 at 79cc0a4ac: a relative PATH component, resolved from the
        # child's cwd, and a non-executable earlier match execvp skips.
        relative = Path(self.directory.name)
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["quartus_fit", "top"], cwd=str(relative), env={"PATH": "quartus/bin"})
        decoy = relative / "decoy"
        decoy.mkdir()
        (decoy / "quartus_fit").write_text("not a program\n")
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["quartus_fit", "top"], env={"PATH": f"{decoy}:{self.bin}"})
        # Codex P1s at 77920ff7d, 73a1bfe62 and 60968319c: which PATH entry
        # execvp ends up running depends on the failures it skips, so every
        # licensed executable on the child's PATH needs its lease, even
        # behind a runnable stand-in.
        standin = relative / "standin"
        standin.mkdir()
        (standin / "quartus_fit").write_text("#!/bin/sh\nexit 0\n")
        (standin / "quartus_fit").chmod(0o755)
        self.assertEqual(qt.licensed_quartus_editions(["quartus_fit", "top"], f"{standin}:{self.bin}"), {"lite"})
        self.assertEqual(qt.licensed_quartus_editions(["quartus_fit", "top"], str(standin)), set())
        self.assertEqual(qt.licensed_quartus_editions(["quartus_fit", "top"], f"{decoy}:{self.bin}"), {"lite"})
        # An executable stand-in whose interpreter is missing (ENOENT, and
        # execvp moves on to the licensed one).
        broken = relative / "broken"
        broken.mkdir()
        (broken / "quartus_fit").write_text("#!/definitely/missing/interpreter\n")
        (broken / "quartus_fit").chmod(0o755)
        self.assertEqual(qt.licensed_quartus_editions(["quartus_fit", "top"], f"{broken}:{self.bin}"), {"lite"})
        # ...and a bare name found on the child's PATH, not this process's.
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["quartus_fit", "top"], env={"PATH": str(self.bin)})
        # Codex P1s at 7b395e311: the executed program given apart from argv,
        # a symlink to a licensed program, and a --help in another argument.
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["alias", "--flow", "compile", "top"], executable=str(self.bin / "quartus_sh"))
        link_dir = Path(self.directory.name) / "links"
        link_dir.mkdir()
        (link_dir / "quartus_fit").symlink_to(self.bin / "quartus_fit")
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run([str(link_dir / "quartus_fit"), "top"])
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["sh", "-c", "$0 --flow compile top", str(self.bin / "quartus_sh"), "--help"])
        # Codex P1 at 1406e08bc: a command name assembled from expansions.
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["sh", "-c", "Q=quartus_; Q=${Q}sh; $Q --flow compile top"], env={"PATH": str(self.bin)})
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["sh", "-c", "eval \"$CMD\""], env={"CMD": "true"})
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["sh", "-c", "timeout 60 $Q top"], env={"Q": "true"})
        # Codex P1 at 35e36d0b5: an option operand of a wrapper.
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["sh", "-c", "Q=quartus_; Q=${Q}sh; env -u X $Q --flow compile top"],
                           env={"PATH": str(self.bin)})
        self.assertEqual(subprocess.run(["sh", "-c", "echo $HOME; true"], capture_output=True).returncode, 0)
        # Codex P1 at 3e1144c04: an assignment given to env as its own argument.
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["env", f"Q={self.bin / 'quartus_sh'}", "sh", "-c", "$Q --flow compile top"])
        # Codex P1 at 7f7121f88: a shell-local assignment run later as $Q.
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["sh", "-c", "Q=quartus_sh; $Q --flow compile top"], env={"PATH": str(self.bin)})
        # Codex P1 at 827ccac13: a relative name in shell code is resolved
        # after its cd, which the hook cannot follow: some lease is needed.
        with self.assertRaises(qt.QuartusLeaseError):
            subprocess.run(["sh", "-c", f"cd {self.bin}; ./quartus_fit top"])
        # Codex P1 at 502d9f161: it may resolve to either edition (PATH=/pro/bin;
        # quartus_sh), so one edition's lease is not enough; both are.
        with qt.quartus_lease("lite"):
            with self.assertRaises(qt.QuartusLeaseError):
                subprocess.run(["sh", "-c", "PATH=/pro/bin; quartus_sh --flow compile top"])
            with qt.quartus_lease("pro"):
                self.assertEqual(subprocess.run(["sh", "-c", f"cd {self.bin}; ./quartus_fit top"],
                                                capture_output=True).returncode, 0)
        # Codex P1 at 34cd931cc: an operator attached to the program name.
        for payload in (f"{self.bin}/quartus_sh>/dev/null", f"{self.bin}/quartus_fit&", f"true;{self.bin}/quartus_fit"):
            with self.assertRaises(qt.QuartusLeaseError):
                subprocess.run(["sh", "-c", payload])

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


def fake_dispatch(code: int) -> subprocess.Popen:
    """A finished dispatch with exit code code."""
    return subprocess.Popen(["sh", "-c", f"exit {code}"])


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
                      mock.patch.object(quartus_jobs.shutil, "disk_usage", return_value=plenty),
                      mock.patch.object(quartus_jobs, "_mem_available_kib", return_value=500 * 1024 ** 2),
                      mock.patch.object(quartus_jobs, "ADMISSION_LOCK", str(root / "admission.lock")),
                      mock.patch.object(quartus_jobs, "ADMISSION_WAITERS", str(root / "waiting"))):
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

    def test_the_slot_is_held_for_an_orphan_with_a_scrubbed_environment(self):
        # Codex P1 at 9b5a6a3cf: the launcher starts an env={} child and exits
        # at once, before any scan; neither the token nor parentage from the
        # dead launcher finds it. The subreaper does.
        marker = Path(self.directory.name) / "orphan-done"
        orphan = f"import time, pathlib; time.sleep(2); pathlib.Path({str(marker)!r}).touch()"
        code = f"import subprocess, sys; subprocess.Popen([sys.executable, '-c', {orphan!r}], env={{}})"
        started = time.monotonic()
        self.assertEqual(self.jobs.run("lite", "", [sys.executable, "-c", code], sample_seconds=0.2), 0)
        self.assertTrue(marker.exists(), "the slot was released while the orphan still ran")
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
        result = subprocess.run(["bash", "-s"], env=dict(os.environ, PATH=f"{fake}:{os.environ['PATH']}"),
                                capture_output=True, text=True, input=script)
        self.assertEqual(result.returncode, 75, result.stderr)
        self.assertIn("refusing", result.stderr)
        self.assertEqual(list((root / "projects/runs").iterdir()), [])

    def test_the_peak_counts_a_descendant_with_a_scrubbed_environment(self):
        # The counterexample for the environment token alone: a producer that
        # runs Quartus under a controlled environment (functional_execution).
        run_dir = Path(self.directory.name) / "scrubbed"
        run_dir.mkdir()
        grandchild = "block = bytearray(96 * 1024 * 1024); import time; time.sleep(1.5)"
        code = ("import subprocess, sys; raise SystemExit(subprocess.run([sys.executable, '-c', %r],"
                " start_new_session=True, env={}).returncode)") % grandchild
        with mock.patch.dict(os.environ, {"FES_QUARTUS_RUN_DIR": str(run_dir)}):
            self.assertEqual(self.jobs.run("lite", "test", [sys.executable, "-c", code], sample_seconds=0.2), 0)
        self.assertGreater(json.loads((run_dir / "peak-rss.json").read_text())["peak_rss_kib"], 96 * 1024)

    def test_a_wrapper_error_stops_the_job_before_the_slot_is_released(self):
        # Codex P1 at a089560de: the run directory vanished, the heartbeat
        # write raised, and run unwound over a live job. Any error in the
        # wrapper must now stop and reap the job before the lease unlocks;
        # a record write is best-effort.
        survivor = Path(self.directory.name) / "survivor2"
        run_dir = Path(self.directory.name) / "vanishing"
        run_dir.mkdir()
        boom = RuntimeError("wrapper failure")
        with mock.patch.object(self.jobs, "KILL_GRACE_SECONDS", 1), \
                mock.patch.object(self.jobs, "HEARTBEAT_SECONDS", 0.3), \
                mock.patch.dict(os.environ, {"FES_QUARTUS_RUN_DIR": str(run_dir)}):
            run_dir.rmdir()
            # A vanished run directory no longer unwinds the wrapper...
            self.assertEqual(self.jobs.run("lite", "", ["sleep", "1"], sample_seconds=0.2), 0)
            # ...and any other wrapper error stops the whole job first.
            real, calls = self.jobs.job_processes, []

            def failing_once(*args):
                calls.append(1)
                if len(calls) == 6:
                    raise boom
                return real(*args)

            with mock.patch.object(self.jobs, "job_processes", failing_once):
                with self.assertRaises(RuntimeError):
                    self.jobs.run("lite", "", ["sh", "-c", f"(sleep 5; touch {survivor}) & sleep 5"],
                                  sample_seconds=0.2)
        self.assertEqual(qt.flock_holders(self.pools["lite"][0]), set())
        time.sleep(5.5)
        self.assertFalse(survivor.exists(), "a process of the job outlived the released slot")

    def test_admission_counts_what_other_running_jobs_may_still_grow_into(self):
        # Codex P1 at 6db0f20ca: two callers both saw 17 GiB free and both
        # started a 9 GiB job. Admission now reserves each other holder's
        # peak less its current use, under one host-wide lock.
        other = self.pools["pro"][0]
        self.pools["lite"] = (str(Path(self.directory.name) / "lite-a.lock"),)
        qt.DEFAULT_POOL.update(self.pools)
        holder = subprocess.Popen(["flock", other, "sleep", "30"])
        self.addCleanup(holder.wait)
        self.addCleanup(holder.terminate)
        deadline = time.monotonic() + 10
        while not qt.flock_holders(other) and time.monotonic() < deadline:
            time.sleep(0.05)
        gib = 1024 ** 2
        # 12 GiB available, a Pro holder that has not grown yet (reserves ~9),
        # and a Lite job needing 6: not admitted until memory frees up.
        memory = iter([12 * gib, 12 * gib, 40 * gib])
        with mock.patch.object(self.jobs, "_mem_available_kib", lambda: next(memory)), \
                mock.patch.object(self.jobs, "ADMISSION_RETRY_SECONDS", 0):
            self.assertEqual(self.jobs.run("lite", "", ["true"], sample_seconds=0.1), 0)
            self.assertEqual(next(memory, "drained"), "drained", "admitted before memory was available")

    def test_the_time_cap_covers_the_wait_for_memory(self):
        # Codex P1 at d7af65924: admission could hold a slot forever.
        marker = Path(self.directory.name) / "never-started"
        with mock.patch.object(self.jobs, "_mem_available_kib", return_value=1), \
                mock.patch.object(self.jobs, "ADMISSION_RETRY_SECONDS", 0.1):
            code = self.jobs.run("lite", "", ["touch", str(marker)], sample_seconds=0.1, cap_seconds=0.5)
        self.assertEqual(code, 124)
        self.assertFalse(marker.exists())
        self.assertEqual(qt.flock_holders(self.pools["lite"][0]), set())

    def test_memory_freed_after_the_cap_starts_nothing(self):
        # Codex P1 at 9ead0d308: memory freed between the cap and the next
        # retry admitted a job after its cap.
        marker = Path(self.directory.name) / "after-cap"
        memory = iter([1, 500 * 1024 ** 2])
        with mock.patch.object(self.jobs, "_mem_available_kib", lambda: next(memory, 500 * 1024 ** 2)), \
                mock.patch.object(self.jobs, "ADMISSION_RETRY_SECONDS", 0.6):
            code = self.jobs.run("lite", "", ["touch", str(marker)], sample_seconds=0.1, cap_seconds=0.3)
        self.assertEqual(code, 124)
        self.assertFalse(marker.exists())

    def test_an_exited_launchers_pid_is_not_followed(self):
        # Codex P1 at d7af65924: once the launcher has exited, its pid may be
        # reused; the job set must not take that process in.
        with mock.patch.object(self.jobs, "job_processes", wraps=self.jobs.job_processes) as scanned:
            self.assertEqual(self.jobs.run("lite", "", ["sh", "-c", "sleep 1 & exit 0"], sample_seconds=0.2), 0)
        roots = [call.args[1] for call in scanned.call_args_list]
        self.assertIsNone(roots[-1], "the scan still followed the exited launcher's pid")

    def test_a_holder_of_several_slots_counts_its_memory_once(self):
        # Codex P1 at 9d516bdbf: one runner holding two Lite slots (parallel
        # seeds) with 8 GiB resident may still grow to 2 x 6 = 12 GiB; its
        # RSS was subtracted from each slot's peak separately (reserving 0).
        gib = 1024 ** 2
        with mock.patch.object(qt, "flock_holders", side_effect=lambda path: {4242} if "lite" in path else set()), \
                mock.patch.object(qt, "lease_pool", side_effect=lambda edition, host=None:
                                  ("/x/lite-a.lock", "/x/lite-b.lock") if edition == "lite" else ("/x/pro.lock",)), \
                mock.patch.object(self.jobs, "_tree_rss_kib", return_value=8 * gib):
            self.assertEqual(self.jobs.outstanding_kib("none"), 4 * gib)

    def test_an_exited_launcher_is_never_signalled(self):
        # Codex P1 at 5def89121: the cap signalled process.pid after the
        # launcher had exited, when the number may name another process.
        sent, launched, real = [], [], subprocess.Popen

        def popen(*args, **kwargs):
            launched.append(real(*args, **kwargs))
            return launched[-1]

        with mock.patch.object(self.jobs, "_signal_all", lambda pids, number: sent.append(list(pids))), \
                mock.patch.object(self.jobs.subprocess, "Popen", popen), \
                mock.patch.object(self.jobs, "KILL_GRACE_SECONDS", 0.3):
            code = self.jobs.run("lite", "", ["sh", "-c", "sleep 1.5 & exit 0"], sample_seconds=0.2, cap_seconds=0.6)
        self.assertEqual(code, 124)
        self.assertTrue(sent, "the cap sent nothing")
        self.assertTrue(all(launched[0].pid not in pids for pids in sent), sent)

    def test_nested_holders_count_their_tree_once(self):
        # Codex P1 at dfa0123c9: a run wrapper (pid 100) and its seed child
        # (pid 200) each hold a Lite slot; the child's 8 GiB was counted in
        # both trees. Together they may grow to 12 GiB: 4 GiB outstanding.
        gib = 1024 ** 2
        holders = {"/x/lite-a.lock": {100}, "/x/lite-b.lock": {200}}
        with mock.patch.object(qt, "flock_holders", side_effect=lambda path: holders.get(path, set())), \
                mock.patch.object(qt, "lease_pool", side_effect=lambda edition, host=None:
                                  ("/x/lite-a.lock", "/x/lite-b.lock") if edition == "lite" else ("/x/pro.lock",)), \
                mock.patch.object(self.jobs, "_ancestors_of", side_effect=lambda pid: [100, 1] if pid == 200 else [1]), \
                mock.patch.object(self.jobs, "_tree_rss_kib", side_effect=lambda pid: 8 * gib):
            self.assertEqual(self.jobs.outstanding_kib("none"), 4 * gib)

    def test_a_signal_while_waiting_for_memory_is_a_notified_cancellation(self):
        # Codex P2 at dfa0123c9: the default action killed the wrapper during
        # admission and nothing recorded it.
        run_dir = Path(self.directory.name) / "cancelled"
        run_dir.mkdir()
        timer = threading.Timer(0.5, lambda: os.kill(os.getpid(), signal.SIGTERM))
        timer.start()
        try:
            with mock.patch.object(self.jobs, "_mem_available_kib", return_value=1), \
                    mock.patch.object(self.jobs, "ADMISSION_RETRY_SECONDS", 0.1), \
                    mock.patch.dict(os.environ, {"FES_QUARTUS_RUN_DIR": str(run_dir)}):
                code = self.jobs.run("lite", "", ["true"], sample_seconds=0.1)
        finally:
            timer.cancel()
        self.assertEqual(code, 128 + signal.SIGTERM)
        self.assertIn('"status": "cancelled"', (run_dir / "result.json").read_text())

    def test_a_stale_waiter_marker_exempts_nobody(self):
        # Codex P1 at f2eae796b: a waiter killed after writing its marker left
        # it behind; a later holder with the same pid was then not counted.
        other = self.pools["pro"][0]
        holder = subprocess.Popen(["flock", other, "sleep", "30"])
        self.addCleanup(holder.wait)
        self.addCleanup(holder.terminate)
        deadline = time.monotonic() + 10
        while not qt.flock_holders(other) and time.monotonic() < deadline:
            time.sleep(0.05)
        pid = next(iter(qt.flock_holders(other)))
        waiting = Path(self.jobs.ADMISSION_WAITERS)
        waiting.mkdir(exist_ok=True)
        (waiting / str(pid)).write_text("12345")  # another process's start time
        self.assertGreater(self.jobs.outstanding_kib("none"), 8 * 1024 ** 2)
        (waiting / str(pid)).write_text(self.jobs._start_time(pid))
        self.assertEqual(self.jobs.outstanding_kib("none"), 0)

    def test_a_signal_while_the_job_starts_still_stops_the_job(self):
        # Codex P1 at 9b3488e06: SIGTERM between Popen and the relay handlers
        # killed the wrapper and left the job running unleased.
        marker = Path(self.directory.name) / "late-child"
        real = subprocess.Popen

        def popen_then_signalled(*args, **kwargs):
            os.kill(os.getpid(), signal.SIGTERM)
            return real(*args, **kwargs)

        with mock.patch.object(self.jobs.subprocess, "Popen", popen_then_signalled):
            code = self.jobs.run("lite", "", ["sh", "-c", f"sleep 2; touch {marker}"], sample_seconds=0.2)
        self.assertEqual(code, 128 + signal.SIGTERM)
        time.sleep(2.5)
        self.assertFalse(marker.exists(), "the job outlived the wrapper's lease")

    def test_two_waiters_never_reserve_against_each_other(self):
        # Codex P1 at 7fd9ac7e4: two Lite jobs, memory for one. Each counted
        # the other's idle, not-yet-admitted wrapper as a job, and both waited
        # forever. A waiter now marks itself and is not counted.
        root = Path(self.directory.name)
        pools = {"pro": (str(root / "p.lock"),), "lite": (str(root / "a.lock"), str(root / "b.lock"))}
        done = root / "admitted"
        code = textwrap.dedent(f"""
            import sys, time; sys.path.insert(0, {str(MISTEROSS)!r})
            from scripts import quartus_tools as qt, quartus_jobs as jobs
            qt.LEASE_POOLS = {{}}; qt.DEFAULT_POOL = {pools!r}
            jobs.ADMISSION_LOCK = {str(root / "adm.lock")!r}
            jobs.ADMISSION_WAITERS = {str(root / "wait")!r}
            jobs.ADMISSION_RETRY_SECONDS = 0.2
            jobs._mem_available_kib = lambda: 8 * 1024 ** 2
            jobs.run("lite", "", ["sh", "-c", "echo x >> {done}; sleep 2"], sample_seconds=0.2)
            """)
        waiters = [subprocess.Popen([sys.executable, "-c", code], stdout=subprocess.DEVNULL,
                                    stderr=subprocess.DEVNULL) for _ in range(2)]
        try:
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline and (not done.exists() or len(done.read_text().split()) < 2):
                time.sleep(0.1)
            self.assertEqual(len(done.read_text().split()) if done.exists() else 0, 2,
                             "the two waiters deadlocked")
        finally:
            for waiter in waiters:
                waiter.wait(timeout=30)

    def test_a_relayed_signal_is_never_reported_as_success(self):
        # Codex P1 at 6db0f20ca: the command traps SIGTERM and exits 0.
        script = "trap 'exit 0' TERM; sleep 5 & wait"
        timer = threading.Timer(1.0, lambda: os.kill(os.getpid(), signal.SIGTERM))
        timer.start()
        try:
            code = self.jobs.run("lite", "", ["sh", "-c", script], sample_seconds=0.2)
        finally:
            timer.cancel()
        self.assertEqual(code, 128 + signal.SIGTERM)

    def test_every_outcome_is_notified(self):
        run_dir = Path(self.directory.name) / "notified"
        run_dir.mkdir()
        sink = Path(self.directory.name) / "notifications"
        with mock.patch.dict(os.environ, {"FES_QUARTUS_RUN_DIR": str(run_dir),
                                          "FES_QUARTUS_NOTIFY": f"cat >> {sink}"}):
            self.assertEqual(self.jobs.run("lite", "ok", ["true"], sample_seconds=0.1), 0)
            self.assertEqual(self.jobs.run("lite", "bad", ["sh", "-c", "exit 3"], sample_seconds=0.1), 3)
            low = shutil.disk_usage("/")._replace(free=1)
            with mock.patch.object(self.jobs.shutil, "disk_usage", return_value=low):
                self.assertEqual(self.jobs.run("lite", "low", ["true"], sample_seconds=0.1), 75)
        text = sink.read_text()
        for status in ('"finished"', '"failed"', '"refused"'):
            self.assertIn(status, text)
        self.assertIn('"status": "refused"', (run_dir / "result.json").read_text())

    def test_a_cap_that_never_expires_is_refused(self):
        # Codex P2 at c86dc09d0: nan and inf caps never expire.
        for text in ("nan", "inf", "-1", "0"):
            with self.assertRaises(SystemExit), mock.patch("sys.stderr"):
                self.jobs.main(["run", "--edition", "lite", "--cap-minutes", text, "--", "true"])
        for value in (float("nan"), float("inf")):
            with self.assertRaises(SystemExit):
                self.jobs.run("lite", "", ["true"], cap_seconds=value)

    def test_run_returns_the_job_exit_code(self):
        self.assertEqual(self.jobs.run("pro", "", ["sh", "-c", "exit 7"], sample_seconds=0.1), 7)

    def probes(self, chip_free, chip_kib, boat_free, boat_kib):
        return {"chip-30x0-ai-dev": {"host": "chip-30x0-ai-dev", "free": chip_free, "available_kib": chip_kib},
                "powerboat": {"host": "powerboat", "free": boat_free, "available_kib": boat_kib}}

    def pick(self, edition, probes):
        with mock.patch.object(self.jobs, "probe", lambda host, _edition, timeout=60: probes[host]):
            host, _, ready = self.jobs.pick(edition)
            return host if ready else f"wait:{host}"

    def test_pick_prefers_a_free_slot_with_the_memory_for_a_job(self):
        gib = 1024 ** 2
        self.assertEqual(self.pick("lite", self.probes(0, 20 * gib, 1, 30 * gib)), "powerboat")
        self.assertEqual(self.pick("lite", self.probes(1, 20 * gib, 0, 40 * gib)), "chip-30x0-ai-dev")
        # A free slot without the memory for one job does not count as free.
        self.assertEqual(self.pick("lite", self.probes(1, 2 * gib, 1, 30 * gib)), "powerboat")
        # Pro prefers chip when both are free; otherwise whichever is free.
        self.assertEqual(self.pick("pro", self.probes(1, 15 * gib, 2, 40 * gib)), "chip-30x0-ai-dev")
        self.assertEqual(self.pick("pro", self.probes(0, 15 * gib, 1, 40 * gib)), "powerboat")
        # Nothing free: not ready; a remote job waits (where the most memory is).
        self.assertEqual(self.pick("pro", self.probes(0, 15 * gib, 0, 40 * gib)), "wait:powerboat")
        # Codex P1: a free slot below the memory threshold is not ready, even
        # when the other host has no free slot.
        self.assertEqual(self.pick("pro", self.probes(1, 5 * gib, 0, 40 * gib)), "wait:powerboat")

    def test_remote_waits_rather_than_dispatching_onto_a_host_without_memory(self):
        gib = 1024 ** 2
        rounds = [self.probes(1, 5 * gib, 0, 40 * gib), self.probes(1, 5 * gib, 1, 40 * gib)]
        calls = []

        def probe(host, _edition, timeout=60):
            data = dict(rounds[min(len(calls) // 2, 1)][host], home="/home/u")
            calls.append(host)
            return data

        with mock.patch.object(self.jobs, "probe", probe), mock.patch.object(self.jobs, "REPROBE_SECONDS", 0), \
                mock.patch.object(self.jobs, "resolve_pushed", return_value="e" * 40), \
                mock.patch.object(self.jobs, "local_host", return_value="elsewhere"), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="https://github.com/DeanoC/fes\n"), \
                mock.patch.object(self.jobs, "_dispatch", side_effect=lambda target, script="": fake_dispatch(0)) as launched:
            self.assertEqual(self.jobs.remote("pro", "auto", "HEAD", "", ["true"]), 0)
        self.assertEqual(len(calls), 4, "it dispatched before a host was ready")
        self.assertIn("powerboat", launched.call_args.args[0])

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
        # Codex P2 at 9b3488e06: the caller's origin, even on a mirror another
        # caller created.
        self.assertLess(script.index("remote set-url origin https://github.com/DeanoC/fes"),
                        script.index("fetch -q origin"))
        self.assertEqual(subprocess.run(["bash", "-n", "-s"], input=script, text=True).returncode, 0)
        lite, _ = self.jobs.remote_script(commit="b" * 40, host="powerboat", home="/home/u", edition="lite",
                                         label="", command=["true"], stamp="s", origin="https://example/x")
        self.assertIn("export QUARTUS_ROOTDIR=/home/u/intelFPGA_lite/17.0/quartus", lite)

    def test_the_wait_for_a_slot_is_capped_and_cancellable(self):
        # Codex P2 at 7f7121f88: run could wait for a slot forever.
        holder = subprocess.Popen(["flock", self.pools["lite"][0], "sleep", "30"])
        self.addCleanup(holder.wait)
        self.addCleanup(holder.terminate)
        deadline = time.monotonic() + 10
        while not qt.flock_holders(self.pools["lite"][0]) and time.monotonic() < deadline:
            time.sleep(0.05)
        started = time.monotonic()
        self.assertEqual(self.jobs.run("lite", "", ["true"], sample_seconds=0.1, cap_seconds=1), 124)
        self.assertLess(time.monotonic() - started, 6)
        timer = threading.Timer(0.5, lambda: os.kill(os.getpid(), signal.SIGTERM))
        timer.start()
        try:
            self.assertEqual(self.jobs.run("lite", "", ["true"], sample_seconds=0.1), 128 + signal.SIGTERM)
        finally:
            timer.cancel()

    def test_admission_sleeps_never_pass_the_cap(self):
        # Codex P2 at 3e1144c04: a 30 s retry sleep overran a short cap.
        with mock.patch.object(self.jobs, "_mem_available_kib", return_value=1), \
                mock.patch.object(self.jobs, "ADMISSION_RETRY_SECONDS", 30):
            started = time.monotonic()
            self.assertEqual(self.jobs.run("lite", "", ["true"], sample_seconds=0.1, cap_seconds=1), 124)
        self.assertLess(time.monotonic() - started, 5)

    def test_a_named_host_waits_for_the_same_readiness(self):
        # Codex P1 at 7d0c69357: --host skipped the memory check.
        gib = 1024 ** 2
        answers = [{"host": "powerboat", "free": 1, "available_kib": 2 * gib, "home": "/h", "slots": {}},
                   {"host": "powerboat", "free": 1, "available_kib": 40 * gib, "home": "/h", "slots": {}}]
        with mock.patch.object(self.jobs, "probe", side_effect=answers) as probed, \
                mock.patch.object(self.jobs, "REPROBE_SECONDS", 0), \
                mock.patch.object(self.jobs, "resolve_pushed", return_value="e" * 40), \
                mock.patch.object(self.jobs, "local_host", return_value="elsewhere"), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="https://github.com/DeanoC/fes\n"), \
                mock.patch.object(self.jobs, "_dispatch", side_effect=lambda target, script="": fake_dispatch(0)):
            self.assertEqual(self.jobs.remote("pro", "powerboat", "HEAD", "", ["true"]), 0)
        self.assertEqual(probed.call_count, 2, "it launched on a named host below the memory threshold")

    def test_a_probe_that_times_out_or_answers_garbage_is_one_failed_host(self):
        # Codex P2 at 7d0c69357: one bad host aborted the whole pick.
        with mock.patch.object(self.jobs.subprocess, "run",
                               side_effect=subprocess.TimeoutExpired(["ssh"], 60)):
            self.assertIn("error", self.jobs.probe("chip-30x0-ai-dev", "pro"))
        with mock.patch.object(self.jobs.subprocess, "run",
                               return_value=subprocess.CompletedProcess([], 0, "not json", "")):
            self.assertIn("error", self.jobs.probe("chip-30x0-ai-dev", "pro"))
        gib = 1024 ** 2
        probes = {"chip-30x0-ai-dev": {"host": "chip-30x0-ai-dev", "error": "probe failed: timed out"},
                  "powerboat": {"host": "powerboat", "free": 1, "available_kib": 40 * gib}}
        self.assertEqual(self.pick("pro", probes), "powerboat")

    def test_a_commit_on_another_remote_only_is_not_pushed(self):
        # Codex P2: the build host fetches origin, so only origin counts.
        def fake(argv, **_kwargs):
            listed = "  origin/main\n" if "--list" not in argv else ""
            return subprocess.CompletedProcess(argv, 0, listed, "")
        with mock.patch.object(self.jobs.subprocess, "run", side_effect=fake), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="c" * 40 + "\n"):
            with self.assertRaises(SystemExit):
                self.jobs.resolve_pushed("HEAD")

    def test_a_remote_that_fails_before_the_wrapper_is_recorded_and_notified(self):
        # Codex P2 at c39608e8c: setup failures (disk, clone, fetch) left no
        # outcome. The remote program records one, and the caller notifies.
        script, _ = self.jobs.remote_script(
            commit="d" * 40, host="powerboat", home="/home/u", edition="lite", label="",
            command=["true"], stamp="s", origin="https://example.invalid/none")
        root = Path(self.directory.name)
        fake = root / "bin"
        fake.mkdir()
        (fake / "git").write_text("#!/bin/sh\nexit 128\n")
        (fake / "git").chmod(0o755)
        (fake / "df").write_text("#!/bin/sh\necho Avail; echo 400G\n")
        (fake / "df").chmod(0o755)
        script = script.replace(self.jobs.PROJECTS, str(root / "projects"))
        result = subprocess.run(["bash", "-s"], env=dict(os.environ, PATH=f"{fake}:{os.environ['PATH']}"),
                                capture_output=True, text=True, input=script)
        self.assertNotEqual(result.returncode, 0)
        (run_dir,) = (root / "projects/runs").iterdir()
        self.assertEqual(json.loads((run_dir / "result.json").read_text())["status"], "setup-failed")
        sink = root / "caller-notified"
        with mock.patch.object(self.jobs, "resolve_pushed", return_value="e" * 40), \
                mock.patch.object(self.jobs, "probe", return_value={"host": "powerboat", "free": 1, "home": "/h",
                                                                    "available_kib": 500 * 1024 ** 2, "slots": {}}), \
                mock.patch.object(self.jobs, "local_host", return_value="elsewhere"), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="https://github.com/DeanoC/fes\n"), \
                mock.patch.dict(os.environ, {"FES_QUARTUS_NOTIFY": f"cat > {sink}"}):
            with mock.patch.object(self.jobs, "_dispatch", side_effect=lambda target, script="": fake_dispatch(255)):
                self.assertEqual(self.jobs.remote("lite", "powerboat", "HEAD", "", ["true"]), 255)
        self.assertIn('"remote-failed"', sink.read_text())

    def test_the_remote_cap_covers_waiting_for_a_host(self):
        # Codex P2 at 7b395e311: remote waited for a host forever.
        busy = {"host": "powerboat", "free": 0, "available_kib": 1, "home": "/h", "slots": {}}
        with mock.patch.object(self.jobs, "probe", return_value=busy), \
                mock.patch.object(self.jobs, "REPROBE_SECONDS", 0.05), \
                mock.patch.object(self.jobs, "resolve_pushed", return_value="e" * 40), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="https://github.com/DeanoC/fes\n"):
            started = time.monotonic()
            self.assertEqual(self.jobs.remote("lite", "powerboat", "HEAD", "", ["true"], cap_minutes=0.01), 124)
        self.assertLess(time.monotonic() - started, 10)

    def test_setup_and_the_run_share_one_deadline(self):
        # Codex P2 at 7bfc1b435: setup time was added on top of the cap.
        script, _ = self.jobs.remote_script(
            commit="f" * 40, host="powerboat", home="/h", edition="lite", label="", command=["true"],
            stamp="s", origin="https://example.invalid/x", cap_minutes=5, deadline_epoch=1_000_000)
        self.assertIn("1000000 - $(date +%s)", script)
        self.assertIn("bounded git -C", script)
        self.assertIn("--deadline-epoch 1000000 --", script)
        self.assertEqual(self.jobs.run("lite", "", ["true"], deadline_epoch=time.time() - 1), 124)

    def test_the_caller_hears_a_successful_remote(self):
        # Codex P2 at 7bfc1b435: only failures were notified on the caller.
        sink = Path(self.directory.name) / "caller"
        ready = {"host": "powerboat", "free": 1, "available_kib": 500 * 1024 ** 2, "home": "/h", "slots": {}}
        with mock.patch.object(self.jobs, "probe", return_value=ready), \
                mock.patch.object(self.jobs, "resolve_pushed", return_value="e" * 40), \
                mock.patch.object(self.jobs, "local_host", return_value="elsewhere"), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="https://github.com/DeanoC/fes\n"), \
                mock.patch.dict(os.environ, {"FES_QUARTUS_NOTIFY": f"cat > {sink}"}):
            with mock.patch.object(self.jobs, "_dispatch", side_effect=lambda target, script="": fake_dispatch(0)):
                self.assertEqual(self.jobs.remote("lite", "powerboat", "HEAD", "", ["true"]), 0)
        self.assertIn('"status": "finished"', sink.read_text())

    def test_no_host_answering_is_a_notified_failure(self):
        # Codex P2 at 827ccac13: pick raised before any notification.
        sink = Path(self.directory.name) / "down"
        with mock.patch.object(self.jobs, "probe", side_effect=lambda host, edition, timeout=60:
                               {"host": host, "error": "probe failed: timed out"}), \
                mock.patch.object(self.jobs, "resolve_pushed", return_value="e" * 40), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="https://github.com/DeanoC/fes\n"), \
                mock.patch.dict(os.environ, {"FES_QUARTUS_NOTIFY": f"cat > {sink}"}):
            real_run = subprocess.run
            with mock.patch.object(self.jobs.subprocess, "run", side_effect=lambda argv, **kw: real_run(argv, **kw)):
                self.assertEqual(self.jobs.remote("lite", "auto", "HEAD", "", ["true"]), 255)
        self.assertIn('"remote-failed"', sink.read_text())

    def test_probes_and_waits_fit_the_time_left(self):
        # Codex P2 at 827ccac13: fixed 60 s probes and sleeps overran a cap.
        timeouts = []
        busy = {"free": 0, "available_kib": 1, "home": "/h", "slots": {}}
        with mock.patch.object(self.jobs, "probe", side_effect=lambda host, edition, timeout=60:
                               timeouts.append(timeout) or dict(busy, host=host)), \
                mock.patch.object(self.jobs, "resolve_pushed", return_value="e" * 40), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="https://github.com/DeanoC/fes\n"):
            started = time.monotonic()
            self.assertEqual(self.jobs.remote("lite", "auto", "HEAD", "", ["true"], cap_minutes=0.02), 124)
        self.assertLess(time.monotonic() - started, 5)
        self.assertTrue(all(t <= 1.5 for t in timeouts), timeouts)

    def test_a_cancelled_dispatcher_stops_the_remote_run_and_notifies(self):
        # Codex P2 at 502d9f161: a signal during the ssh call killed the
        # dispatcher and left the remote job running, with no notification.
        sink = Path(self.directory.name) / "cancel"
        ready = {"host": "powerboat", "free": 1, "available_kib": 500 * 1024 ** 2, "home": "/h", "slots": {}}
        run_dir = Path(self.directory.name) / "remote-run"
        run_dir.mkdir()
        # Stands in for the remote program: still in setup (a child working,
        # no pid file yet) when the signal comes, as in a slow clone.
        script = (f"sleep 2.5; echo $$ $(cut -d' ' -f22 /proc/$$/stat) > {run_dir}/run.pid; "
                  f"sleep 30 & echo $! > {run_dir}/child; wait")
        with mock.patch.object(self.jobs, "probe", return_value=ready), \
                mock.patch.object(self.jobs, "resolve_pushed", return_value="e" * 40), \
                mock.patch.object(self.jobs, "local_host", return_value="powerboat"), \
                mock.patch.object(self.jobs, "remote_script", return_value=(script, str(run_dir))), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="https://github.com/DeanoC/fes\n"), \
                mock.patch.dict(os.environ, {"FES_QUARTUS_NOTIFY": f"cat > {sink}"}):
            timer = threading.Timer(0.5, lambda: os.kill(os.getpid(), signal.SIGTERM))
            timer.start()
            try:
                code = self.jobs.remote("lite", "powerboat", "HEAD", "", ["true"])
            finally:
                timer.cancel()
        self.assertEqual(code, 128 + signal.SIGTERM)
        self.assertIn('"status": "cancelled"', sink.read_text())
        time.sleep(0.5)
        for name in ("run.pid", "child"):
            pid = int((run_dir / name).read_text().split()[0])
            alive = Path(f"/proc/{pid}/stat").exists() and Path(f"/proc/{pid}/stat").read_text().split()[2] != "Z"
            self.assertFalse(alive, f"the remote {name} outlived the cancelled dispatcher")

    def test_the_deadline_holds_while_ssh_has_not_started_the_remote_shell(self):
        # Codex P2 at 1406e08bc: a hung ssh kept the dispatcher past its cap.
        ready = {"host": "powerboat", "free": 1, "available_kib": 500 * 1024 ** 2, "home": "/h", "slots": {}}
        with mock.patch.object(self.jobs, "probe", return_value=ready), \
                mock.patch.object(self.jobs, "resolve_pushed", return_value="e" * 40), \
                mock.patch.object(self.jobs, "local_host", return_value="powerboat"), \
                mock.patch.object(self.jobs, "KILL_GRACE_SECONDS", -29), \
                mock.patch.object(self.jobs, "remote_script", return_value=("exec sleep 60", "/nonexistent/run")), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="https://github.com/DeanoC/fes\n"):
            started = time.monotonic()
            self.assertEqual(self.jobs.remote("lite", "powerboat", "HEAD", "", ["true"], cap_minutes=0.03), 124)
        self.assertLess(time.monotonic() - started, 40)

    def test_a_signal_while_choosing_a_host_is_a_notified_cancellation(self):
        sink = Path(self.directory.name) / "cancel-choose"
        busy = {"host": "powerboat", "free": 0, "available_kib": 1, "home": "/h", "slots": {}}
        with mock.patch.object(self.jobs, "probe", return_value=busy), \
                mock.patch.object(self.jobs, "REPROBE_SECONDS", 0.1), \
                mock.patch.object(self.jobs, "resolve_pushed", return_value="e" * 40), \
                mock.patch.object(self.jobs.subprocess, "check_output", return_value="https://github.com/DeanoC/fes\n"), \
                mock.patch.dict(os.environ, {"FES_QUARTUS_NOTIFY": f"cat > {sink}"}):
            timer = threading.Timer(0.5, lambda: os.kill(os.getpid(), signal.SIGTERM))
            timer.start()
            try:
                code = self.jobs.remote("lite", "powerboat", "HEAD", "", ["true"])
            finally:
                timer.cancel()
        self.assertEqual(code, 128 + signal.SIGTERM)
        self.assertIn('"status": "cancelled"', sink.read_text())

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
