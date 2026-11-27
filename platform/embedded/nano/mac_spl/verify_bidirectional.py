"""Replay the CBL target's two translated Ethernet grants from a single physical UART capture."""

import hashlib
import re

PARKING = "87b2d7f4721c819b554212f2b80c193a07a2531e"
SCOPE = "de25-cbl-bidirectional-mac-spl-v1"
PARK = "FES_CBL_BIDI PARK no-storage no-bl31"


def need(ok, reason):
    if not ok:
        raise ValueError(reason)


class Trace:
    def __init__(self, lines):
        self.lines = lines
        self.index = 0

    def starts(self, prefix):
        return self.index < len(self.lines) and self.lines[self.index].startswith(prefix)

    def exact(self, expected):
        need(self.index < len(self.lines) and self.lines[self.index] == expected, "missing " + expected)
        self.index += 1

    def row(self, label, spec):
        # Fields and widths are part of the SPL wire format. Reject a second
        # occurrence, a missing field or a well-formed row at the wrong stage.
        need(self.starts(label + " "), "missing " + label)
        tokens = self.lines[self.index][len(label) + 1 :].split(" ")
        self.index += 1
        fields = spec.split()
        need(len(tokens) == len(fields), "unexpected field count in " + label)
        out = {}
        for token, field in zip(tokens, fields):
            key, kind = field.split(":")
            k, sep, value = token.partition("=")
            need(sep and k == key, "unexpected field in " + label)
            if kind == "b":
                need(value in ("0", "1"), "invalid bit")
                out[key] = int(value)
            elif kind in ("d", "rc"):
                pattern = r"(?:0|[1-9][0-9]{0,18})" if kind == "d" else r"(?:0|-1)"
                need(re.fullmatch(pattern, value), "invalid decimal")
                out[key] = int(value)
            elif kind.startswith("x"):
                need(re.fullmatch("[0-9a-f]{" + kind[1:] + "}", value), "invalid register width")
                out[key] = int(value, 16)
            else:
                out[key] = value
        return out


def power(trace, identity, stage):
    values = []
    for n in range(stage, stage + 7):
        row = trace.row("FES_NANO_CPU_TRACE", "id:s stage:x2 value:x8 mpidr:x16 el:d")
        need(
            (row["id"], row["stage"], row["mpidr"], row["el"]) == (identity, n, 0x81000000, 3),
            "wrong CPU checkpoint",
        )
        values.append(row["value"])
    need(
        not any(values[1:4]) and values[4] & 30 == values[5] & 30 == 2 and not values[6] & 0xC00,
        "spare CPU released",
    )
    return values[6]


def tx_observations(trace, identity):
    caps = None
    if trace.starts("FES_CBL_TX_CAPS"):
        caps = trace.row(
            "FES_CBL_TX_CAPS", "id:s version:x8 feature0:x8 feature1:x8 feature2:x8 interface:x8"
        )
        need(caps["id"] == identity, "wrong MAC capture")
    phy = []
    while trace.starts("FES_CBL_TX_PHY"):
        row = trace.row(
            "FES_CBL_TX_PHY",
            "id:s sample:d us:d id1:x4 id2:x4 bmcr:x4 bmsr_latch:x4 bmsr:x4 adv:x4 partner:x4 gig_adv:x4 gig_partner:x4 pcs:x4 resolved:x4",
        )
        need(row["id"] == identity and row["sample"] == len(phy) < 2, "unexpected PHY capture")
        phy.append(row)
    t = trace.row(
        "FES_CBL_MAC_TX END",
        "id:s rc:rc stage:d arm:b mapped:b mac:b possible:b dispatch:b tps:b revoked:b reset:b disposition:s writes:b terminal_per0:x8 terminal_route:x8 terminal_sid:x8 terminal_cr0:x8 terminal_ack:x8 terminal_ste:x8 reg:x8 value:x8 desc:x8 dma:x8 fault_reg:x8 fault_value:x8 phy:x4 drain_dma:x8 drain_mtl:x8 drain_status:x8",
    )
    need(
        t["id"] == identity and t["terminal_per0"] == t["value"] == 0xFF7FBFFF and t["reg"] == 0x10D11024,
        "missing fresh held reset",
    )
    flags = tuple(t[k] for k in "arm mapped mac possible dispatch tps revoked reset writes".split())
    state = tuple(t["terminal_" + k] for k in ("route", "sid", "cr0", "ack", "ste"))
    mode = t["disposition"]
    if mode == "unchanged":
        need(
            t["rc"] == -1
            and t["stage"] in (0, 1)
            and not any(flags)
            and state == (0x30, 0, 0, 0, 0)
            and t["desc"] == t["dma"] == t["phy"] == 0,
            "unproven unchanged state",
        )
    else:
        need(state == (3, 0x10001, 13, 13, 1), "the DMA domain is not in terminal abort")
        if mode == "revoked-undispatched":
            need(
                t["rc"] == -1
                and t["stage"] in (3, 4, 5)
                and flags == (1, 1, t["mac"], 0, 0, 0, 1, 1, 1)
                and t["desc"] == t["dma"] == 0,
                "unproven pre-dispatch revocation",
            )
        else:
            need(
                flags == (1,) * 9
                and not t["dma"] & 0x3200
                and not t["drain_dma"] & 0xFF03
                and not t["drain_mtl"] & 0x3E
                and t["drain_status"] & 0x3202 == 2,
                "unproven TX stop, drain or revocation",
            )
            if mode == "closed":
                need(
                    t["rc"] == 0
                    and t["stage"] == 8
                    and t["desc"] & 0xF8000000 == 0x30000000
                    and t["fault_reg"] == t["fault_value"] == 0,
                    "invalid TX success",
                )
            else:
                need(
                    mode == "failed-drained" and t["rc"] == -1 and t["stage"] == 5 and t["fault_reg"],
                    "invalid TX failure",
                )
    if caps or phy:
        need(t["mac"] and t["stage"] >= 3, "sampling before MAC admission")
    if t["stage"] >= 4:
        need(
            caps
            and caps["version"] == 0x107631
            and caps["interface"] & 3 == 1
            and caps["feature0"] & 0x81800023 == 0x80800023
            and caps["feature1"] & 0xC7DF == 0x4207
            and caps["feature2"] & 0x3CF3CF == 0x1C71C7,
            "invalid MAC capabilities",
        )
    if phy:
        need(t["stage"] >= 4 and t["phy"] == phy[-1]["resolved"], "contradictory PHY state")
    if t["stage"] >= 5:
        need(
            len(phy) == 2
            and 100000 <= phy[1]["us"] - phy[0]["us"] <= 18000000
            and phy[1]["bmsr_latch"] & 0x34 == 0x24,
            "missing steady link",
        )
        for p in phy:
            need(
                p["id1"] == 0x22
                and p["id2"] == 0x1622
                and p["bmcr"] & 0xDE00 == 0x1000
                and p["bmsr"] & 0x34 == 0x24
                and p["adv"] & 31 == 1
                and p["partner"] & 0x201F == 1
                and p["gig_adv"] & 0xE200 == 0x200
                and p["gig_partner"] & 0xB800 == 0x3800
                and p["gig_partner"] & 255 != 255
                and p["pcs"] & 4 != 0
                and p["resolved"] & 0x78 == 0x48,
                "PHY not qualified for gigabit TX",
            )
    t["phy_samples"] = phy
    t["capabilities"] = caps
    return t


def rx_observations(trace, identity, n, frame, tx):
    r = trace.row(
        "FES_CBL_BIDI RX",
        "id:s round:d possible:b armed:b matched:b stopped:b retained:b failure:d desc:x8 channel:x8 mac:x8 mtl:x8 dma:x8 ch:x8 dwell_us:d reply:s",
    )
    need(r["id"] == identity and r["round"] == n, "wrong receive round")
    if not r["possible"]:
        need(
            not any(
                r[k]
                for k in (
                    "armed",
                    "matched",
                    "stopped",
                    "retained",
                    "failure",
                    "desc",
                    "channel",
                    "mac",
                    "mtl",
                    "dma",
                    "ch",
                    "dwell_us",
                )
            )
            and r["reply"] == "-"
            and tx["disposition"] in ("unchanged", "revoked-undispatched"),
            "RX state contradicts no DMA",
        )
        return r
    need(
        tx["disposition"] in ("closed", "failed-drained")
        and r["armed"] == r["stopped"] == r["retained"] == 1
        and r["dwell_us"] >= 10000
        and r["channel"] & 0x3300 == 0x100
        and not r["mac"] & 7
        and not r["mtl"] & 0x3FFF0037
        and not r["dma"] & 0xF03
        and not r["ch"] & 0x7FFF0000,
        "RX channel, MAC, MTL or AXI retirement unproven",
    )
    descriptor_ok = r["desc"] & 0xF000BFFF == 0x3000003C
    if r["matched"]:
        need(
            not r["failure"] and descriptor_ok and re.fullmatch("[0-9a-f]{120}", r["reply"]),
            "invalid received descriptor",
        )
        reply = bytes.fromhex(r["reply"])
        need(
            hashlib.sha256(reply).hexdigest() == frame["reply_sha256"]
            and reply[28:44].hex() == frame["nonce_hex"]
            and reply[:6] == bytes.fromhex("02fe2508cb01")
            and reply[6:12] != reply[:6]
            and not reply[6] & 1
            and any(reply[6:12])
            and reply[12:28] == b"\x88\xb5FES-CBL-RX-v1\0"
            and reply[44:] == bytes(16),
            "unexpected reply bytes",
        )
        request = reply[6:12] + reply[:6] + reply[12:22] + b"T" + reply[23:]
        need(hashlib.sha256(request).hexdigest() == frame["request_sha256"], "request and reply do not pair")
    else:
        need(
            tx["disposition"] == "failed-drained" and r["reply"] == "-" and r["failure"] in (1, 2, 3),
            "unexpected recovered RX failure",
        )
        if r["failure"] == 2:
            need(not r["desc"] & (1 << 31) and not descriptor_ok, "invalid descriptor failure")
        if r["failure"] == 3:
            need(descriptor_ok, "incorrect failed payload classification")
    return r



def flp_rows(rows, identity, receipt):
    if "phy_flp_16ms" not in receipt:
        if any(line.startswith(("FES_CBL_TX_TRAIN", "FES_CBL_TX_FLP")) for line in rows):
            raise ValueError("undeclared PHY setup")
        return rows, []
    if receipt["phy_flp_16ms"] is not True:
        raise ValueError("invalid selected PHY setup")
    from pathlib import Path
    helper = Path(__file__).resolve().with_name("verify_flp.py")
    code = helper.read_bytes()
    if hashlib.sha256(code).hexdigest() != "83ba3d66aa4da828432c46668edb5b26702b0668e125e837e8d4aebea4646dae":
        raise ValueError("changed PHY evidence verifier")
    module = {"__name__": "fes_verify_flp"}
    exec(compile(code, str(helper), "exec"), module)
    return module["extract"](rows, identity)


def decode_closed(raw, receipt):
    identity = receipt.get("diagnostic_id", "")
    frames = receipt.get("frames")
    need(
        receipt.get("scope") == SCOPE
        and receipt.get("status") == "prepared"
        and receipt.get("parking_revision") == PARKING
        and isinstance(identity, str)
        and re.fullmatch("[0-9a-f]{16}", identity)
        and isinstance(frames, list)
        and len(frames) == 2,
        "invalid bidirectional receipt",
    )
    for frame in frames:
        need(
            isinstance(frame, dict)
            and set(frame) == {"request_sha256", "reply_sha256", "nonce_hex"}
            and all(
                isinstance(frame[k], str) and re.fullmatch("[0-9a-f]{64}", frame[k])
                for k in ("request_sha256", "reply_sha256")
            )
            and isinstance(frame["nonce_hex"], str)
            and re.fullmatch("[0-9a-f]{32}", frame["nonce_hex"])
            and int(frame["nonce_hex"], 16),
            "invalid selected trial frames",
        )
    need(
        all(
            receipt.get(k) is True for k in ("startup_predicate_report", "capability_snapshot", "phy_samples")
        )
        and receipt.get("frame_sha256") == frames[0]["request_sha256"],
        "unverified producer capability or mismatched first frame",
    )
    need(all(frames[0][k] != frames[1][k] for k in frames[0]), "reused grant witness")
    need(isinstance(raw, bytes) and 0 < len(raw) <= 2 << 20, "invalid UART capture")
    data = raw.replace(b"\r\n", b"\n")
    need(data.endswith((PARK + "\n").encode()), "missing final bidirectional Park")
    lines = data.decode("ascii", errors="replace").splitlines()
    banners = [i for i, l in enumerate(lines) if l.startswith("U-Boot SPL ")]
    need(
        len(banners) == 1
        and lines[banners[0]].startswith("U-Boot SPL 2025.01-fes-nano-cpu-" + identity + " "),
        "multiple boots or wrong SPL",
    )
    marked = lambda line: line.startswith(
        ("FES_CBL_", "FES_NANO_CPU_TRACE", "FES_NANO_SPL", "DDR: init success")
    )
    need(
        not any(marked(line) for line in lines[: banners[0]])
        and not any("Trying to boot from" in l or "FES_NANO_SPL HANDOFF" in l for l in lines),
        "capture crossed the standalone boot boundary",
    )
    records, flp = flp_rows([l for l in lines if marked(l)], identity, receipt)
    tr = Trace(records)

    def checkpoint(n):
        return f"FES_NANO_CPU_TRACE id={identity} stage={n:02x} value=00000000 mpidr=0000000081000000 el=3"

    tr.exact(checkpoint(1))
    boot = power(tr, identity, 0x20)
    for n in range(2, 18):
        tr.exact(checkpoint(n))
    tr.exact(
        "FES_NANO_SPL BEGIN cause=00000004 per0=ff7fbfff bridge=0000004f cpus=0000000f release=00000000 kind=4 valid=1"
    )
    tr.exact(
        "FES_NANO_SPL SCOPE mpidr=81000000 core=0 cause_now=00000004 cpus_now=0000000f release_now=00000000 reset_timeouts=00000000"
    )
    tr.exact("DDR: init success")
    tr.exact("FES_CBL_MAC_TX BEGIN id=" + identity)
    for phase in ("first", "live"):
        p = tr.row("FES_CBL_MAC_TX POWER", "phase:s cpu0:x8 cpu1:x8 cpu2:x8 cpu3:x8 req:x8 ack:x8 boot:x8")
        need(
            p["phase"] == phase
            and p["boot"] == boot
            and not (p["cpu1"] | p["cpu2"] | p["cpu3"])
            and p["req"] & 30 == p["ack"] & 30 == 2,
            "unqualified CPU ownership",
        )
    setup = tr.starts("FES_CBL_TX_SETUP")
    if setup:
        p = tr.row("FES_CBL_TX_SETUP", "id:s daif_before:x3 daif_after:x3 serror:x3 l4sp_hz:d emaca_hz:d")
        need(
            p["id"] == identity
            and p["daif_before"] in (0x2C0, 0x3C0)
            and p["daif_after"] == 0x3C0
            and p["serror"] == 0
            and (p["l4sp_hz"], p["emaca_hz"]) == (100000000, 250000000),
            "unqualified EL3 or clocks",
        )
    gate = tr.row("FES_CBL_TX_GATE", "id:s predicate:s observed:x16 required:x16")
    passed = gate["predicate"] == "accepted"
    need(
        gate["id"] == identity
        and re.fullmatch("[a-z][a-z0-9-]*", gate["predicate"])
        and setup == (passed or gate["predicate"] == "stable-dma-owner")
        and (
            (gate["observed"] == gate["required"] == 1) if passed else (gate["observed"] != gate["required"])
        ),
        "invalid starting gate",
    )
    outcomes = []
    terminal = None
    for n, frame in enumerate(frames, 1):
        if not tr.starts("FES_CBL_BIDI BEGIN"):
            if n == 1:
                terminal = tx_observations(tr, identity)
                if flp:
                    terminal["phy_flp"] = flp[0]
                need(
                    not passed and terminal["disposition"] == "unchanged" and terminal["stage"] == 0,
                    "unverified pre-grant failure",
                )
            break
        need(passed, "grant follows failed startup")
        tr.exact(f"FES_CBL_BIDI BEGIN id={identity} round={n} vmid={n} nonce={frame['nonce_hex']}")
        granted = tr.starts("FES_CBL_BIDI GRANT")
        grant_cmd = (6, 2)[n - 1]
        retired_cmd = (12, 8)[n - 1]
        if granted:
            tr.exact(
                f"FES_CBL_BIDI GRANT id={identity} round={n} vmid={n} previous={n - 1} cmd={grant_cmd} ste=0000000d"
            )
        terminal = tx_observations(tr, identity)
        if flp:
            terminal["phy_flp"] = flp[n - 1]
        need(granted == bool(terminal["mapped"]), "missing or contradictory observed active mapping")
        if n == 2:
            need(granted, "second round cannot reestablish the original unprogrammed domain")
            previous = outcomes[0]
            if terminal["phy_samples"]:
                need(
                    terminal["phy_samples"][0]["us"]
                    >= previous["tx"]["phy_samples"][-1]["us"] + previous["rx"]["dwell_us"],
                    "new grant predates the first observation and retirement dwell",
                )
        received = rx_observations(tr, identity, n, frame, terminal)
        completed = terminal["disposition"] == "closed" and bool(received["matched"])
        cmd = retired_cmd if granted else 0
        tr.exact(f"FES_CBL_BIDI ROUND id={identity} round={n} vmid={n} cmd={cmd} success={int(completed)}")
        outcomes.append(
            dict(
                round=n,
                vmid=n,
                grant_cmd=grant_cmd if granted else None,
                retired_cmd=cmd,
                success=completed,
                tx=terminal,
                rx=received,
            )
        )
        if not completed:
            break
        if n == 1:
            need(tr.starts("FES_CBL_BIDI BEGIN"), "missing new VMID after successful round 1")
    need(terminal is not None and power(tr, identity, 0x30) == boot, "boot owner changed across grants")
    tr.exact(checkpoint(18))
    tr.exact(PARK)
    need(tr.index == len(tr.lines), "extraneous UART evidence")
    return dict(
        **({"phy_flp": flp} if flp else {}),
        scope=SCOPE,
        diagnostic_id=identity,
        parking_revision=PARKING,
        uart_sha256=hashlib.sha256(raw).hexdigest(),
        target_closed=True,
        mac_in_reset=True,
        parked=True,
        rounds=outcomes,
        successful_rounds=sum(x["success"] for x in outcomes),
        target_succeeded=len(outcomes) == 2 and all(x["success"] for x in outcomes),
        disposition=terminal["disposition"],
        startup=gate,
    )


def decode_positive(raw, receipt):
    result = decode_closed(raw, receipt)
    need(result["target_succeeded"], "both independently retired grants are required")
    return result
