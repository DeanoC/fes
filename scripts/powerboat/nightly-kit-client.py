#!/usr/bin/env python3
"""Synchronous protected-runner adapter for Powerboat kit acceptance."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

request = sys.stdin.buffer.read()
binding = hashlib.sha256(request).hexdigest()
raw = subprocess.check_output(["ssh", "-oBatchMode=yes", "powerboat",
                               ".local/share/fes-qualification/bin/nightly-kit-worker.py", binding])
evidence = json.loads(raw)
if evidence["request_sha256"] != binding or evidence["request"] != json.loads(request):
    sys.exit("unexpected kit worker request identity")
identity = hashlib.sha256(raw).hexdigest()
directory = Path(os.environ["FES_NIGHTLY_STORE"]) / "evidence"
with tempfile.NamedTemporaryFile(dir=directory, delete=False) as f:
    f.write(raw)
    f.flush()
    os.fsync(f.fileno())
os.replace(f.name, directory / identity)
print(json.dumps(dict(request_sha256=binding, evidence_sha256=identity,
                      hil_check_sha256=evidence["smoke"].get("hil_check_sha256"),
                      verdict=evidence["smoke"]["verdict"], kit=evidence["kit"], restored=evidence["restored"])))
