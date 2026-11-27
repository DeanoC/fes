import gzip
import hashlib
import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "bidi_replay", ROOT / "platform/embedded/nano/mac_spl/verify_bidirectional.py"
)
BIDI = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BIDI)
IDENTITY = "5abcdef055000009"


def trial():
    # Retained real CBL boot prologue and Park checkpoint layout. All request,
    # descriptor and grant observations below are explicitly a software fixture.
    old = (
        gzip.decompress((ROOT / "docs/validation/2026-11-27-de25-native-ethernet/tx07-uart.gz").read_bytes())
        .decode()
        .replace("\r\n", "\n")
        .replace("74313bc05ee1a048", IDENTITY)
        .splitlines()
    )
    head = old[: next(i for i, s in enumerate(old) if s.startswith("FES_CBL_TX_CAPS"))]
    suffix = old[next(i for i, s in enumerate(old) if " stage=30 " in s) : -1] + [BIDI.PARK]
    blocks = []
    frames = []
    phy = " id1=0022 id2=1622 bmcr=1140 bmsr_latch=796d bmsr=796d adv=01e1 partner=cde1 gig_adv=0300 gig_partner=3c00 pcs=0004 resolved=0f49"
    summary = (
        " rc=0 stage=8 arm=1 mapped=1 mac=1 possible=1 dispatch=1 tps=1 revoked=1 reset=1 disposition=closed writes=1"
        " terminal_per0=ff7fbfff terminal_route=00000003 terminal_sid=00010001 terminal_cr0=0000000d terminal_ack=0000000d"
        " terminal_ste=00000001 reg=10d11024 value=ff7fbfff desc=3000003c dma=00000001 fault_reg=00000000 fault_value=00000000"
        " phy=0f49 drain_dma=00000000 drain_mtl=00000000 drain_status=00000002"
    )
    for n in (1, 2):
        nonce = bytes([n]) * 16
        request = bytes.fromhex("02ee1234567802fe2508cb0188b5") + b"FES-CBL-TX-v1\0" + nonce + bytes(16)
        reply = request[6:12] + request[:6] + request[12:22] + b"R" + request[23:]
        frames.append(
            dict(
                nonce_hex=nonce.hex(),
                request_sha256=hashlib.sha256(request).hexdigest(),
                reply_sha256=hashlib.sha256(reply).hexdigest(),
            )
        )
        blocks.append(
            [
                f"FES_CBL_BIDI BEGIN id={IDENTITY} round={n} vmid={n} nonce={nonce.hex()}",
                f"FES_CBL_BIDI GRANT id={IDENTITY} round={n} vmid={n} previous={n - 1} cmd={6 if n == 1 else 2} ste=0000000d",
                f"FES_CBL_TX_CAPS id={IDENTITY} version=00107631 feature0=cafd5b3f feature1=49ef7a27 feature2=221c71c7 interface=00000001",
                *(
                    f"FES_CBL_TX_PHY id={IDENTITY} sample={i} us={n * 1000000 + i * 110000}" + phy
                    for i in (0, 1)
                ),
                f"FES_CBL_MAC_TX END id={IDENTITY}" + summary,
                f"FES_CBL_BIDI RX id={IDENTITY} round={n} possible=1 armed=1 matched=1 stopped=1 retained=1 failure=0 desc=3000003c channel=00000180 mac=00000000 mtl=00000000 dma=00000000 ch=00000000 dwell_us=10000 reply={reply.hex()}",
                f"FES_CBL_BIDI ROUND id={IDENTITY} round={n} vmid={n} cmd={12 if n == 1 else 8} success=1",
            ]
        )
    receipt = dict(
        scope=BIDI.SCOPE,
        status="prepared",
        parking_revision=BIDI.PARKING,
        diagnostic_id=IDENTITY,
        frames=frames,
        frame_sha256=frames[0]["request_sha256"],
        startup_predicate_report=True,
        capability_snapshot=True,
        phy_samples=True,
    )
    return head, blocks, suffix, receipt


def wire(lines):
    return ("\r\n".join(lines) + "\r\n").encode()


class BidirectionalWitness(unittest.TestCase):
    def test_two_independent_grants(self):
        head, (first, second), suffix, r = trial()
        raw = wire(head + first + second + suffix)
        self.assertEqual(BIDI.decode_positive(raw, r)["successful_rounds"], 2)
        mutations = [
            (b"previous=1 cmd=2", b"previous=0 cmd=2"),
            (b"cmd=12 success=1", b"cmd=6 success=1"),
            (b"dwell_us=10000", b"dwell_us=9999"),
            (b"retained=1", b"retained=0"),
            (b"channel=00000180", b"channel=00001180"),
            (b"mtl=00000000 dma=", b"mtl=00010000 dma="),
            (b"ch=00000000", b"ch=10000000"),
            (b"armed=1 matched=1", b"armed=1 matched=0"),
            (b"desc=3000003c channel", b"desc=1000003c channel"),
            (b"desc=3000003c channel", b"desc=3000803c channel"),
            (b"desc=3000003c channel", b"desc=30000040 channel"),
            (b"possible=1 armed=1", b"possible=0 armed=1"),
            (b"drain_status=00000002", b"drain_status=00000000"),
            (b"feature0=cafd5b3f", b"feature0=ca7d5b3f"),
            (b"FES_CBL_BIDI PARK", b"FES_CBL_MAC_TX PARK"),
            (b"pcs=0004", b"pcs=0002"),
            (b"cmd=8 success=1", b"cmd=12 success=1"),
            (b"us=1110000", b"us=1000000"),
            (b"us=2000000", b"us=1119999"),
            (b"reply=02fe", b"reply=04fe"),
            (b"nonce=0101", b"nonce=0202"),
            (b"DDR: init success", b"Trying to boot from MMC"),
            (b"round=2 vmid=2", b"round=1 vmid=1"),
        ]
        for a, b in mutations:
            with self.subTest(a=a, b=b), self.assertRaises(ValueError):
                BIDI.decode_closed(raw.replace(a, b, 1), r)
        for changes in (
            head + first + suffix,
            head + second + first + suffix,
            head + first + first + second + suffix,
        ):
            with self.assertRaises(ValueError):
                BIDI.decode_positive(wire(changes), r)
        with self.assertRaises(ValueError):
            BIDI.decode_closed(raw, {**r, "frames": [r["frames"][0]] * 2})

    def test_failed_second_grant_preserves_first(self):
        head, (first, second), suffix, r = trial()
        second = second.copy()
        second[-3] = (
            second[-3]
            .replace("rc=0 stage=8", "rc=-1 stage=5")
            .replace("disposition=closed", "disposition=failed-drained")
            .replace("fault_reg=00000000", "fault_reg=9480080c")
        )
        second[-2] = (
            second[-2].replace("matched=1", "matched=0").replace("failure=0", "failure=3").split(" reply=")[0]
            + " reply=-"
        )
        second[-1] = second[-1].replace("success=1", "success=0")
        raw = wire(head + first + second + suffix)
        result = BIDI.decode_closed(raw, r)
        self.assertEqual(result["successful_rounds"], 1)
        self.assertFalse(result["target_succeeded"])
        with self.assertRaises(ValueError):
            BIDI.decode_positive(raw, r)
        with self.assertRaises(ValueError):
            BIDI.decode_closed(raw.replace(b"failure=3 desc=3000003c", b"failure=3 desc=b000003c"), r)
        with self.assertRaises(ValueError):
            BIDI.decode_closed(raw.replace(b"failure=3", b"failure=0"), r)

    def test_proven_stop_before_first_or_second_receive(self):
        head, (first, second), suffix, receipt = trial()
        terminal = (
            f"FES_CBL_MAC_TX END id={IDENTITY} rc=-1 stage=4 arm=1 mapped=1 mac=1 possible=0 dispatch=0 tps=0 revoked=1 reset=1 "
            "disposition=revoked-undispatched writes=1 terminal_per0=ff7fbfff terminal_route=00000003 terminal_sid=00010001 "
            "terminal_cr0=0000000d terminal_ack=0000000d terminal_ste=00000001 reg=10d11024 value=ff7fbfff desc=00000000 "
            "dma=00000000 fault_reg=10810204 fault_value=00070300 phy=0300 drain_dma=00000000 drain_mtl=00000000 drain_status=00000000"
        )
        for n, block in enumerate((first, second), 1):
            empty = (
                f"FES_CBL_BIDI RX id={IDENTITY} round={n} possible=0 armed=0 matched=0 stopped=0 retained=0 failure=0 "
                "desc=00000000 channel=00000000 mac=00000000 mtl=00000000 dma=00000000 ch=00000000 dwell_us=0 reply=-"
            )
            interrupted = block[:3] + [
                s.replace("resolved=0f49", "resolved=0300").replace("pcs=0004", "pcs=0000")
                for s in block[3:5]
            ]
            interrupted += [terminal, empty, block[-1].replace("success=1", "success=0")]
            raw = wire(head + ([] if n == 1 else first) + interrupted + suffix)
            outcome = BIDI.decode_closed(raw, receipt)
            self.assertEqual(outcome["successful_rounds"], n - 1)
            self.assertFalse(outcome["target_succeeded"])
            with self.assertRaises(ValueError):
                BIDI.decode_positive(raw, receipt)
            with self.assertRaises(ValueError):
                BIDI.decode_closed(raw.replace(b"possible=0 armed=0", b"possible=0 armed=1"), receipt)

    def test_initial_unchanged_refusal(self):
        head, _, suffix, receipt = trial()
        head = head[:-2]
        head.append(
            f"FES_CBL_TX_GATE id={IDENTITY} predicate=sdm-config-status observed=0000000010000000 required=0000000000000000"
        )
        head.append(
            f"FES_CBL_MAC_TX END id={IDENTITY} rc=-1 stage=0 arm=0 mapped=0 mac=0 possible=0 dispatch=0 tps=0 revoked=0 reset=0 "
            "disposition=unchanged writes=0 terminal_per0=ff7fbfff terminal_route=00000030 terminal_sid=00000000 terminal_cr0=00000000 "
            "terminal_ack=00000000 terminal_ste=00000000 reg=10d11024 value=ff7fbfff desc=00000000 dma=00000000 fault_reg=00000000 "
            "fault_value=00000000 phy=0000 drain_dma=00000000 drain_mtl=00000000 drain_status=00000000"
        )
        raw = wire(head + suffix)
        self.assertEqual(BIDI.decode_closed(raw, receipt)["rounds"], [])
        with self.assertRaises(ValueError):
            BIDI.decode_closed(raw.replace(b"writes=0", b"writes=1"), receipt)
