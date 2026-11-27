"""Replay the complete bounded CBL MAC SPL UART capture, including the final park."""
import hashlib
import re

NAMES = ("clock_stat", "main_en", "main_bypass", "noc_clk", "noc_div", "per_en",
         "per_bypass", "emac_sel", "counter_rst", "per0_reset", "tsn0", "tsn0_sidctrl",
         "tsn0_sids", "l4_fw_emac0", "noc_idle")
PARK = "FES_NANO_CPU_DIAG PARK no-ddr no-storage no-bl31"


def skip(words):
    if words["per0_reset"] & 0x101:
        return "tsn0-reset"
    if words["clock_stat"] & 0x01030301 != 0x00010100:
        return "clock-state"
    if words["main_en"] & 14 != 14 or not words["per_en"] & 1:
        return "clock-gate"
    line = 1 << ((words["emac_sel"] >> 26) & 1)
    if ((words["noc_clk"] >> 16) & 7) > 1 or words["main_bypass"] & 2 or (words["per_bypass"] | words["counter_rst"]) & line:
        return "clock-source"
    return None


def verify(raw: bytes, diagnostic_id: str) -> dict:
    if not isinstance(raw, bytes) or len(raw) > 4 << 20 or not re.fullmatch("[a-f0-9]{16}", diagnostic_id):
        raise ValueError("invalid UART evidence or diagnostic ID")
    # Reject truncation and duplicated evidence. Preserve irrelevant banner and
    # normal CPU-report bytes while keeping every observer line in exact order.
    lines = raw.replace(b"\r\n", b"\n").decode("ascii", errors="replace").split("\n")
    banners = [i for i, line in enumerate(lines) if line.startswith("U-Boot SPL ")]
    if (len(banners) != 1 or not lines[banners[0]].startswith("U-Boot SPL 2025.01-fes-nano-cpu-" + diagnostic_id + " ")
            or any(s.startswith("FES_NANO_CPU_TRACE") for s in lines[:banners[0]])
            or not raw.replace(b"\r\n", b"\n").rstrip(b"\n").endswith(PARK.encode())):
        raise ValueError("missing fresh matched SPL banner or terminal park")
    relevant = [s for s in lines if s.startswith(("FES_NANO_MAC_SPL", "FES_NANO_CPU_TRACE", "FES_NANO_CPU_DIAG"))]
    tls = re.compile(r"FES_NANO_CPU_TRACE id=" + diagnostic_id +
                     r" stage=([a-f0-9]{2}) value=00000000 mpidr=([a-f0-9]{16}) el=3")
    scope = None
    for idx in range(17):
        record = tls.fullmatch(relevant[idx]) if idx < len(relevant) else None
        if not record or int(record[1], 16) != idx+1 or (scope is not None and record[2] != scope):
            raise ValueError("initial CBL OCRAM trace incomplete, foreign or out of order")
        scope = record[2]
    if scope != "0000000081000000":
        raise ValueError("unrecognized CBL CPU scope")
    if len(relevant) < 18 or relevant[17] != "FES_NANO_MAC_SPL BEGIN id=" + diagnostic_id + " el=3":
        raise ValueError("missing fresh EL3 MAC observation")
    index = 18
    readings = []
    for observation in range(2):
        words = {}
        for name in NAMES:
            prefix = f"FES_NANO_MAC_SPL WORD pass={observation} name={name} value="
            record = re.fullmatch(re.escape(prefix) + "([0-9a-f]{8})", relevant[index]) if index < len(relevant) else None
            if not record:
                raise ValueError("missing or reordered read-only register observation")
            words[name] = int(record[1], 16)
            index += 1
        readings.append(words)
    reason = "unstable" if readings[0] != readings[1] else skip(readings[1])
    version = None
    if reason:
        expected = ["FES_NANO_MAC_SPL VERSION skipped=" + reason]
    else:
        record = re.fullmatch(r"FES_NANO_MAC_SPL VERSION value=([0-9a-f]{8})", relevant[index+1]) if index+1 < len(relevant) else None
        if not record or int(record[1], 16) & 0xffff != 0x7631:
            raise ValueError("missing or mismatched CBL MAC version")
        version = int(record[1], 16)
        expected = ["FES_NANO_MAC_SPL VERSION read", record[0]]
    final = f"FES_NANO_CPU_TRACE id={diagnostic_id} stage=12 value=00000000 mpidr={scope} el=3"
    if relevant[index:] != [*expected, "FES_NANO_MAC_SPL END state=observed", final, PARK] or (PARK+"\n").encode() not in raw.replace(b"\r\n", b"\n"):
        raise ValueError("MAC observation or terminal park incomplete")
    if reason == "unstable":
        raise ValueError("clock/reset/security values changed during the observation")
    return dict(scope="de25-cbl-mac-ocramspl-v1", diagnostic_id=diagnostic_id, cpu_mpidr=scope,
                uart_sha256=hashlib.sha256(raw).hexdigest(), register_samples=readings,
                mac_version=version, mac_skip_reason=reason, parked=True, dma_started=False)
