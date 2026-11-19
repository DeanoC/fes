#!/usr/bin/env python3
"""List processes matching a token, without the token appearing in a shell's
own command line. A textual pgrep/pkill pattern typed into a shell matches
that shell, which kills it (exit 144) or deadlocks a waiter; reading /proc from
a script file avoids that entirely. See feedback-pkill-self-match.
"""
import os, sys
token = sys.argv[1]
me = {os.getpid(), os.getppid()}
for entry in sorted(os.listdir("/proc")):
    if not entry.isdigit():
        continue
    pid = int(entry)
    if pid in me:
        continue
    try:
        cmd = open(f"/proc/{pid}/cmdline", "rb").read().replace(b"\0", b" ").decode(errors="replace")
        cwd = os.readlink(f"/proc/{pid}/cwd")
    except OSError:
        continue
    if token in cmd:
        try:
            with open(f"/proc/{pid}/stat") as fh:
                starttime = fh.read().split()[21]
        except OSError:
            starttime = "?"
        print(f"{pid:>8}  {cmd[:110]}")
        print(f"          cwd {cwd}")
