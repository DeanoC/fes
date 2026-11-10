#!/usr/bin/env python3
"""Summarise census.sh output as a Markdown table. Usage: summarize.py <out-dir>"""
import re, sys
from pathlib import Path

out = Path(sys.argv[1])
READERS = ["lite2005", "pro2005", "pro2009", "yosys-pc386", "yosys-i386", "yosys-shared", "verilator"]
forms = sorted({p.name for r in READERS for p in (out / r).glob("f*")})

def ids(log, kind):
    return sorted(set(re.findall(rf"^{kind} \((\d+)\)", log, re.M)))

def bus(log, name, width):
    """The constant value of an output bus from Quartus's stuck-pin warnings (13410), MSB first.
    Lite names single bits (v[3]) or one range (slot[1..2]); Pro lists ranges (y[0..1,3,6..8]).
    A bit with no warning is '?'."""
    stuck = {}
    for spec, lvl in re.findall(rf'Pin "{name}\[([\d.,]+)\]" is stuck at (VCC|GND)', log):
        for part in spec.split(","):
            lo, _, hi = part.partition("..")
            for i in range(int(lo), int(hi or lo) + 1):
                stuck[i] = "1" if lvl == "VCC" else "0"
    return "".join(stuck.get(i, "?") for i in range(width - 1, -1, -1))

def cell(reader, form):
    d = out / reader / form
    if not (d / "rc").exists():
        return "not run"
    rc = int((d / "rc").read_text())
    log = (d / "log").read_text(errors="replace") if (d / "log").exists() else ""
    obs = "syn" if form == "f10_synthesis_defined" else "ok"
    if reader.startswith(("lite", "pro")):
        if rc:
            return f"**rejected** (rc {rc}; errors {', '.join(ids(log, 'Error')) or '?'})"
        m = re.search(rf'Pin "{obs}" is stuck at (VCC|GND)', log)
        if m:
            const = "1" if m.group(1) == "VCC" else "0"
        else:
            const = "not constant"
        if form.startswith("f09"):
            text = "".join(p.read_text(errors="replace") for p in sorted(d.glob("census.*.summary")))
            text += "".join(p.read_text(errors="replace") for p in sorted(d.glob("census.syn.rpt")))
            bits = re.search(r"Total block memory bits\s*[:;]\s*([\d,]+)", text)
            const = f"n/a; block memory bits {bits.group(1) if bits else '0 (no RAM section)'}"
        if form == "f06a_size_cast":
            const += f", slot=3'b{bus(log, 'slot', 3)}"
        if form == "f01g_signed_pkg_shift":
            const += f", y=16'b{bus(log, 'y', 16)}"
        if form == "f01f_signed_pkg_value":
            const += f", v=8'b{bus(log, 'v', 8)}"
        crit = sorted(set(re.findall(r"^Critical Warning \((\d+)\)", log, re.M)) - {"20759"})
        if crit:
            const += f" (critical warnings {', '.join(crit)})"
        warn = [w for w in ids(log, "Warning") if w not in {"18236", "20028", "13024", "21074", "292013"}]
        return f"accepted, {obs}={const}" + (f" (warnings {', '.join(warn)})" if warn else "")
    if reader.startswith("yosys"):
        if form == "f09_attr_in_define" and rc == 0:
            return "accepted, ramstyle reaches the memory"
        if form == "f09b_ram_control" and rc == 0:
            return "accepted, no ramstyle (control)"
        if "SAT proof finished - no model found: SUCCESS" in log:
            return f"accepted, {obs}=1 proven"
        if rc and "SAT proof finished" not in log:
            err = re.findall(r"ERROR:\s*(.*)$", log, re.M)
            return f"**rejected** ({err[0][:120] if err else f'rc {rc}'})"
        return f"accepted, {obs}=1 **disproven**"
    if reader == "verilator":
        if rc:
            err = re.findall(r"%Error[^:]*: (?:[^ ]*\.sv:\d+:\d+: )?(.*)$", log, re.M)
            return f"**rejected** ({err[0][:120] if err else f'rc {rc}'})"
        warn = sorted(set(re.findall(r"%Warning-([A-Z0-9_]+)", log)))
        extra = ""
        if (d / "sim.log").exists():
            bad = " ".join(re.findall(r"bad=\d+", (d / "sim.log").read_text()))
            sim_rc = int((d / "sim.rc").read_text()) if (d / "sim.rc").exists() else None
            if sim_rc != 0:
                return f"**wrong** (sim {bad or 'no result'}, rc {sim_rc})"
            extra = "; sim " + bad
        if (d / "preproc").exists():
            extra = "; preprocessed: " + (d / "preproc").read_text().strip()
        return "accepted" + (f" (warnings {', '.join(warn)})" if warn else "") + extra
    return "?"

print("| Form | " + " | ".join(READERS) + " |")
print("| --- |" + " --- |" * len(READERS))
for f in forms:
    print(f"| `{f}` | " + " | ".join(cell(r, f) for r in READERS) + " |")
