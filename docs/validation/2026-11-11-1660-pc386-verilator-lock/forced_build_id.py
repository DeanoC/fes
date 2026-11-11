"""D12 check for #1660: the PR's lock and slot with main's BUILD_ID.

Usage, from sources/misteross of a clean checkout at the PR commit, with QUARTUS_ROOTDIR set as for
`make build-fes-pc386-quartus`:

    python3 forced_build_id.py <main's build-summary.json> <CACHE_ROOT>

It runs the producer's own steps (tool authentication, write_project, firmware_rom,
elaborate_machine, run_quartus, validate_build_evidence) into build/fes-pc386-quartus, the
producer's output path, with the BUILD_ID taken from main's build summary. It exports no package.
Compare build/fes-pc386-quartus/core.rbf with main's core.rbf."""
import json
import sys
from pathlib import Path

root = Path.cwd().resolve()
sys.path.insert(0, str(root))
from scripts import build_fes_pc386_quartus as q  # noqa: E402
from scripts import fes_build_common as board  # noqa: E402

main_id = json.loads(Path(sys.argv[1]).read_text())["build_id"]
tools = q._authenticate_tools(root, cache_root=Path(sys.argv[2]))
print("yosys", tools["identities"]["yosys"])
print("build_id", main_id, flush=True)
output = board._prepare_output(root, relative=q.OUTPUT, build_outputs=q.BUILD_OUTPUTS)
project = q.write_project(root, output, main_id, 1)
q.firmware_rom(root)
q.elaborate_machine(root, output, project, tools["yosys"].path)
q.run_quartus(tools["quartus_sh"], tools["quartus_root"], project, output / "quartus.log")
evidence = q.validate_build_evidence(output, project)
(output / "forced-summary.json").write_text(
    json.dumps({"build_id": main_id, "tools": tools["identities"], **evidence}, indent=2, sort_keys=True) + "\n")
print("done", flush=True)
