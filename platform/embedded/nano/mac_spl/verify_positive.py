"""Replay the target half of a CBL positive TX trial; Ethernet capture is separate."""
import hashlib
import re

PARKING = "87b2d7f4721c819b554212f2b80c193a07a2531e"
PARK = "FES_CBL_MAC_TX PARK no-storage no-bl31"


def decode_closed(raw: bytes, receipt: dict) -> dict:
    identity = receipt.get("diagnostic_id", "")
    frame_hash = receipt.get("frame_sha256", "")
    if (not isinstance(raw, bytes) or len(raw) > 2 << 20 or
            receipt.get("scope") != "de25-cbl-positive-mac-spl-v1" or
            receipt.get("status") != "prepared" or receipt.get("startup_predicate_report") is not True or
            receipt.get("phy_samples") is not True or receipt.get("capability_snapshot") is not True or receipt.get("parking_revision") != PARKING or not re.fullmatch("[0-9a-f]{16}", identity) or
            not re.fullmatch("[0-9a-f]{64}", frame_hash)):
        raise ValueError("invalid positive diagnostic source receipt or UART stream")
    data = raw.replace(b"\r\n", b"\n")
    if not data.endswith((PARK+"\n").encode()):
        raise ValueError("missing final terminal park")
    lines = data.decode("ascii", errors="replace").splitlines()
    banners = [i for i, line in enumerate(lines) if line.startswith("U-Boot SPL ")]
    if len(banners) != 1 or not lines[banners[0]].startswith("U-Boot SPL 2025.01-fes-nano-cpu-"+identity+" "):
        raise ValueError("missing matched SPL startup")
    prefixes = ("FES_NANO_CPU_TRACE", "FES_NANO_SPL", "FES_CBL_MAC_TX", "FES_CBL_TX_GATE", "FES_CBL_TX_SETUP", "FES_CBL_TX_CAPS", "FES_CBL_TX_PHY")
    trace = [s for s in lines if s.startswith(prefixes)]
    if any(s.startswith(prefixes) for s in lines[:banners[0]]):
        raise ValueError("positive diagnostics before matched banner")
    def power_trace(stage):
        rows=[]
        for index in range(7):
            line = re.compile(r"FES_NANO_CPU_TRACE id=" + identity + f" stage={stage+index:02x}" +
                              r" value=([a-f0-9]{8}) mpidr=0000000081000000 el=3")
            matches=[(s, int(m[1],16)) for s in trace if (m:=line.fullmatch(s))]
            if len(matches)!=1:
                raise ValueError("missing or duplicate qualified power checkpoint")
            rows.append(matches[0])
        values=[v for _,v in rows]
        if any(values[1:4]) or values[4]&0x1e!=2 or values[5]&0x1e!=2 or values[6]&0xc00:
            raise ValueError("spare CPU released or unqualified selected core")
        return [s for s,_ in rows], values
    early, early_power = power_trace(0x20)
    late, late_power = power_trace(0x30)
    prefix = [f"FES_NANO_CPU_TRACE id={identity} stage={i:02x} value=00000000 mpidr=0000000081000000 el=3" for i in range(1,18)]
    expected = [prefix[0],*early,*prefix[1:]] + [
        "FES_NANO_SPL BEGIN cause=00000004 per0=ff7fbfff bridge=0000004f cpus=0000000f release=00000000 kind=4 valid=1",
        "FES_NANO_SPL SCOPE mpidr=81000000 core=0 cause_now=00000004 cpus_now=0000000f release_now=00000000 reset_timeouts=00000000",
        "FES_CBL_MAC_TX BEGIN id="+identity,
    ]
    if trace[:len(expected)] != expected or trace[-9:] != [*late,
            f"FES_NANO_CPU_TRACE id={identity} stage=12 value=00000000 mpidr=0000000081000000 el=3", PARK]:
        raise ValueError("missing ordered matching cold CPU, ownership or park")
    phy_samples = []
    fields = ("id1","id2","bmcr","bmsr_latch","bmsr","adv","partner","gig_adv","gig_partner","pcs","resolved")
    rows = [i for i,line in enumerate(trace) if line.startswith("FES_CBL_TX_PHY")]
    if rows:
        if (len(rows)>2 or rows != list(range(rows[0],rows[0]+len(rows))) or
                rows[-1]+1>=len(trace) or not trace[rows[-1]+1].startswith("FES_CBL_MAC_TX END ")):
            raise ValueError("out-of-order PHY observations")
        for i,row in enumerate(rows):
            m = re.fullmatch(r"FES_CBL_TX_PHY id="+identity+f" sample={i} us=([0-9]{{1,20}})"+
                             "".join(" "+f+"=([0-9a-f]{4})" for f in fields),trace[row])
            if not m:
                raise ValueError("malformed PHY observation")
            phy_samples.append(dict(zip(fields,(int(v,16) for v in m.groups()[1:])),time_us=int(m[1])))
        del trace[rows[0]:rows[-1]+1]
    def link_ready(p):
        return (p["id1"]==0x22 and p["id2"]==0x1622 and p["bmcr"] & 0xde00 == 0x1000 and
                p["bmsr"] & 0x34 == 0x24 and p["adv"] & 31 == 1 and p["partner"] & 0x201f == 1 and
                p["gig_adv"] & 0xe200 == 0x200 and p["gig_partner"] & 0xb800 == 0x3800 and
                p["gig_partner"] & 0xff != 0xff and p["pcs"] & 6 == 4 and p["resolved"] & 0x78 == 0x48)
    caps = None
    capability_rows = [i for i,line in enumerate(trace) if line.startswith("FES_CBL_TX_CAPS")]
    if capability_rows:
        index = capability_rows[0]
        if len(capability_rows) != 1 or index+1 >= len(trace) or not trace[index+1].startswith("FES_CBL_MAC_TX END "):
            raise ValueError("missing or out-of-order MAC capability snapshot")
        observed = re.fullmatch(r"FES_CBL_TX_CAPS id=" + identity +
            r" version=([a-f0-9]{8}) feature0=([a-f0-9]{8}) feature1=([a-f0-9]{8})"
            r" feature2=([a-f0-9]{8}) interface=([a-f0-9]{8})", trace.pop(index))
        if not observed:
            raise ValueError("invalid MAC capability snapshot")
        caps = dict(zip(("version","feature0","feature1","feature2","interface"),
                        (int(value,16) for value in observed.groups())))
    if len(trace) not in (len(expected)+13, len(expected)+14):
        raise ValueError("missing or duplicated diagnostic evidence")
    powers = []
    for n, phase in enumerate(("first", "live")):
        m = re.fullmatch(r"FES_CBL_MAC_TX POWER phase=" + phase +
                         r" cpu0=([a-f0-9]{8}) cpu1=00000000 cpu2=00000000 cpu3=00000000"
                         r" req=([a-f0-9]{8}) ack=([a-f0-9]{8}) boot=([a-f0-9]{8})",trace[len(expected)+n])
        if not m:
            raise ValueError("missing sole primary CPU power evidence")
        primary, req, ack, boot = (int(x, 16) for x in m.groups())
        if req & 0x1e != 2 or ack & 0x1e != 2 or boot & 0xc00:
            raise ValueError("current CPU power ownership differs")
        powers.append(dict(primary=primary,request=req,acknowledgement=ack,boot=boot))
    if len({early_power[6],powers[0]["boot"],powers[1]["boot"],late_power[6]}) != 1:
        raise ValueError("selected boot configuration changed")
    setup = None
    gate_index = len(expected) + 2
    if len(trace) == len(expected) + 14:
        setup_match = re.fullmatch(r"FES_CBL_TX_SETUP id=" + identity +
            r" daif_before=(2c0|3c0) daif_after=3c0 serror=000 l4sp_hz=100000000 emaca_hz=250000000",
            trace[gate_index])
        if not setup_match:
            raise ValueError("unqualified EL3 setup or controller reference clock")
        setup = dict(daif_before=int(setup_match[1],16), daif_after=0x3c0, serror=0,
                     l4sp_hz=100000000, emaca_hz=250000000)
        gate_index += 1
    gate = re.fullmatch(r"FES_CBL_TX_GATE id=" + identity +
                        r" predicate=([a-z][a-z0-9-]*) observed=([a-f0-9]{16}) required=([a-f0-9]{16})",
                        trace[gate_index])
    if not gate:
        raise ValueError("missing startup predicate")
    predicate, observed, required = gate[1], int(gate[2], 16), int(gate[3], 16)
    passed_startup = predicate == "accepted"
    if (passed_startup and (observed, required) != (1, 1)) or (not passed_startup and observed == required):
        raise ValueError("contradictory startup predicate")
    if (setup is not None) != (passed_startup or predicate == "stable-dma-owner"):
        raise ValueError("startup evidence is inconsistent with the recorded predicate")
    terminal = trace[gate_index+1]
    fields = re.fullmatch(
        r"FES_CBL_MAC_TX END id="+identity+
        r" rc=(0|-1) stage=([0-8]) arm=([01]) mapped=([01]) mac=([01]) possible=([01])"
        r" dispatch=([01]) tps=([01]) revoked=([01]) reset=([01]) disposition=([a-z-]+)"
        r" writes=([01]) terminal_per0=([0-9a-f]{8}) terminal_route=([0-9a-f]{8})"
        r" terminal_sid=([0-9a-f]{8}) terminal_cr0=([0-9a-f]{8}) terminal_ack=([0-9a-f]{8})"
        r" terminal_ste=([0-9a-f]{8}) reg=10d11024 value=([0-9a-f]{8}) desc=([0-9a-f]{8})"
        r" dma=([0-9a-f]{8}) fault_reg=([0-9a-f]{8}) fault_value=([0-9a-f]{8}) phy=([0-9a-f]{4}) drain_dma=([0-9a-f]{8}) drain_mtl=([0-9a-f]{8}) drain_status=([0-9a-f]{8})", terminal)
    if not fields:
        raise ValueError("missing terminal hardware closure")
    rc, stage = int(fields[1]), int(fields[2])
    arm, mapped, mac, possible, dispatch, tps, revoked, reset = map(int, fields.groups()[2:10])
    disposition, writes = fields[11], int(fields[12])
    per0, route, sid, cr0, ack, ste, reset_readback, desc, dma, fault_reg, fault_value, phy, drain_dma, drain_mtl, drain_status = (
        int(x, 16) for x in fields.groups()[12:])
    if per0 != 0xff7fbfff or reset_readback != per0:
        raise ValueError("terminal reset scope differs from the initial CBL state")
    flags = (arm, mapped, mac, possible, dispatch, tps, revoked, reset, writes)
    state = (route, sid, cr0, ack, ste)
    abort = (3, 0x10001, 13, 13, 1)
    if disposition == "closed":
        valid = (rc == 0 and stage == 8 and flags == (1,)*9 and state == abort
                 and desc & 0xf8000000 == 0x30000000 and not dma & 0x3200
                 and fault_reg == fault_value == 0 and phy & 0x78 == 0x48)
    elif disposition == "failed-drained":
        valid = (rc==-1 and stage==5 and flags==(1,)*9 and state==abort and fault_reg!=0 and not dma & 0x3200)
    elif disposition == "unchanged":
        valid = (rc == -1 and stage in (0,1) and flags == (0,)*9
                 and state == (0x30, 0, 0, 0, 0) and desc == dma == phy == 0)
    elif disposition == "revoked-undispatched":
        valid = (rc == -1 and stage in (3,4,5) and flags == (1,1,mac,0,0,0,1,1,1)
                 and state == abort and desc == dma == 0)
    else:
        valid = False
    if tps and (drain_dma & 0xff03 or drain_mtl & 0x3e or drain_status & 0x3202 != 2):
        raise ValueError("TX queue, channel or AXI activity after stop")
    if phy_samples and (stage < 4 or phy_samples[-1]["resolved"] != phy):
        raise ValueError("PHY observation without MAC admission or contradictory resolved status")
    if stage >= 5 and (len(phy_samples)!=2 or not all(link_ready(p) for p in phy_samples) or
            phy_samples[1]["bmsr_latch"] & 0x34 != 0x24 or
            not 100000 <= phy_samples[1]["time_us"] - phy_samples[0]["time_us"] <= 12000000):
        raise ValueError("two independent linked PMA/PCS snapshots required before dispatch")
    if caps is not None and not (mac and stage >= 3):
        raise ValueError("MAC capability read without a released controller")
    if stage >= 4:
        if caps is None or (caps["version"] != 0x107631 or caps["interface"] & 3 != 1 or
                caps["feature0"] & 0x81800023 != 0x80800023 or
                caps["feature1"] & 0xc7df != 0x4207 or
                caps["feature2"] & 0x3cf3cf != 0x1c71c7):
            raise ValueError("controller did not satisfy the RGMII, MDIO, FIFO or DMA geometry")
    if stage > 0 and not passed_startup:
        raise ValueError("hardware admission followed a rejected startup predicate")
    if not valid:
        raise ValueError("terminal result conflicts with observed ownership or controller state")
    if any("Trying to boot from" in s or "FES_NANO_SPL HANDOFF" in s for s in lines):
        raise ValueError("unexpected storage or firmware handoff")
    return dict(scope=receipt["scope"], diagnostic_id=identity, frame_sha256=frame_hash,
                uart_sha256=hashlib.sha256(raw).hexdigest(), target_closed=True,
                parking_revision=PARKING, cpu_power=powers, device_frame_dispatched=bool(dispatch),
                process_stopped=bool(tps), translation_revoked=bool(revoked), disposition=disposition,
                mac_in_reset=True, parked=True, terminal_reset=per0, tx_descriptor=desc, dma_status=dma,
                fault_reg=fault_reg, fault_value=fault_value, phy_status=phy,
                startup=dict(predicate=predicate, observed=observed, required=required), setup=setup, capabilities=caps, phy_samples=phy_samples,
                vendor_link_check_fail=[bool(p["resolved"] & 1) for p in phy_samples],
                target_succeeded=disposition=="closed", drain_dma=drain_dma, drain_mtl=drain_mtl, drain_status=drain_status)


def decode_positive(raw: bytes, receipt: dict) -> dict:
    result = decode_closed(raw, receipt)
    if not result["target_succeeded"]:
        raise ValueError("diagnostic did not complete successful TX")
    return result
