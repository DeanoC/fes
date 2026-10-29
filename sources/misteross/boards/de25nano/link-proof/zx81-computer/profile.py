"""Combined computer/mailbox/display and registered PR RAM-pack diagnostic."""
from pathlib import Path
import importlib.util
import json
import re
import xml.etree.ElementTree as ET

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[5]
BOARD = HERE.parent.parent
COMPUTER = BOARD / "zx81-hps"
identity = "fes.de25-zx81-computer-empty-ram16k.v1"
persona_names = {"a": "zx81_empty", "b": "zx81_ram16k"}
timing_path = "sources/misteross/boards/de25nano/link-proof/zx81-computer/timing.tcl"
COMPUTER_INSTANCE = "soc_inst|subsys_periph|fes_computer|fes_computer"
STATIC_ROOT = COMPUTER_INSTANCE + "|external_bus"


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


ram = load(HERE.parent / "zx81-ram/profile.py", "de25_computer_ram_recipe")
computer = load(COMPUTER / "build.py", "de25_computer_base_recipe")
interface = json.loads(json.dumps(ram.interface))
interface.update(name=identity, computer={"peripheral_offset": 0x10300, "span": 32,
    "clock_hz": 100000000, "pixel_hz": 74250000,
    "execution": "transport-healthy-and-PR-running-and-mailbox-RUN",
    "frame_ownership": "two-packed-M20K-banks-acknowledged-at-output-frame-boundary"})
interface["machine"]["reset"] = "PR-not-running-or-mailbox-HOLD-or-transport-fault-registers-idle-memory-bus"


def source_paths():
    paths = ram.source_paths() + [ROOT / n for n in computer.capture()]
    paths += [HERE / n for n in ("profile.py", "timing.tcl")]
    return sorted(set(paths))


def captured_helpers(project):
    snapshot = project.parent.parent / "functional-source"
    board = snapshot / "sources/misteross/boards/de25nano"
    return (load(board / "link-proof/zx81-ram/profile.py", "de25_frozen_ram_recipe"),
            load(board / "zx81-hps/build.py", "de25_frozen_computer_recipe"))


def prepare_base(archive, output, frozen):
    _, comp = captured_helpers(output / "hps/project")
    selected = {"fes_revision": frozen["revision"], "source_classification":
                "committed" if frozen["committed"] else "working-copy-diagnostic"}
    comp.prepare_captured(archive, output, frozen["files"], selected)
    return output / "hps"


def prepare(local, project, frozen):
    parent, comp = captured_helpers(project)
    parent.prepare(local, project, frozen)
    # The computer component owns machine/ROM/keyboard/tape/video. Its FPGA
    # fileset is the one compilation path for those modules; the slot alone
    # adds the RAM-pack module at the root. Remove duplicate generated inputs.
    mailbox = project / "fes_mailbox"
    for name in parent.FILES:
        if name == "zx81_ram_pack.v":
            continue
        target = "zx81_dpram_de25.v" if name == "zx81_dpram.v" else name
        (mailbox / target).write_bytes((local / name).read_bytes())
        (local / name).unlink()
    (local / "static.v").unlink()
    script = project / "integrate-mailbox.tcl"
    text = script.read_text()
    anchor = "add_instance fes_computer fes_de25_computer 1.0"
    text = comp.replace_once(text, anchor, anchor + "\nset_instance_parameter_value fes_computer EXTERNAL_RAM 1")
    anchor = "save_system $peripheral_system\n\n# Refresh"
    text = comp.replace_once(text, anchor, "add_interface fes_computer_expansion conduit end\n"
        "set_interface_property fes_computer_expansion EXPORT_OF fes_computer.expansion\n\n" + anchor)
    anchor = "save_system " + comp.load(project.parent.parent / "functional-source" / comp.BOARD / "hps/build.py", "de25_comp_hps_tcl").tcl_word(project / "qsys_top.qsys") + "\n"
    text = comp.replace_once(text, anchor, "add_interface fes_computer_expansion conduit end\n"
        "set_interface_property fes_computer_expansion EXPORT_OF subsys_periph.fes_computer_expansion\n" + anchor)
    script.write_text(text)
    qsf = project / "golden_top.qsf"
    text, count = re.subn(r'^set_global_assignment -name VERILOG_MACRO "FES_DE25_COMPUTER_BUILD_ID=.*\n', '', qsf.read_text(), flags=re.M)
    if count != 1: raise ValueError("computer build identity assignment changed")
    qsf.write_text(text)
    endpoint = frozen["files"]["sources/misteross/boards/de25nano/zx81-hps/fes_de25_computer.v"]
    if endpoint.count(b"wire [15:0] peek_address = EXTERNAL_RAM ? {8'h40, diagnostic_address}") != 1:
        raise ValueError("combined diagnostic peek address changed")
    if endpoint.count(b"!machine_reset ? {bus_rfsh_n, bus_m1_n, bus_wr_n, bus_rd_n,") != 1:
        raise ValueError("combined CPU bus fence changed")


def qsf(build_id):
    return ('set_global_assignment -name VERILOG_MACRO "QUARTUS=1"\n'
            f'set_global_assignment -name VERILOG_MACRO "FES_DE25_COMPUTER_BUILD_ID=128\'h{build_id}"\n'
            'set_global_assignment -name SEARCH_PATH fes_link\n'
            'set_global_assignment -name NUMBER_OF_PROTECTED_REGISTERS_REPORTED 10000\n'
            'set_global_assignment -name NUMBER_OF_INVERTED_REGISTERS_REPORTED 10000\n'
            'set_global_assignment -name VERILOG_FILE fes_link/zx81_ram_pack.v\n')


def top(source):
    source = computer.replace_once(source, "wire pr_req, pr_reset, pr_rsp_valid, pr_frozen, pr_running;",
        "wire pr_req, pr_reset, pr_rsp_valid, pr_frozen, pr_running;\n"
        "wire [45:0] zx81_request;\nwire [19:0] zx81_response;\nwire [7:0] zx81_peek;")
    source = computer.replace_once(source, ".fes_pr_slot_req(pr_req),",
        ".fes_pr_diagnostic_memory_peek(zx81_peek),\n"
        ".fes_computer_expansion_expansion_running(pr_running),\n"
        ".fes_computer_expansion_diagnostic_address(pr_address),\n"
        ".fes_computer_expansion_expansion_request(zx81_request),\n"
        ".fes_computer_expansion_expansion_response(zx81_response),\n"
        ".fes_computer_expansion_memory_peek(zx81_peek),\n.fes_pr_slot_req(pr_req),")
    return computer.replace_once(source, ".rsp_valid(pr_rsp_valid), .rsp_data(pr_rsp_data));",
        ".rsp_valid(pr_rsp_valid), .rsp_data(pr_rsp_data),\n"
        ".bus_request(zx81_request), .bus_response(zx81_response));")


def verify_generation(project, hps):
    _, comp = captured_helpers(project)
    result = comp.verify_computer(project, hps)
    tree = ET.parse(project / "peripheral_subsys/peripheral_subsys.qsys")
    parameters = tree.findall(".//module[@name='fes_computer']/parameter[@name='EXTERNAL_RAM']")
    if len(parameters) != 1 or parameters[0].get("value") != "1":
        raise ValueError("generated computer must select external PR RAM")
    result["external_ram"] = True
    result["pll_reviews"] = {"system": comp.pll_review(project / "ip/pll", 100000000),
                             "pixel": comp.pll_review(project / "sys_pll", 74250000)}
    return result


def verify_memories(revision, rows):
    ram.verify_memories(revision, rows, static_root=COMPUTER_INSTANCE)
    video = [r for r in rows if r.get("Name", "").startswith(COMPUTER_INSTANCE + "|video|")]
    tape = [r for r in rows if r.get("Name", "").startswith(COMPUTER_INSTANCE + "|tape|")]
    for label, selected, bits in (("video", video, 177120), ("tape", tape, 131072)):
        if sum(int(r["Logical Size"]) for r in selected) != bits or any(r.get("Type") != "M20K" for r in selected):
            raise ValueError("combined " + label + " must map to its exact M20K storage")


def verify_synthesis(report):
    return ram.verify_synthesis(report, static_root=STATIC_ROOT)
