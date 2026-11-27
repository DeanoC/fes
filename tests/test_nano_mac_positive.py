import hashlib
import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
NANO = ROOT / "platform/embedded/nano"
spec = importlib.util.spec_from_file_location("mac_positive_replay", NANO / "mac_spl/verify_positive.py")
replay = importlib.util.module_from_spec(spec)
spec.loader.exec_module(replay)


class PositiveTX(unittest.TestCase):
    def test_standalone_tables_tx_and_failure_disposition(self):
        with tempfile.TemporaryDirectory() as tmp:
            exe = Path(tmp) / "mac-positive"
            subprocess.run(["cc", "-std=c11", "-Wall", "-Wextra", "-Werror", "-O2", "-I", str(NANO),
                            str(ROOT / "tests/test_nano_mac_positive.c"),
                            str(NANO / "mac_tx_diagnostic.c"), "-o", str(exe)], check=True)
            subprocess.run([str(exe)], check=True, capture_output=True, timeout=15)

    def test_closed_receipt_and_incomplete_evidence(self):
        identity = "1a2b3c4d5e6f7890"
        receipt = dict(scope="de25-cbl-positive-mac-spl-v1", status="prepared", startup_predicate_report=True, parking_revision=replay.PARKING, diagnostic_id=identity,
                       frame_sha256=hashlib.sha256(b"fixture only").hexdigest())
        def trace(n, value=0):
            return f"FES_NANO_CPU_TRACE id={identity} stage={n:02x} value={value:08x} mpidr=0000000081000000 el=3"
        end = (f"FES_CBL_MAC_TX END id={identity} rc=0 stage=8 arm=1 mapped=1 mac=1 possible=1 dispatch=1 "
               "tps=1 revoked=1 reset=1 disposition=closed writes=1 terminal_per0=ff7fbfff terminal_route=00000003 "
               "terminal_sid=00010001 terminal_cr0=0000000d terminal_ack=0000000d terminal_ste=00000001 "
               "reg=10d11024 value=ff7fbfff desc=3000003c dma=00000001 fault_reg=00000000 fault_value=00000000 phy=004c")
        power_words=(0x90000021,0,0,0,0x23,0x23,0x70021e)
        lines = [f"U-Boot SPL 2025.01-fes-nano-cpu-{identity} (Nov 1)",
                 trace(1), *(trace(0x20+i,v) for i,v in enumerate(power_words)), *map(trace,range(2,18)),
                 "FES_NANO_SPL BEGIN cause=00000004 per0=ff7fbfff bridge=0000004f cpus=0000000f release=00000000 kind=4 valid=1",
                 "FES_NANO_SPL SCOPE mpidr=81000000 core=0 cause_now=00000004 cpus_now=0000000f release_now=00000000 reset_timeouts=00000000",
                 "DDR: init success", f"FES_CBL_MAC_TX BEGIN id={identity}",
                 *(f"FES_CBL_MAC_TX POWER phase={phase} cpu0=90000021 cpu1=00000000 cpu2=00000000 cpu3=00000000 req=00000023 ack=00000023 boot=0070021e" for phase in ("first", "live")),
                 f"FES_CBL_TX_SETUP id={identity} daif_before=2c0 daif_after=3c0 serror=000 l4sp_hz=100000000 emaca_hz=250000000",
                 f"FES_CBL_TX_GATE id={identity} predicate=accepted observed=0000000000000001 required=0000000000000001",
                 end, *(trace(0x30+i,v) for i,v in enumerate(power_words)), trace(18), replay.PARK]
        raw = ("\r\n".join(lines)+"\r\n").encode()
        self.assertTrue(replay.decode_positive(raw,receipt)["target_closed"])
        for original,altered in ((b"tps=1",b"tps=0"),(b"ack=00000023",b"ack=00000021"),(b"cpu1=00000000",b"cpu1=00000001"),(b"disposition=closed",b"disposition=cold-recovery"),
                                 (b"arm=1",b"arm=0"),(b"3000003c",b"b000003c"),
                                 (b"3000003c",b"3800003c"),(b"3000003c",b"7000003c"),
                                 (b"daif_before=2c0",b"daif_before=240"),(b"daif_after=3c0",b"daif_after=2c0"),
                                 (b"serror=000",b"serror=100"),(b"250000000",b"125000000"),
                                 (b"dma=00000001",b"dma=00002001"), (b"phy=004c",b"phy=002c"),
                                 (b"phy=004c",b"phy=004d"), (b"phy=004c",b"phy=0044"),
                                 (b"FES_CBL_MAC_TX BEGIN",b"Trying to boot from MMC\r\nFES_CBL_MAC_TX BEGIN"),
                                 (b"per0=ff7fbfff",b"per0=ff7fbffe"),
                                 (replay.PARK.encode(),b"no final park")):
            with self.subTest(altered=altered), self.assertRaises(ValueError):
                replay.decode_positive(raw.replace(original,altered),receipt)
        with self.assertRaises(ValueError):
            replay.decode_positive(raw+raw,receipt)
        unchanged = (f"FES_CBL_MAC_TX END id={identity} rc=-1 stage=1 arm=0 mapped=0 mac=0 possible=0 dispatch=0 "
                     "tps=0 revoked=0 reset=0 disposition=unchanged writes=0 terminal_per0=ff7fbfff terminal_route=00000030 "
                     "terminal_sid=00000000 terminal_cr0=00000000 terminal_ack=00000000 terminal_ste=00000000 "
                     "reg=10d11024 value=ff7fbfff desc=00000000 dma=00000000 fault_reg=16000020 fault_value=00000008 phy=0000")
        revoked = (f"FES_CBL_MAC_TX END id={identity} rc=-1 stage=4 arm=1 mapped=1 mac=1 possible=0 dispatch=0 "
                   "tps=0 revoked=1 reset=1 disposition=revoked-undispatched writes=1 terminal_per0=ff7fbfff "
                   "terminal_route=00000003 terminal_sid=00010001 terminal_cr0=0000000d terminal_ack=0000000d "
                   "terminal_ste=00000001 reg=10d11024 value=ff7fbfff desc=00000000 dma=00000000 "
                   "fault_reg=10810204 fault_value=00001622 phy=0000")
        for terminal in (unchanged, unchanged.replace("stage=1","stage=0"), revoked,
                         revoked.replace("stage=4","stage=3").replace("mac=1","mac=0"), revoked.replace("stage=4","stage=5")):
            wire = raw.replace(end.encode(), terminal.encode())
            result = replay.decode_closed(wire, receipt)
            self.assertTrue(result["target_closed"])
            self.assertFalse(result["device_frame_dispatched"])
            self.assertFalse(result["process_stopped"])
            with self.assertRaises(ValueError):
                replay.decode_positive(wire, receipt)
            for old,new in (("possible=0","possible=1"), ("dispatch=0","dispatch=1"), ("stage=","stage=7"),
                            ("value=ff7fbfff","value=ff7fbffe"), ("dma=00000000","dma=00000001"),
                            ("terminal_ack=0000000","terminal_ack=fffffff"), ("fault_reg=","lost_reg=")):
                bad = wire.replace(old.encode(),new.encode())
                self.assertNotEqual(wire,bad)
                with self.subTest(old=old), self.assertRaises(ValueError):
                    replay.decode_closed(bad,receipt)

        rejected = b"predicate=sdm-config-status observed=0000000010000000 required=0000000000000000"
        accepted = b"predicate=accepted observed=0000000000000001 required=0000000000000001"
        startup = raw.replace(accepted, rejected).replace(end.encode(), unchanged.replace("stage=1", "stage=0").encode())
        setup = next(line.encode()+b"\r\n" for line in lines if line.startswith("FES_CBL_TX_SETUP"))
        with self.assertRaises(ValueError):
            replay.decode_closed(startup, receipt)
        startup = startup.replace(setup, b"")
        result = replay.decode_closed(startup, receipt)
        self.assertEqual(result["startup"]["predicate"], "sdm-config-status")
        self.assertFalse(result["device_frame_dispatched"])
        with self.assertRaises(ValueError):
            replay.decode_closed(startup.replace(b"stage=0 arm=0", b"stage=1 arm=0"), receipt)
