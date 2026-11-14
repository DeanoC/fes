#!/usr/bin/env python3
"""The session HIL drivers' run loop and liveness check, as they are today.

Every session's kit driver is a copy of its predecessor (`hil_games.py`,
`hil_x87.py`, `hil_m5g.py` ...), and they all carry the loop below: it is
extracted here, behaviour for behaviour, so that the harness failures of
14 November 2026 have somewhere to be tested. The cases in
`sources/misteross/tests/test_kit_hil.py` say what each of those failures
cost; against this extraction they fail, which is the point of this commit.
Issue #1849 lists them.

Nothing imports this module yet.
"""
import json
import subprocess
import sys
import time
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
import kit as kitlib  # noqa: E402,F401

IDLE_LIMIT = 1800.0


def drain(source, execute, *, mode='batch', idle_limit=IDLE_LIMIT, heartbeat=None,
          log=print, describe=None, now=time.monotonic, sleep=time.sleep, run_limit=None):
    """Run the commands `source()` offers, as the drivers do today.

    Exhausting the command file waits for one to be appended: silently, and for
    half an hour, whether or not any more are coming. The #1368 session read
    that silence as a dead wrapper.

    A command that fails is recorded and the script carries on, so the i386
    x87 run kept running over a core the kit had refused at admission.
    """

    done = 0
    began = idle_since = now()
    while True:
        lines = source().split('\n')
        if done >= len(lines):
            if now() - idle_since > idle_limit:
                raise SystemExit('no command for 30 minutes')
            if run_limit is not None and now() - began > run_limit:
                raise SystemExit('run time limit')
            sleep(0.5)
            continue
        line = lines[done]
        done += 1
        if not line.strip():
            continue
        op = json.loads(line)
        if op.get('op') == 'quit':
            return done, []
        try:
            execute(op)
        except Exception as error:  # noqa: BLE001 - recorded, and the script goes on
            log(f'{type(error).__name__}: {error}')
        idle_since = now()


def wrapper_alive(pattern):
    """Is the run's wrapper still alive? Matched by command line, as today."""

    found = subprocess.run(['pgrep', '-af', pattern], stdin=subprocess.DEVNULL,
                           capture_output=True, text=True, timeout=30)
    return bool(found.stdout.strip())
