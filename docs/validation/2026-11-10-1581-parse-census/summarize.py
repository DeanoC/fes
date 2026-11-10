#!/usr/bin/env python3
"""Summarise census.sh output as a Markdown table, or check that a run is complete.

  summarize.py <out-dir>            print results.md's table
  summarize.py --check <out-dir>    exit 1, naming each cell, unless every cell of the readers and
                                    forms in READERS and FORMS (default: all) is a result

A cell counts as a result only when the reader's own verdict is in its output: Quartus's
"... was successful" line and its summary file, or an "Error (NNNNN)" message; Yosys's
"End of script." line, or an "ERROR:" message; Verilator's report banner, or a "%Error" message.
Anything else (no output, a timeout, a crash, a tool that never ran) is "not run", never a
rejection. The rows are the forms in forms/, not whatever directories the output holds.
"""
import os
import re
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
READERS = ["lite2005", "pro2005", "pro2009", "yosys-pc386", "yosys-i386", "yosys-shared", "verilator"]
FORMS = sorted(p.stem for p in (HERE / "forms").glob("f*.sv"))
QUARTUS_QUIET = {"18236", "20028", "13024", "21074", "292013"}


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


def read(path):
    return path.read_text(errors="replace") if path.exists() else ""


def number(path):
    try:
        return int(path.read_text().strip())
    except (OSError, ValueError):
        return None


def not_run(why):
    return False, f"**not run** ({why})"


def quartus(d, form, rc, log):
    if rc != 0:
        errors = [e for e in ids(log, "Error")]
        if not errors or "was unsuccessful" not in log:
            return not_run(f"rc {rc}, no Quartus error message")
        return True, f"**rejected** (rc {rc}; errors {', '.join(errors)})"
    summary = "".join(read(p) for p in sorted(d.glob("census.*.summary")))
    if "was successful" not in log or not summary:
        return not_run("rc 0, no Quartus report")
    obs = "syn" if form == "f10_synthesis_defined" else "ok"
    m = re.search(rf'Pin "{obs}" is stuck at (VCC|GND)', log)
    const = ("1" if m.group(1) == "VCC" else "0") if m else "not constant"
    if form.startswith("f09"):
        bits = re.search(r"Total block memory bits\s*[:;]\s*([\d,]+)", summary + read(d / "census.syn.rpt"))
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
    warn = [w for w in ids(log, "Warning") if w not in QUARTUS_QUIET]
    return True, f"accepted, {obs}={const}" + (f" (warnings {', '.join(warn)})" if warn else "")


def yosys(d, form, rc, log):
    obs = "syn" if form == "f10_synthesis_defined" else "ok"
    if rc == 0:
        if "End of script." not in log:
            return not_run("rc 0, no Yosys result")
        if form == "f09_attr_in_define":
            return True, "accepted, ramstyle reaches the memory"
        if form == "f09b_ram_control":
            return True, "accepted, no ramstyle (control)"
        if "SAT proof finished - no model found: SUCCESS" in log:
            return True, f"accepted, {obs}=1 proven"
        return not_run("rc 0, no SAT result")
    if "SAT proof finished - model found: FAIL" in log:
        return True, f"accepted, {obs}=1 **disproven**"
    err = re.findall(r"ERROR:\s*(.*)$", log, re.M)
    if not err:
        return not_run(f"rc {rc}, no Yosys error message")
    return True, f"**rejected** ({err[0][:120]})"


def verilator(d, form, rc, log):
    if rc != 0:
        err = re.findall(r"%Error[^:]*: (?:[^ ]*\.sv:\d+:\d+: )?(.*)$", log, re.M)
        if not err:
            return not_run(f"rc {rc}, no Verilator error message")
        return True, f"**rejected** ({err[0][:120]})"
    if "V e r i l a t i o n   R e p o r t" not in log:
        return not_run("rc 0, no Verilator report")
    warn = sorted(set(re.findall(r"%Warning-([A-Z0-9_]+)", log)))
    extra = ""
    if form == "f04_struct_shadow":
        # The struct read is judged by simulation: the bench must have run to its count.
        sim_rc, bad = number(d / "sim.rc"), re.findall(r"bad=(\d+)", read(d / "sim.log"))
        if sim_rc is None or sim_rc >= 124 or not bad:
            return not_run(f"simulation rc {sim_rc}")
        if int(bad[0]) != 0:
            return True, f"**wrong** (sim bad={bad[0]}, rc {sim_rc})"
        if sim_rc != 0:
            return not_run(f"simulation rc {sim_rc} with bad=0")
        extra = "; sim bad=0"
    if form == "f10_synthesis_defined":
        pre, pre_rc = read(d / "preproc").strip(), number(d / "preproc.rc")
        if pre_rc != 0 or "assign syn" not in pre:
            return not_run(f"preprocess rc {pre_rc}")
        extra = "; preprocessed: " + pre
    return True, "accepted" + (f" (warnings {', '.join(warn)})" if warn else "") + extra


def cell(out, reader, form):
    d = out / reader / form
    rc = number(d / "rc")
    if rc is None:
        return not_run("no result")
    if rc >= 124:
        return not_run(f"rc {rc}: timeout, tool not run or killed")
    log = read(d / "log")
    if reader.startswith(("lite", "pro")):
        return quartus(d, form, rc, log)
    if reader.startswith("yosys"):
        return yosys(d, form, rc, log)
    return verilator(d, form, rc, log)


def main():
    args = sys.argv[1:]
    check = args[:1] == ["--check"]
    out = Path(args[-1])
    if check:
        readers = os.environ.get("READERS", "").split() or READERS
        forms = os.environ.get("FORMS", "").split() or FORMS
        bad = [f"{r} {f}: {text}" for r in readers for f in forms for ok, text in [cell(out, r, f)] if not ok]
        # Control: every reader accepts f08_no_carry. A reader whose control cell is anything else
        # (a licence or set-up error carries a real error id) has no trustworthy column.
        if "f08_no_carry" in forms:
            for r in readers:
                ok, text = cell(out, r, "f08_no_carry")
                if ok and not text.startswith("accepted"):
                    bad.append(f"{r} control f08_no_carry: {text}")
        for line in bad:
            print(f"not a result: {line}", file=sys.stderr)
        sys.exit(1 if bad else 0)
    marker = sorted(p.name for p in out.glob("ALL_DONE.*"))
    if "ALL_DONE.all" not in marker:
        print(f"Incomplete run: {out} has no ALL_DONE.all ({', '.join(marker) or 'no marker'}).\n")
    print("| Form | " + " | ".join(READERS) + " |")
    print("| --- |" + " --- |" * len(READERS))
    for f in FORMS:
        print(f"| `{f}` | " + " | ".join(cell(out, r, f)[1] for r in READERS) + " |")


if __name__ == "__main__":
    main()
