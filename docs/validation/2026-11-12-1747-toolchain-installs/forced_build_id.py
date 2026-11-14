"""D12 check for #1747: run a registered producer with main's BUILD_ID.

Usage, from sources/misteross of a clean checkout at the PR commit:

    python3 ../../docs/validation/2026-11-12-1747-toolchain-installs/forced_build_id.py \
        <producer module> <main's build-summary.json> -- <producer arguments>

for example `scripts.build_fes_pong ../main/build-summary.json -- --cache-root /abs/cache`.

It runs the producer's own command line unchanged except for two things:

- build_identity returns main's BUILD_ID in place of the one the PR's build
  record gives;
- the final package export (and Pong's check of that export) is skipped,
  because it rightly refuses a manifest whose build.id differs from the record.

The producer still authenticates its tools from the PR's per-tool installs and
runs its own synthesis, placement search, route, timing and source and
tool-identity checks. Compare the output's core.rbf and synth.json with main's.
"""
import importlib
import json
import sys
from pathlib import Path

root = Path.cwd().resolve()
sys.path.insert(0, str(root))
separator = sys.argv.index("--")
module_name, summary = sys.argv[1:separator]
arguments = sys.argv[separator + 1:]
producer = importlib.import_module(module_name)
main_id = json.loads(Path(summary).read_text())["build_id"]
producer.build_identity = lambda record: main_id
producer.export_package = lambda encoded, rbf, *args, **kwargs: rbf
if hasattr(producer, "validate_exported_package"):
    producer.validate_exported_package = lambda directory, *args: directory
print("forced build_id", main_id, flush=True)
sys.argv = [producer.__file__, *arguments]
result = producer.main()
raise SystemExit(result)
