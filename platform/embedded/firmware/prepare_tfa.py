#!/usr/bin/env python3
"""Prepare a contained-only TF-A source variant from the exact reviewed file.

Writes a fresh private output, never patches the reference checkout in place.
The resulting source is a prototype, not a boot-qualified firmware profile.
"""
import argparse
import difflib
import hashlib
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
SOURCE = "plat/intel/soc/common/socfpga_sip_svc.c"
REFERENCE_BLOB = "3c223c1498e3a412e075e4f921cd78acd3d0fcd6"


def contained_source(data):
    blob = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
    if blob != REFERENCE_BLOB:
        raise ValueError("TF-A source is not the reviewed v2.12 Git blob")
    text = data.decode()
    def replace(old, new):
        nonlocal text
        if text.count(old) != 1:
            raise ValueError("pinned TF-A patch context is not unique")
        text = text.replace(old, new)
    header = (Path(__file__).parent / "generated/fes_embedded_control.hpp").read_text()
    def constant(name):
        values = re.findall(r"constexpr std::uint32_t " + name + r" = (0x[0-9a-f]+)u;", header)
        if len(values) != 1:
            raise ValueError("missing generated firmware contract constant " + name)
        return values[0]
    query = constant("FesEmbeddedFirmwareQuery")
    fields = ["0", constant("FesEmbeddedFirmwareMagic"), constant("FesEmbeddedFirmwareVersion"), constant("FesEmbeddedFirmwareFeatures")]
    helper = "static void fes_contained_query(uint64_t result[4])\n{\n" + "".join("\tresult[%d] = %sU;\n" % (i,v) for i,v in enumerate(fields)) + "}\n\n"
    replace("static uint64_t socfpga_sip_handler(", helper + "static uint64_t socfpga_sip_handler(")
    replace("\tcase INTEL_SIP_SMC_FPGA_CONFIG_ISDONE:",
            "\tcase " + query + "U: {\n\t\tuint64_t result[4];\n\t\tfes_contained_query(result);\n"
            "\t\tSMC_RET4(handle, result[0], result[1], result[2], result[3]);\n\t}\n\n"
            "\tcase INTEL_SIP_SMC_FPGA_CONFIG_ISDONE:")
    replace("\tif (bridge_disable != 0U) {\n\t\tsocfpga_bridges_enable(~0);\t/* Enable bridge */\n\t\tbridge_disable = false;\n\t}",
            "\t/* FES contained profile: ISDONE never opens a bridge. The runtime\n"
            "\t * separately opens only the control bridge, verifies live identity,\n"
            "\t * then explicitly releases data bridges. */")
    replace("\trequest_type = RECONFIGURATION;\n\n\tif (!CONFIG_TEST_FLAG(flag, PARTIAL_CONFIG)) {",
            "\t/* FES profile admits full configuration only. */\n"
            "\tif (flag != 0U) {\n\t\treturn INTEL_SIP_SMC_STATUS_REJECTED;\n\t}\n\n"
            "\trequest_type = RECONFIGURATION;\n\n\tif (!CONFIG_TEST_FLAG(flag, PARTIAL_CONFIG)) {")
    old = "\t/* Disable bridge on full reconfiguration */\n\tif (bridge_disable) {\n\t\tsocfpga_bridges_disable(~0);\n\t}\n"
    replace(old, "")
    replace("\tmailbox_clear_response();\n\n\tmailbox_send_cmd(MBOX_JOB_ID, MBOX_CMD_CANCEL, NULL, 0U,\n\t\t\tCMD_CASUAL, NULL, NULL);",
            "\t/* Containment precedes CANCEL/RECONFIG, not just data writes. */\n"
            "\tif (socfpga_bridges_disable(~0U) != 0) {\n"
            "\t\trequest_type = NO_REQUEST;\n\t\treturn INTEL_SIP_SMC_STATUS_ERROR;\n\t}\n"
            "\tmailbox_clear_response();\n\n"
            "\tstatus = mailbox_send_cmd(MBOX_JOB_ID, MBOX_CMD_CANCEL, NULL, 0U,\n"
            "\t\t\tCMD_CASUAL, NULL, NULL);\n"
            "\tif (status < 0) {\n\t\trequest_type = NO_REQUEST;\n"
            "\t\treturn INTEL_SIP_SMC_STATUS_ERROR;\n\t}")
    replace("\tif (status < 0) {\n\t\tbridge_disable = false;\n\t\trequest_type = NO_REQUEST;\n\t\treturn INTEL_SIP_SMC_STATUS_ERROR;\n\t}\n\n\tmax_blocks = response[0];",
            "\tif (status < 0 || resp_len < 2U || resp_len > ARRAY_SIZE(response) ||\n"
            "\t    response[0] == 0U || response[0] > INT32_MAX || response[1] == 0U) {\n"
            "\t\trequest_type = NO_REQUEST;\n\t\treturn INTEL_SIP_SMC_STATUS_ERROR;\n\t}\n\n"
            "\tmax_blocks = response[0];")
    # START initializes remapping with NULL. WRITE supplies a real pointer.
    old = "\t\t*mem = *mem - DRAM_BASE;\n\t} else {\n\t\t*mem = *mem - DRAM_BASE;\n\t}\n"
    replace(old, "\t}\n\tif (mem != NULL) {\n\t\t*mem = *mem - DRAM_BASE;\n\t}\n")
    return text.encode()


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--reference", type=Path, required=True)
    p.add_argument("--output", type=Path, required=True)
    a = p.parse_args()
    output = a.output.absolute()
    if ".." in output.parts or any(x.is_symlink() for x in (output, *output.parents)) or not output.is_relative_to(ROOT / "out") or output.exists():
        p.error("output must be a fresh non-symlink directory beneath this worktree's out/")
    original = a.reference.read_bytes()
    modified = contained_source(original)
    output.mkdir(parents=True)
    (output / "socfpga_sip_svc.c").write_bytes(modified)
    diff = "".join(difflib.unified_diff(original.decode().splitlines(True), modified.decode().splitlines(True),
                                      fromfile="a/" + SOURCE, tofile="b/" + SOURCE))
    (output / "contained.patch").write_text(diff)
    (output / "result.json").write_text(json.dumps({
        "reference_git_blob": REFERENCE_BLOB, "source_path": SOURCE,
        "reference_sha256": hashlib.sha256(original).hexdigest(),
        "patched_sha256": hashlib.sha256(modified).hexdigest(),
        "patch_sha256": hashlib.sha256(diff.encode()).hexdigest(),
        "preparer_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "contract_header_sha256": hashlib.sha256((Path(__file__).parent / "generated/fes_embedded_control.hpp").read_bytes()).hexdigest(),
        "classification": "prepared TF-A source only; not boot or hardware acceptance",
        "limitations": ["explicit bridge enable still requires exclusive runtime sequencing and board readback",
                        "control-only bridge routing and DDR remapper behaviour require board qualification"]}, indent=2) + "\n")
    print(output)


if __name__ == "__main__":
    main()
