"""Opt-in standard ZX81/managed Linux base with an empty or 16K PR socket.

Uses the existing standard board and PR compiler paths. This profile does not
admit a package, supply a physical runtime constructor, or program hardware.
"""
from pathlib import Path
import importlib.util
import json

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[5]
BOARD = HERE.parent.parent
identity = "fes.de25-standard-zx81-empty-ram16k.v1"
persona_names = {"a": "zx81_empty", "b": "zx81_ram16k"}
timing_path = "sources/misteross/boards/de25nano/link-proof/zx81-standard/timing.tcl"
STATIC_ROOT = "zx81_bus"
MACHINE = "soc_inst|subsys_periph|fes_scanout|fes_scanout|core|machine"
# Pro26.3 adds these defaults when editing the HPS component. Complete IP
# bytes match the independent internal16K canonical generation control.
# Original-parameter deltas still use the existing closed Linux policy.
generated_hps_parameters = {"hps_subsys/ip/hps_subsys/agilex_hps.ip": {
    "ED_en_sim_gen": "true", "FPGA_EMAC0_tx_clk_en": "true",
    "FPGA_EMAC1_tx_clk_en": "true", "FPGA_EMAC2_tx_clk_en": "true",
    "FPGA_EMAC_PTP_clk_en": "true"}}


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


ram = load(HERE.parent / "zx81-ram/profile.py", "standard_pr_ram")
computer = load(BOARD / "zx81-hps/build.py", "standard_pr_computer")
interface = json.loads(json.dumps(ram.interface))
interface.update(name=identity, computer={"peripheral_offset": 0x10300, "span": 32,
    "clock_hz": 100000000, "pixel_hz": 74250000,
    "execution": "standard-FesGpCoreDriver-and-full-public-machine-reset",
    "frame_ownership": "standard-allocated-Linux-DMA-display",
    "runtime_admission": "not-qualified"})
interface["diagnostic"]["last_data_low_byte"] = "persona diagnostic checksum; not memory peek"
interface["machine"]["constant_request_bits"] = {"43:30": 0}
interface["machine"]["reset"] = "public-full-machine-reset-registers-an-idle-CPU-memory-bus"


def source_paths():
    return sorted(set([ROOT / n for n in computer.capture_standard_board()] + ram.source_paths() +
        [HERE / n for n in ("profile.py", "bus.v", "timing.tcl", "pcm-clocks.sdc", "pcm-timing.tcl")]))


def proof_root(project):
    # prepare_standard_board owns standard-base/combined/hps/project.
    return project.parents[3]


def standard_output(project):
    return project.parents[2]


def helpers(project):
    board = proof_root(project) / "functional-source/sources/misteross/boards/de25nano"
    std = load(board / "zx81-hps/build.py", "standard_pr_frozen_computer")
    scanout = load(board / "scanout/build.py", "standard_pr_frozen_scanout")
    display = load(board / "display/build.py", "standard_pr_frozen_display")
    return std, scanout, display


def prepare_base(archive, output, frozen):
    if not frozen["committed"]:
        raise ValueError("standard PR base requires committed captured source")
    std, _, _ = helpers(output / "standard-base/combined/hps/project")
    # Retained helpers are not Git checkouts. As in the existing scanout
    # recipe, bind all capture reads to the already verified frozen bytes.
    std.capture_standard_board = lambda revision=None: frozen["files"].copy()
    std.capture_standard = lambda revision=None: frozen["files"].copy()
    initial_id = std.sha(json.dumps({n: std.sha(d) for n,d in sorted(frozen["files"].items())},
                                    sort_keys=True).encode())[:32]
    std.prepare_standard_board(archive, output / "standard-base", frozen["revision"],
        package_build_id=initial_id, expansion_socket=True)
    return output / "standard-base/combined/hps"


def emif_reference_clock(project):
    # Preserve the canonical board's exact constraint derivation and its audit.
    # The actual generated EMIF clock and fitted coverage remain required.
    _, scanout, _ = helpers(project)
    review = scanout.managed_fabric_helper(project.parent).verify_managed_debug_constraints(project)
    return {"owner": "canonical-managed-GHRD", "constraint_review": review,
            "generated_clock_period_ns": "6.000", "fitted_validation": "required"}


def prepare(local, project, frozen):
    board = "sources/misteross/boards/de25nano/"
    retained_ram = load(proof_root(project) / "functional-source" / board / "link-proof/zx81-ram/profile.py",
                        "standard_pr_frozen_ram")
    (local / "personas.v").write_bytes(frozen["files"][board + "link-proof/zx81-ram/personas.v"])
    (local / "bus.v").write_bytes(frozen["files"][board + "link-proof/zx81-standard/bus.v"])
    (local / "zx81_bus_pack.vh").write_bytes(
        frozen["files"]["sources/misteross/cores/fes-zx81/rtl/zx81_bus_pack.vh"])
    (local / "pcm-clocks.sdc").write_bytes(frozen["files"][board + "link-proof/zx81-standard/pcm-clocks.sdc"])
    timing = project / "fes_mailbox/standard-clocks.sdc"
    timing.write_bytes(pcm_timing_source(timing.read_bytes()))
    source = frozen["files"]["sources/misteross/cores/fes-zx81/rtl/zx81_ram_pack.v"]
    (local / "zx81_ram_pack.v").write_bytes(retained_ram.retarget_ram_pack(source))
    (project / "zx8x.hex").write_bytes(frozen["files"]["sources/misteross/cores/fes-zx81/rtl/zx8x.hex"])


def pcm_timing_source(source):
    before = b'''# The internal-16K base has no PSG persona: constant PCM removes the sample
# crossing. Adding a sound persona must supply and qualify its coherent bundle.
set audio_payload [get_registers -nowarn {fes_audio_inst|audio|held_sample[*] fes_audio_inst|audio|request_meta fes_audio_inst|audio|ack_meta fes_audio_inst|audio|hold_meta}]
if {[get_collection_size $audio_payload] != 0} {error "DE25 standard base unexpectedly contains a live PCM crossing"}
'''
    if source.count(before) != 1:
        raise ValueError("standard internal16K PCM timing selection changed")
    source = source.replace(before, b'''# The PR shell retains the live expansion PCM path.
source [file join [file dirname [info script]] .. fes_link pcm-clocks.sdc]
''')
    note = b"# incorrectly align their rising edges. The base PCM data output is constant."
    if source.count(note) != 1:
        raise ValueError("standard I2S clock phase explanation changed")
    return source.replace(note, b"# incorrectly align their rising edges. Live PR PCM output timing is separate.")


def qsf(build_id):
    return ('set_global_assignment -name SEARCH_PATH fes_link\n'
            'set_global_assignment -name NUMBER_OF_PROTECTED_REGISTERS_REPORTED 10000\n'
            'set_global_assignment -name NUMBER_OF_INVERTED_REGISTERS_REPORTED 10000\n'
            'set_global_assignment -name VERILOG_FILE fes_link/bus.v\n'
            'set_global_assignment -name VERILOG_FILE fes_link/zx81_ram_pack.v\n')


def top(source):
    replace = computer.replace_once
    # Both mute causes are in the system domain. Register their combined
    # value there, so the audio synchronizer receives one glitch-free source.
    # Keep this selection local to the live-PCM PR profile.
    source = replace(source, "wire standard_exec_reset;", "wire standard_exec_reset;\n"
        "(* preserve, dont_merge *) reg standard_pr_audio_hold = 1'b1;\n"
        "always @(posedge system_clk_100_internal)\n"
        "  standard_pr_audio_hold <= standard_exec_reset || scanout_clock_fault;")
    source = replace(source, ".hold(standard_exec_reset || scanout_clock_fault)",
                     ".hold(standard_pr_audio_hold)")
    source = replace(source, "wire pr_req, pr_reset, pr_rsp_valid, pr_frozen, pr_running;",
        "wire pr_req, pr_reset, pr_rsp_valid, pr_frozen, pr_running;\n"
        "wire [45:0] zx81_request;\nwire [19:0] zx81_persona_response, zx81_response;\n"
        "wire [15:0] zx81_address;\nwire [7:0] zx81_data;\n"
        "wire zx81_mreq_n, zx81_iorq_n, zx81_rd_n, zx81_wr_n, zx81_m1_n, zx81_rfsh_n, zx81_clock, zx81_reset;")
    for name, value in (("expansion_running", "pr_running"), ("bus_addr", "zx81_address"),
            ("bus_wdata", "zx81_data"), ("bus_cpu_clock", "zx81_clock"), ("bus_reset", "zx81_reset"),
            *[("bus_" + n, "zx81_" + n) for n in ("mreq_n", "iorq_n", "rd_n", "wr_n", "m1_n", "rfsh_n")]):
        before = "1'b1" if name == "expansion_running" else ""
        source = replace(source, f".fes_scanout_expansion_{name}({before})",
                         f".fes_scanout_expansion_{name}({value})")
    for name, before, bits in (("rdata", "8'b0", "7:0"), ("peek_data", "8'b0", "15:8"),
            ("dsel", "1'b0", "16"), ("romcs", "1'b0", "17"), ("wait", "1'b0", "18"),
            ("ram_present", "1'b0", "19")):
        source = replace(source, f".fes_scanout_expansion_bus_{name}({before})",
                         f".fes_scanout_expansion_bus_{name}(zx81_response[{bits}])")
    return replace(source, ".rsp_valid(pr_rsp_valid), .rsp_data(pr_rsp_data));",
        ".rsp_valid(pr_rsp_valid), .rsp_data(pr_rsp_data),\n"
        ".bus_request(zx81_request), .bus_response(zx81_persona_response));\n"
        "fes_de25_zx81_pr_bus zx81_bus (\n"
        ".clk(system_clk_100_internal), .machine_reset(zx81_reset), .cpu_clock(zx81_clock),\n"
        ".address(zx81_address), .write_data(zx81_data), .mreq_n(zx81_mreq_n), .iorq_n(zx81_iorq_n),\n"
        ".rd_n(zx81_rd_n), .wr_n(zx81_wr_n), .m1_n(zx81_m1_n), .rfsh_n(zx81_rfsh_n),\n"
        ".peek_address(14'd0), .bus_response(zx81_persona_response),\n"
        ".bus_request(zx81_request), .response(zx81_response));")


def bind_identity(project, build_id):
    std, _, _ = helpers(project)
    receipt = project.parent / "inputs.json"
    inputs = json.loads(receipt.read_text())
    previous = inputs["standard_zx81"]["build_id"]
    script = project / "integrate-mailbox.tcl"
    script.write_text(std.replace_once(script.read_text(),
        "set_instance_parameter_value fes_scanout BUILD_ID 0x" + previous,
        "set_instance_parameter_value fes_scanout BUILD_ID 0x" + build_id))
    inputs["scanout"]["build_id"] = inputs["standard_zx81"]["build_id"] = build_id
    inputs["standard_zx81"]["standard_sdc_sha256"] = std.sha(
        (project / "fes_mailbox/standard-clocks.sdc").read_bytes())
    inputs["standard_zx81"]["staged_source_sha256"] = std.standard_staged_sources(project)
    inputs["standard_zx81"]["pr_pcm_timing"] = "coherent-mono8-v1"
    inputs["scanout"]["compiled_top_sha256"] = std.sha((project / "golden_top.v").read_bytes())
    inputs["scanout"]["rendered_integration_sha256"] = std.sha(script.read_bytes())
    receipt.write_text(json.dumps(inputs, indent=2, sort_keys=True) + '\n')
    (project / "fes_link/standard-base-inputs.json").write_bytes(receipt.read_bytes())
    path = standard_output(project) / "snapshot.json"
    snapshot = json.loads(path.read_text()); snapshot["standard_zx81"] = inputs["standard_zx81"]
    path.write_text(json.dumps(snapshot, indent=2, sort_keys=True) + '\n')


def verify_memory_handoff(project, hps):
    return hps.verify_scanout_memory_handoff(project)


def verify_generation(project, hps):
    std, scanout, display = helpers(project)
    if ((project.parent / "inputs.json").read_bytes() !=
            (project / "fes_link/standard-base-inputs.json").read_bytes()):
        raise ValueError("standard base preparation receipt changed")
    original = proof_root(project) / "functional-source/sources/misteross/boards/de25nano/zx81-hps/standard-clocks.sdc"
    if (project / "fes_mailbox/standard-clocks.sdc").read_bytes() != pcm_timing_source(original.read_bytes()):
        raise ValueError("standard PR PCM timing derivation changed")
    scanout.repair_generated_reset_timing(project.parent)
    scanout.shared_reset_helper(project.parent).apply(project.parent)
    result = std.verify_standard_board(standard_output(project), scanout, hps, display,
                                       expected_expansion_socket=True)
    result["expansion_socket"] = True
    result["timing_and_hardware_admission"] = "not-qualified"
    return result


def verify_memories(revision, rows):
    ram.verify_memories(revision, rows, static_root=MACHINE)


def verify_synthesis(report):
    return ram.verify_synthesis(report, static_root=STATIC_ROOT)


def extra_project_inputs(project):
    return [project / "integrate-mailbox.tcl", project / "fes_combined/sys_pll.ip",
            *[p for p in (project / "fes_mailbox").rglob("*") if p.is_file()]]
