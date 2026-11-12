"""D12 check for #1747: build fes.coleco with this PR's tooling and main's BUILD_ID.

Usage, from sources/misteross of a clean checkout at the PR commit:

    python3 ../../docs/validation/2026-11-12-1747-toolchain-installs/forced_build_id.py \
        <main's build-summary.json> <CACHE_ROOT>

It runs the registered producer's build() (native-video shell, HIP) unchanged
except that build_identity returns main's BUILD_ID in place of the one the
PR's build record gives. The producer still authenticates its tools from the
PR's per-tool installs and runs its own synthesis, placement search, route,
timing and seal checks. Compare build/fes-coleco-native-video/core.rbf and
synth.json with main's.
"""
import json
import sys
from pathlib import Path

root = Path.cwd().resolve()
sys.path.insert(0, str(root))
from scripts import build_fes_coleco_socket_v2 as producer  # noqa: E402

main_id = json.loads(Path(sys.argv[1]).read_text())["build_id"]
producer.build_identity = lambda record: main_id
print("forced build_id", main_id, flush=True)
print(producer.build(root, cache_root=Path(sys.argv[2]), native_video=True), flush=True)
