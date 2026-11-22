"""Shared captured C observation fixtures for Nano Tenfoot and installer trace tests."""
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "platform/embedded/nano"))
from service_trace import PREFIX, SCOPE

FIX = ROOT / "tests/fixtures/nano-program-service"
SPEC = json.loads((FIX / "expected.json").read_text())
BOOT = (FIX / "boot.txt").read_bytes()
LAUNCH = (FIX / "launch.txt").read_bytes()
STOP = (FIX / "stop.txt").read_bytes()
OWNER = re.search(rb"FES_NANO_SPL HANDOFF owner=([0-9a-f]+)", BOOT)[1].decode().zfill(16)
PROGRAMS = [{"identity": SPEC["identity"], "configuration": SPEC["configuration"]}] * 2


class Model:
    def __init__(self):
        self.phase = "ready"
        self.generation = 0
        self.held = True
        self.loads = 0
        self.fetches = 0
        self.commands = []
        self.fault = ""
        self.owned = True
        self.clock = 10.0
        self.ready = self.reply("status")

    def reply(self, op, ok=True):
        r = dict(
            scope=SCOPE,
            op=op,
            ok=ok,
            state=self.phase,
            owner=OWNER,
            generation=self.generation,
            held=self.held,
        )
        if self.phase == "running":
            r.update(SPEC["identity"])
        if op == "status":
            r["programs"] = [
                {"index": i, "package_id": p["identity"]["package_id"]}
                for i, p in enumerate(PROGRAMS)
            ]
        if op == "activity" and ok:
            r["activity"] = {"Cycles": self.fetches, "Address": 0x3D1}
        if not ok:
            r["error"] = "modeled stale or malformed request"
        return r

    def bytes(self, r, prefix=b""):
        return prefix + PREFIX + json.dumps(r, separators=(",", ":")).encode() + b"\n"

    def exchange(self, payload, seconds):
        command = json.loads(payload)
        self.commands.append(command)
        op = command["op"]
        evidence = b""
        fields = (
            {"op"}
            if op == "status"
            else {"op", "owner", "generation"}
            | (
                {"program"}
                if op == "launch"
                else {"rows"}
                if op == "keyboard"
                else set()
            )
        )
        bad = (
            set(command) != fields
            or op != "status"
            and (command.get("owner") != OWNER or command.get("generation") != self.generation)
        )
        bad = (
            bad
            or (op == "launch" and (self.phase != "ready" or not 0 <= command.get("program", -1) < 2))
            or op not in ("status", "launch", "return", "park", "hold", "run", "keyboard", "activity")
        )
        bad = bad or op not in ("status", "launch") and self.phase != "running"
        if not bad:
            if op == "launch":
                generation = 4 + 5 * self.loads
                previous = 0 if not self.loads else generation - 5
                text = LAUNCH.decode()
                text = re.sub(r"\bgeneration=4\b", f"generation={generation}", text)
                text = text.replace("previous=0 rearm=0", f"previous={previous} rearm={int(self.loads > 0)}")
                if self.loads:
                    text = re.sub(
                        r"\bcounter=([0-9a-f]+)",
                        lambda m: "counter=" + format(int(m[1], 16) + self.loads * 20 * 400000000, "x"),
                        text,
                    )
                evidence = text.encode()
                self.phase = "running"
                self.generation += 1
                self.held = False
                self.loads += 1
                self.fetches = 0
            elif op in ("return", "park"):
                evidence = re.sub(rb"\bgeneration=5\b", f"generation={5 + 5 * (self.loads - 1)}".encode(), STOP)
                self.phase = "ready" if op == "return" else "parked"
                self.generation += 1
                self.held = True
            elif op in ("hold", "run"):
                self.held = op == "hold"
            elif op == "activity" and (
                (not self.held and self.fault != "fetch-stopped")
                or self.held and self.fault == "fetch-held"
            ):
                self.fetches += 100
        r = self.reply(op, not bad)
        if self.fault == "owner":
            r["owner"] = "0000000000000001"
        if self.fault == "generation":
            r["generation"] += 1
        if self.fault == "boolean-generation":
            r["generation"] = True
        if self.fault == "held":
            r["held"] = not r["held"]
        if self.fault == "identity":
            r["build_id"] = "a" * 32
        if self.fault == "rejected":
            r.update(ok=False, error="unknown outcome")
        if self.fault == "activation":
            evidence = re.sub(rb"[^\n]*FES_NANO_FABRIC_ACTIVATION[^\n]*\n", b"", evidence)
        if self.fault == "transfer":
            evidence = evidence.replace(b"completed=1646592 pending=0", b"completed=1646591 pending=0")
        if self.fault == "smmu":
            evidence = evidence.replace(b"bypass_controls_clear=1", b"bypass_controls_clear=0")
        if self.fault == "stop":
            evidence = evidence.replace(b"complete=1 data=held", b"complete=0 data=held")
        if self.fault == "side-effect":
            evidence += b"NOTICE:  FES_NANO_CONFIG_QUERY command=4 words=6\n"
        if self.fault == "claim":
            self.owned = False
        if self.fault == "late":
            self.clock += seconds
        if self.fault == "clock":
            self.clock -= 1
        if self.fault == "raise":
            raise OSError("lost serial exchange")
        raw = self.bytes(r, evidence)
        if self.fault == "duplicate":
            raw += self.bytes(r)
        if self.fault == "duplicate-field":
            raw = raw.replace(b'"ok":true', b'"ok":true,"ok":true')
        if self.fault == "truncated":
            raw = raw[:-1]
        if self.fault == "panic":
            raw = b"panic: failure\n" + raw
        return raw
