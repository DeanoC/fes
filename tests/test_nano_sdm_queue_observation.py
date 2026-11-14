"""Synthetic records and source bindings; no physical acceptance implied."""
import unittest
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "platform/embedded/nano/sdm_trace"))
from queue_observation import decode, parse_queue, FIELDS, HEX
from test_nano_sdm_timing import row as timing_row, line as timing_line, wire as timing_wire
from test_nano_sdm_retire_observation import WIRE as RETIRED, ROW, LINE as RETIRE
from test_nano_sdm_input_observation import BOOT, CONFIG, BUILD
from retire_observation import FIELDS as RETIRE_FIELDS
from input_observation import PARK


def row(**edits):
    result = dict(build=BUILD, owner=17, epoch=7, generation=4, bytes=CONFIG["core_size"], sha384=CONFIG["core_sha384"],
                  phase=6, stage=10, policy=1, completion_seen=1, completion_us=6000, attempted=1, initialized=1, submitted=1, failed=0, error=0,
                  timed=1, start_us=5000, last_us=8000, observed_us=8000, samples=40, slots=6,
                  published=1, polls=1, reads=0, read_samples=100, last_offset=0x9C, last_value=12,
                  consumer_samples=10, consumer=12, consumer_read=100, consumer_phase=6,
                  consumer_stage=10, consumer_poll=1, retained=1, reuse=0, activated=0)
    result.update(edits)
    return result


def line(result):
    return ("FES_NANO_RETIRE_QUEUE " + " ".join(key + "=" + (format(result[key], "x") if key in HEX else str(result[key])) for key in FIELDS) + "\n").encode()


def wire(result):
    return timing_wire(timing_row()).replace(PARK, line(result) + PARK)


def failed_wire(result):
    retired = dict(ROW, status=-1, phase=4, retired=0, first_error=-3, writes=4,
                   producer=0, consumer=0, elapsed_us=0, owner="0", epoch="0", generation=0)
    record = ("FES_NANO_RETIRE_RESULT " + " ".join(k + "=" + str(retired[k]) for k in RETIRE_FIELDS) + "\n").encode()
    timing = timing_row(phase=4, mask="1f", writes=4, sources=3, validated=3,
                        last_us=100000, observed_us=120000,
                        first_us=[1000, 2000, 3000, 4000, 5000, 0, 0],
                        latest_us=[1900, 2900, 3900, 4900, 120000, 0, 0])
    return RETIRED.replace(RETIRE, record + timing_line(timing) + line(result))


class QueueObservationTests(unittest.TestCase):
    def decode(self, raw):
        return decode(raw, boot=BOOT, build=BUILD, configuration=CONFIG)

    def test_complete_record_requires_existing_source_and_retirement_proofs(self):
        result = self.decode(wire(row()))
        self.assertTrue(result["passed"] and result["queue_observation_validated"] and result["retirement_verified"])
        self.assertEqual(result["queue_stage"], "complete")
        self.assertFalse(result["queue_sample_is_completion_proof"] or result["buffer_reuse_authority"] or result["DMA_authority"] or result["ordinary_runtime_activation"])

    def test_raw_consumers_and_late_clocks_never_promote_failed_retirement(self):
        for consumer in (6, 7, 9, 11, 12, 16, 0xDEADBEEF):
            q = row(phase=4, stage=6, completion_seen=0, completion_us=0, failed=1, error=-3, last_us=6000, observed_us=120000,
                    read_samples=90, consumer=consumer, consumer_read=89, consumer_phase=4, consumer_stage=6)
            result = self.decode(failed_wire(q))
            self.assertTrue(result["source_flow_verified"] and result["queue_observation_validated"], result["errors"])
            self.assertEqual(result["queue"]["consumer"], consumer)
            self.assertFalse(result["passed"] or result["retirement_verified"] or result["buffer_reuse_authority"] or result["queue_sample_is_completion_proof"])

    def test_identity_authority_bounds_and_success_relations(self):
        changes = dict(build="0" * 16, owner=18, epoch=8, generation=5, bytes=64, sha384="0" * 96,
                       phase=5, stage=9, policy=0, completion_seen=0, completion_us=8001, attempted=0, initialized=0, submitted=0, failed=1, error=-3,
                       timed=0, start_us=9000, last_us=9000, observed_us=9000, samples=0, slots=5,
                       published=0, polls=0, read_samples=101, last_offset=3, consumer_samples=101,
                       consumer=6, consumer_read=101, consumer_phase=5, consumer_stage=9,
                       consumer_poll=2, retained=0, reuse=1, activated=1)
        for key, value in changes.items():
            with self.subTest(field=key):
                self.assertFalse(self.decode(wire(row(**{key: value})))["passed"])

    def test_missing_duplicate_reordered_truncated_unknown_or_late_reject(self):
        q = line(row())
        good = wire(row())
        t = timing_line(timing_row())
        cases = (good.replace(q, b""), good.replace(q, q + q), good.replace(t + q, q + t),
                 good[:-1], good + b"late\n", good.replace(q, q + b"unknown\n"))
        for raw in cases:
            with self.subTest(tail=raw[-100:]), self.assertRaises(ValueError):
                self.decode(raw)
        tokens = q.rstrip().split()
        for i in range(1, len(tokens)):
            with self.subTest(missing=i), self.assertRaises(ValueError):
                parse_queue(b" ".join(tokens[:i] + tokens[i + 1:]))
        with self.assertRaises(ValueError):
            parse_queue(q.replace(b" slots=", b" unknown=0 slots="))

    def test_typed_integer_limits_and_invalid_identity_reject(self):
        for edits in (dict(consumer=1 << 32), dict(stage=1 << 32), dict(start_us=1 << 64),
                      dict(error=1 << 31), dict(error=-(1 << 31) - 1), dict(build="g" * 16)):
            with self.subTest(edits=edits), self.assertRaises(ValueError):
                parse_queue(line(row(**edits)))

    def test_unsampled_raw_values_cannot_be_invented(self):
        for q in (row(consumer_samples=0), row(read_samples=0), row(consumer_read=0), row(attempted=0)):
            self.assertFalse(self.decode(wire(q))["passed"])

    def test_notice_prefix_and_no_completion_from_queue_only(self):
        q = line(row())
        self.assertEqual(parse_queue(q), parse_queue(b"NOTICE:  " + q))
        with self.assertRaises(ValueError):
            self.decode(q + PARK)

    def test_late_final_validation_keeps_bounded_consumption_and_retirement(self):
        retired = dict(ROW, elapsed_us=160000)
        record = ("FES_NANO_RETIRE_RESULT " + " ".join(k + "=" + str(retired[k]) for k in RETIRE_FIELDS) + "\n").encode()
        timing = timing_row(last_us=161000, observed_us=161000,
                            first_us=[1000, 2000, 3000, 4000, 5000, 130000, 150000],
                            latest_us=[1900, 2900, 3900, 4900, 120000, 139000, 161000])
        q = row(last_us=120000, observed_us=120000)
        raw = RETIRED.replace(RETIRE, record + timing_line(timing) + line(q))
        result = self.decode(raw)
        self.assertTrue(result["passed"] and result["queue_observation_validated"], result["errors"])
        self.assertFalse(result["buffer_reuse_authority"] or result["ordinary_runtime_activation"])
        for edits in (dict(completion_seen=0, completion_us=0), dict(completion_us=105001),
                      dict(last_us=1005001, observed_us=1005001), dict(policy=0),
                      dict(completion_us=120001)):
            with self.subTest(edits=edits):
                self.assertFalse(self.decode(raw.replace(line(q), line(dict(q, **edits))))["passed"])

    def test_old_record_cannot_supply_new_completion_policy(self):
        good = line(row())
        old = b" ".join(t for t in good.rstrip().split() if not t.startswith((b"policy=", b"completion_seen=", b"completion_us="))) + b"\n"
        with self.assertRaises(ValueError):
            self.decode(wire(row()).replace(good, old))


if __name__ == "__main__":
    unittest.main()
