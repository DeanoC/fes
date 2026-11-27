#!/usr/bin/env python3
"""Compose a separate pre-storage CBL TX-only SPL on the pinned trace producer."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys

HERE = Path(__file__).resolve().parent
TRACE = "87b2d7f4721c819b554212f2b80c193a07a2531e"
PARKING = TRACE
PARKING_FILES = ("nano_cold.c", "nano_cold.h", "nano_cold_mmio.inc")


def parking_inputs(checkout):
    return {name: subprocess.check_output([
        "git", "-C", checkout, "show", f"{PARKING}:platform/embedded/firmware/{name}"
    ]) for name in PARKING_FILES}



def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def replace_once(source, before, after):
    if source.count(before) != 1:
        raise ValueError(f"expected one positive diagnostic source anchor: {before[:60]!r}")
    return source.replace(before, after)


def customize(source, trace, base, frame, parking):
    soc = source / "arch/arm/mach-socfpga"
    board = soc / "spl_agilex5.c"
    original = trace.function(board.read_text(), "board_init_f")
    begin = original.index('\tret = uclass_get_device_by_name(UCLASS_NOP, "socfpga-secreg"')
    end = original.index("#if CONFIG_IS_ENABLED(PHY_CADENCE_COMBOPHY)", begin)
    # Exact pinned vendor DDR/firewall initialization, ending before MMC/USB/QSPI.
    ddr = original[begin:end]
    if ddr.count("uclass_get_device(UCLASS_RAM") != 1:
        raise ValueError("vendor DDR checkpoint is missing")
    hashes = base(source, False)
    # The seed itself must supply the physical v2 initial policy and MMIO map.
    for name, data in parking.items():
        if (soc / name).read_bytes() != data:
            raise ValueError("seed does not contain qualified CPU source " + name)
    cold = soc / "nano_cold.c"
    with cold.open("a") as output:
        output.write("""
int fes_positive_cpu_scope(struct fes_nano_cold *s, struct fes_nano_cold_sample *now)
{
    return s && s->attempted && !s->failed && s->record.core == 0 &&
           !sample(s, now) && stable(&s->record.first, now, 0);
}
""")
    for name in parking:
        hashes[str((soc/name).relative_to(source))] = digest(soc/name)
    report = soc / "fes_nano_spl.c"
    source_text = report.read_text()
    old = trace.function(source_text, "fes_nano_spl_report")
    new = replace_once(old, "    fes_nano_cpu_diag_park();",
                       "    if (source.failed) fes_nano_cpu_diag_park();")
    report.write_text(replace_once(source_text, old, new) + '\n#include "fes_positive.inc"\n')
    includes = "#include <asm/arch/mailbox_s10.h>\nextern void fes_nano_positive_tx(void);\n"
    end = '\tfes_nano_spl_report();\n\thang(); /* Defensive stop if a diagnostic ABI ever returns. */'
    board.write_text(includes + replace_once(board.read_text(), end,
                     '\tfes_nano_spl_report();\n' + ddr +
                     '\tfes_nano_positive_tx();\n\thang();') +
                     '\nint fes_nano_positive_sdm_status(void) { return mbox_get_fpga_config_status(MBOX_CONFIG_STATUS); }\n')
    clock = soc / "clock_manager_agilex5.c"
    clock.write_text(clock.read_text() + '''
void fes_nano_positive_clock_rates(unsigned *l4sp, unsigned *emac)
{
    *l4sp = cm_get_rate_dm(AGILEX5_L4_SP_CLK);
    *emac = cm_get_rate_dm(AGILEX5_EMAC0_CLK);
}
''')
    copied = []
    for path, name in [(HERE / "positive.inc", "fes_positive.inc"),
                       (HERE.parent / "mac_tx_diagnostic.c", "mac_tx_diagnostic.c"),
                       (HERE.parent / "mac_tx_diagnostic.h", "mac_tx_diagnostic.h")]:
        dest = soc / name
        shutil.copyfile(path, dest)
        copied.append(dest)
    frame_header = soc / "fes_mac_positive_frame.h"
    frame_header.write_text("static const unsigned char fes_positive_frame[60] = {" +
                            ",".join(str(x) for x in frame) + "};\n")
    make = soc / "Makefile"
    make.write_text(make.read_text() + "\nifdef CONFIG_SPL_BUILD\nobj-y += mac_tx_diagnostic.o\nendif\n")
    for path in [report, board, make, clock, frame_header, *copied]:
        hashes[str(path.relative_to(source))] = digest(path)
    return hashes


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--trace-checkout", type=Path, required=True)
    parser.add_argument("--frame", type=Path, required=True)
    parser.add_argument("--frame-sha256", required=True)
    args, rest = parser.parse_known_args()
    root = args.trace_checkout.resolve(strict=True)
    if subprocess.check_output(["git", "-C", root, "rev-parse", "HEAD"], text=True).strip() != TRACE or subprocess.check_output(
            ["git", "-C", root, "status", "--porcelain", "--untracked-files=no"]):
        raise ValueError("positive build requires the clean pinned trace checkout")
    frame = args.frame.read_bytes()
    header = bytes.fromhex("02fe2508cb0188b5") + b"FES-CBL-TX-v1\0"
    if (len(frame) != 60 or frame[0] & 1 or not any(frame[:6]) or frame[:6] == frame[6:12] or
            frame[6:28] != header or not any(frame[28:44]) or any(frame[44:])
            or hashlib.sha256(frame).hexdigest() != args.frame_sha256):
        raise ValueError("incorrect pinned CBL Ethernet witness frame")
    if "--console-control" in rest:
        raise ValueError("positive image requires full cold proof")
    producer = root / "platform/embedded/nano/cpu_diagnostic.py"
    sys.path.insert(0, str(producer.parent))
    spec = importlib.util.spec_from_file_location("original_cbl_cpu_trace", producer)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.PARK = "FES_CBL_MAC_TX PARK no-storage no-bl31"
    base = module.contain_source
    parking = parking_inputs(root)
    module.contain_source = lambda src, _: customize(src, module, base, frame, parking)
    output = Path(rest[rest.index("--output")+1]).resolve()
    sys.argv = [str(producer), *rest]
    module.main()
    # The historical assembler's CPU-only classification describes its original
    # selector. Replace it in this new, private build output with the true scope.
    original_receipt = output / "result.json"
    receipt = json.loads(original_receipt.read_bytes())
    receipt.update(classification="Standalone CBL pre-storage TX-only DMA diagnostic with vendor DDR calibration",
                   diagnostic_mode="mac-tx", diagnostic_runtime_qualified=False)
    original_receipt.write_text(json.dumps(receipt, sort_keys=True, indent=2)+"\n")
    asm = (output/"spl-disassembly.log").read_text()
    for name in ("fes_tx_once","fes_nano_positive_tx","fes_nano_cpu_diag_park"):
        if not re.search(r"^[0-9a-f]+ <"+name+r">:",asm,re.M):
            raise ValueError(f"positive SPL lacks {name}")
    # Inspect the emitted ARM code for the one local exception-mask transition.
    setup_instruction = re.findall(r"\bmsr\s+daifset,\s*#0x4\b", asm, re.I)
    if not setup_instruction or not re.search(r"\bmrs\s+x[0-9]+,\s*isr_el1\b", asm, re.I):
        raise ValueError("missing compiled EL3 exception-mask setup or SError readback")
    sys.path.insert(0,str(HERE.parent))
    from sof_hps import inspect
    covered = inspect((output/"cpu-state.sof").read_bytes(),
                      (output/"build/spl/u-boot-spl-dtb.hex").read_bytes(),
                      Path(rest[rest.index("--vendor-sof")+1]).read_bytes())
    result = dict(scope="de25-cbl-positive-mac-spl-v1",status="prepared",historical_trace=TRACE,
                  startup_predicate_report=True, parking_revision=PARKING, parking_sha256={name:hashlib.sha256(data).hexdigest() for name,data in parking.items()},
                  diagnostic_id=receipt["diagnostic_id"],sof_sha256=receipt["sof_sha256"],
                  spl_sha256=receipt["spl_sha256"],frame_sha256=args.frame_sha256,
                  build_receipt_sha256=digest(original_receipt),embedded_spl=covered,
                  source_sha256={p.name:digest(p) for p in (Path(__file__),HERE/"positive.inc",
                  HERE.parent/"mac_tx_diagnostic.c",HERE.parent/"mac_tx_diagnostic.h",HERE.parent/"sof_hps.py")})
    (output/"mac-positive.json").write_text(json.dumps(result,sort_keys=True,indent=2)+"\n")


if __name__ == "__main__":
    main()
