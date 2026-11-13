"""Source-bound raw queue observations; never a DMA or reuse grant."""
import importlib.util
import re
from pathlib import Path

_spec = importlib.util.spec_from_file_location("queue_bound_timing", Path(__file__).with_name("timing_observation.py"))
_timing = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(_timing)
FIELDS = """build owner epoch generation bytes sha384 phase stage policy completion_seen completion_us attempted initialized submitted
failed error timed start_us last_us observed_us samples slots published polls reads read_samples
last_offset last_value consumer_samples consumer consumer_read consumer_phase consumer_stage
consumer_poll retained reuse activated""".split()
HEX = set("owner epoch last_offset last_value consumer".split())
U64 = set("owner epoch generation start_us last_us observed_us completion_us".split())
STAGES = ("idle", "initial-first", "initial-second", "store", "prepublication",
          "publication", "progress", "final-first", "final-second", "final-guard", "complete")


def require(ok, message):
    if not ok:
        raise ValueError(message)


def parse_queue(line):
    require(type(line) is bytes and len(line) <= 2048 and b"\0" not in line, "bounded queue line required")
    line = re.sub(rb"^NOTICE:[ \t]+", b"", line.rstrip(b"\r\n"))
    require(line.startswith(b"FES_NANO_RETIRE_QUEUE "), "queue prefix required")
    pairs = [p.split("=", 1) for p in line.decode("ascii").split()[1:]]
    require(all(len(p) == 2 for p in pairs) and [p[0] for p in pairs] == FIELDS, "complete ordered queue fields required")
    result = {}
    for key, value in pairs:
        if key in ("build", "sha384"):
            require(re.fullmatch("[0-9a-f]{" + str(16 if key == "build" else 96) + "}", value), "bounded queue identity required")
            result[key] = value
        else:
            pattern = r"-?[0-9]{1,10}" if key == "error" else r"[0-9a-f]{1,16}" if key in HEX else r"[0-9]{1,20}"
            require(re.fullmatch(pattern, value), "bounded queue integer required")
            n = int(value, 16 if key in HEX else 10)
            require(-(1 << 31) <= n < (1 << 31) if key == "error" else 0 <= n < (1 << (64 if key in U64 else 32)), "queue integer overflow")
            result[key] = n
    return result


def queue_errors(q, timing):
    errors = []

    def check(ok, message):
        if not ok:
            errors.append(message)

    source = timing["retirement_observation"]["input_observation"]["result"]
    t = timing["timing"]
    for key in ("build", "owner", "epoch", "generation", "bytes", "sha384"):
        check(q[key] == source[key], "queue/source " + key + " differs")
    check(q["phase"] == t["phase"] and q["phase"] <= 6, "queue/retirement phase differs")
    check(q["stage"] < len(STAGES) and q["consumer_stage"] < len(STAGES), "queue stage invalid")
    check(q["policy"] in (0, 1) and q["completion_seen"] in (0, 1), "queue completion policy invalid")
    if q["submitted"]:
        check(q["policy"] == 1, "retirement requires its explicit final-validation policy")
    if q["completion_seen"]:
        check(q["stage"] >= 7 and q["timed"] == 1 and q["start_us"] <= q["completion_us"] <= q["last_us"], "completion sample order invalid")
        check(q["completion_us"] - q["start_us"] <= 100000, "command consumption exceeded original bound")
    else:
        check(q["completion_us"] == 0 and q["stage"] <= 6, "unobserved completion has sample/stage")
    for key in ("attempted", "initialized", "submitted", "failed", "timed", "published"):
        check(q[key] in (0, 1), "queue boolean invalid: " + key)
    check(q["submitted"] <= q["initialized"] <= q["attempted"], "queue attempt order invalid")
    check(q["slots"] <= 6 and q["polls"] <= 128, "queue slot/poll bound invalid")
    check(q["read_samples"] <= t["reads"] and q["consumer_samples"] <= q["read_samples"], "raw read/sample counts invalid")
    check(q["consumer_read"] <= t["reads"] and q["consumer_poll"] <= q["polls"], "consumer read/poll bound invalid")
    check(q["consumer_phase"] <= q["phase"], "consumer phase invalid")
    check(q["last_offset"] % 4 == 0 and q["last_offset"] <= 0x100AC, "raw read offset invalid")
    if q["consumer_samples"]:
        check(q["consumer_read"] > 0, "consumer sample lacks read position")
    else:
        check(all(q[k] == 0 for k in ("consumer", "consumer_read", "consumer_phase", "consumer_stage", "consumer_poll")), "unsampled consumer has values")
    if not q["read_samples"]:
        check(q["last_offset"] == q["last_value"] == 0, "unsampled read has values")
    if not q["attempted"]:
        check(all(q[k] == 0 for k in ("stage", "policy", "completion_seen", "completion_us", "initialized", "submitted", "failed", "error", "timed", "start_us", "last_us", "observed_us", "samples", "slots", "published", "polls", "reads")), "unattempted queue has activity")
    if q["published"]:
        check(q["slots"] == 6 and q["stage"] >= 5, "publication lacks six slots")
    if q["polls"]:
        check(q["published"] == 1 and q["stage"] >= 6, "poll precedes publication")
    if q["timed"]:
        check(q["samples"] > 0 and q["start_us"] <= q["last_us"], "accepted queue clock invalid")
        check(t["start_us"] <= q["start_us"] and q["last_us"] <= t["observed_us"], "queue clock outside retirement observations")
    else:
        check(q["start_us"] == q["last_us"] == 0, "untimed queue has accepted clocks")
    check((q["retained"], q["reuse"], q["activated"]) == (1, 0, 0), "queue authority fields invalid")
    if timing["retirement_verified"]:
        check((q["stage"], q["attempted"], q["initialized"], q["submitted"], q["failed"], q["error"], q["timed"], q["slots"], q["published"]) == (10, 1, 1, 1, 0, 0, 1, 6, 1), "successful queue state incomplete")
        check(q["completion_seen"] == 1, "successful queue lacks bounded completion sample")
        check(0 < q["polls"] <= 128 and q["consumer_samples"] > 0 and q["consumer"] == 12, "successful queue consumer incomplete")
        check(q["consumer_phase"] == 6 and q["consumer_stage"] == 10, "successful final consumer phase differs")
        check(q["last_us"] == q["observed_us"] and q["last_us"] - q["start_us"] <= (1000000 if q["policy"] == 1 else 100000), "successful queue clock invalid")
    return errors


def decode(raw, *, boot, build, configuration, _terminal=None):
    require(type(raw) is bytes and 0 < len(raw) <= 1 << 20 and raw.endswith(b"\n") and b"\0" not in raw, "complete bounded queue capture required")
    parts = raw.splitlines(keepends=True)
    lines = [re.sub(rb"^NOTICE:[ \t]+", b"", v.rstrip(b"\r\n")) for v in parts]
    rows = [i for i, v in enumerate(lines) if b"FES_NANO_RETIRE_QUEUE" in v]
    require(len(rows) == 1, "one queue record required")
    at = rows[0]
    require(at >= 1 and at == len(parts) - 2 and lines[at - 1].startswith(b"FES_NANO_RETIRE_TIMING "), "queue must follow timing before final Park")
    q = parse_queue(lines[at])
    timing = _timing.decode(b"".join(parts[:at] + parts[at + 1:]), boot=boot, build=build, configuration=configuration, _terminal=_terminal)
    errors = queue_errors(q, timing)
    valid = timing["timing_validated"] and not errors
    return dict(timing, passed=timing["passed"] and valid, retirement_verified=timing["retirement_verified"] and valid,
                queue_observation_validated=valid, queue=q, queue_stage=STAGES[q["stage"]] if q["stage"] < len(STAGES) else "invalid",
                queue_sample_is_completion_proof=False, errors=timing["errors"] + errors)
