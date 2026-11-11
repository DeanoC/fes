"""The timing gate the Z80 Quartus sealers apply to a TimeQuest 17.0.2 report.

It reads the Multicorner Timing Analysis Summary and the Unconstrained Paths
Summary. A report passes only when every analysis's worst slack and TNS are
non-negative and TimeQuest analysed every clock. A miss names the analysis,
its worst slack and TNS, and the clock that holds that worst slack.
"""

from __future__ import annotations

import math
import re

TIMING_CATEGORIES = ("Setup", "Hold", "Recovery", "Removal", "Minimum Pulse Width")
_VALUE = r"(-?[0-9]+(?:\.[0-9]+)?|N/A)"
_ROW_TAIL = r"\s*;\s*".join([_VALUE] * len(TIMING_CATEGORIES))
SUMMARY_ROW_RE = re.compile(rf"^;\s*(\S.*?)\s*;\s*{_ROW_TAIL}\s*;\s*$", re.M)
WORST_SLACK = "Worst-case Slack"
DESIGN_TNS = "Design-wide TNS"
# Registers on a clock TimeQuest cannot analyse are in no slack row.
UNANALYSED_CLOCKS = ("Illegal Clocks", "Unconstrained Clocks")
UNANALYSED_ROW_RE = re.compile(r"^; (Illegal Clocks|Unconstrained Clocks)\s*; (\d+)\s*; (\d+)\s*;", re.M)


def _summary(sta_text: str, error: type[Exception]) -> tuple[tuple, tuple, list[tuple]]:
    """The design-wide slack and TNS rows and the per-clock slack rows."""
    rows = SUMMARY_ROW_RE.findall(sta_text)
    labels = [row[0] for row in rows]
    if labels.count(WORST_SLACK) != 1 or labels.count(DESIGN_TNS) != 1:
        raise error("timing summary lacks required analysis categories")
    slack_index, tns_index = labels.index(WORST_SLACK), labels.index(DESIGN_TNS)
    if tns_index < slack_index:
        raise error("timing summary lacks required analysis categories")
    slack, tns = rows[slack_index][1:], rows[tns_index][1:]
    if "N/A" in slack or "N/A" in tns:
        raise error("timing summary lacks required analysis categories")
    return slack, tns, [row for row in rows[slack_index + 1:tns_index]]


def _number(text: str, error: type[Exception]) -> float:
    value = float(text)
    if not math.isfinite(value):
        raise error("invalid timing number")
    return value


def _signed(text: str, error: type[Exception]) -> tuple[bool, float]:
    """A value with its printed sign, so -0.000 (a miss) differs from 0.000."""
    return text.startswith("-"), _number(text, error)


def _worst_clock(clock_rows: list[tuple], column: int, slack_text: str, error: type[Exception]) -> str:
    """The clocks whose slack in this analysis is the worst-case slack."""
    worst = _signed(slack_text, error)
    clocks = [row[0] for row in clock_rows
              if row[column + 1] != "N/A" and _signed(row[column + 1], error) == worst]
    return " and ".join(clocks) if clocks else "an unreported clock"


def validate_timing_report(sta_text: str, error: type[Exception]) -> list[dict[str, object]]:
    """Each analysis's worst slack and TNS; raises ``error`` on any miss."""
    slack_row, tns_row, clock_rows = _summary(sta_text, error)
    result, misses = [], []
    for column, name in enumerate(TIMING_CATEGORIES):
        slack_text, tns_text = slack_row[column], tns_row[column]
        slack, tns = _number(slack_text, error), _number(tns_text, error)
        # A miss under half a picosecond prints as -0.000, so read the sign.
        if slack_text.startswith("-") or tns_text.startswith("-"):
            clock = _worst_clock(clock_rows, column, slack_text, error)
            misses.append(f"{name} worst slack {slack_text} ns on {clock} (TNS {tns_text} ns)")
        result.append({"type": name, "slack_ns": slack, "tns_ns": tns})
    if misses:
        raise error("timing miss: " + "; ".join(misses))
    unanalysed = UNANALYSED_ROW_RE.findall(sta_text)
    if sorted(row[0] for row in unanalysed) != sorted(UNANALYSED_CLOCKS):
        raise error("timing report lacks the unconstrained paths summary")
    for row, setup, hold in unanalysed:
        if int(setup) or int(hold):
            raise error(f"timing report has {row.lower()}: {setup} setup, {hold} hold")
    return result
