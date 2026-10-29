"""ZX81 RAM-pack proof profile for the shared exact-device PR builder."""
from pathlib import Path
HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[5]
MISTEROSS = ROOT / "sources/misteross"
identity = "fes.de25-zx81-empty-ram16k.v1"
persona_names = {"a": "zx81_empty", "b": "zx81_ram16k"}
timing_path = "sources/misteross/boards/de25nano/link-proof/zx81-ram/timing.tcl"
interface = {
    "name": identity, "version": 1, "clock_hz": 100000000,
    "diagnostic": {"request": {"valid": 1, "address": 8}, "response": {"valid": 1, "data": 32},
        "latency_cycles": 5, "id_probe_address": 0,
        "last_data_low_byte": "machine-peek-at-0x4000-plus-last-diagnostic-address"},
    "machine": {"request_bits": 46, "response_bits": 20, "packing": "zx81_bus_pack.vh",
        "latency_cycles": 4, "sampled_edges": 5, "maximum_contract_cycles": 6,
        "constant_request_bits": {"43:38": 0}, "constant_response_bits": {"18:16": 0, "19": "persona-RAM-parameter"},
        "reset": "running-low-resets-machine-and-disables-request-writes",
        "freeze": "drain-diagnostic-then-four-reset-settle-cycles-flush-bus",
        "selection": "resets-machine-no-program-survival"}}
CORE = MISTEROSS / "cores/fes-zx81/rtl"
Z80 = MISTEROSS / "cores/fes-common/rtl/z80"
FILES = {name: CORE / name for name in ("zx81_machine.sv", "zx81_machine_clock.v", "zx81_dpram.v",
    "zx81_ram_pack.v", "zx81_bus_pack.vh", "zx81_model.vh", "zx8x.hex", "zx8x.mif")}
FILES.update({name: Z80 / name for name in ("fes_z80_alu.sv", "fes_z80_engine.sv", "fes_z80_bus.sv", "fes_z80_nmos.sv")})


def source_paths():
    return list(FILES.values()) + [HERE / name for name in ("profile.py", "personas.v", "static.v", "timing.tcl")]


def replace(source, before, after, label):
    if source.count(before) != 1:
        raise ValueError("ZX81 profile derivation changed: " + label)
    return source.replace(before, after)


def prepare(local, project, frozen):
    def selected(path):
        return frozen["files"][path.relative_to(ROOT).as_posix()]
    for name, path in FILES.items():
        data = selected(path)
        if name == "zx81_dpram.v":
            for before, after in ((b'intended_device_family = "Cyclone V"', b'intended_device_family = "Agilex 5"'),
                                  (b'ram_block_type = "M10K"', b'ram_block_type = "M20K"'),
                                  (b'ramstyle = "M10K"', b'ramstyle = "M20K"')):
                data = replace(data, before, after, name)
        if name == "zx81_ram_pack.v":
            begin = data.index(b'`ifdef SYNTHESIS')
            split = data.index(b'`else', begin)
            end = data.index(b'`endif', split)
            if data.count(b'`ifdef') != 1 or data.count(b'`else') != 1 or data.count(b'`endif') != 1:
                raise ValueError("RAM pack conditional derivation changed")
            data = data[:begin] + data[split + len(b'`else'):end] + data[end + len(b'`endif'):]
        (local / name).write_bytes(data)
    static = selected(HERE / "static.v")
    # These exact drivers justify the six optional constant ingress FFs.
    for driver in (b'wire [15:0] peek_addr = {8'h40, diagnostic_address};',
                   b'bus_request <= {running, cpu_clock, peek_addr[13:0], rfsh_n, m1_n,'):
        if static.count(driver) != 1:
            raise ValueError("constant bus driver derivation changed")
    persona = selected(HERE / "personas.v")
    if persona.count(b'bus_response <= {RAM != 0, 3'b000, peek_data, read_data};') != 1:
        raise ValueError("constant response driver derivation changed")
    for name in ("personas.v", "static.v"):
        (local / name).write_bytes(selected(HERE / name))
    for name in ("zx8x.hex", "zx8x.mif"):
        (project / name).write_bytes(selected(FILES[name]))
    shell = (local / "shell.v").read_bytes()
    shell = replace(shell, b'input wire rsp_valid,', b'input wire [7:0] memory_peek,\n    input wire rsp_valid,', "peek input")
    shell = replace(shell, b'last_data <= response_data_q;',
        b'last_data <= state == RUNNING ? {response_data_q[31:8], memory_peek} : response_data_q;', "peek retirement")
    (local / "shell.v").write_bytes(shell)
    hw = local / "fes_de25_pr_control_hw.tcl"
    hw.write_bytes(hw.read_bytes() + b'\nadd_interface_port diagnostic memory_peek memory_peek Input 8\n')


def qsf():
    verilog = ("static.v", "zx81_machine_clock.v", "zx81_dpram.v", "zx81_ram_pack.v")
    sv = ("zx81_machine.sv", "fes_z80_alu.sv", "fes_z80_engine.sv", "fes_z80_bus.sv", "fes_z80_nmos.sv")
    return ('set_global_assignment -name VERILOG_MACRO "QUARTUS=1"\n'
            'set_global_assignment -name SEARCH_PATH fes_link\n' +
        ''.join(f'set_global_assignment -name {kind} fes_link/{name}\n' for kind, files in
            (("VERILOG_FILE", verilog), ("SYSTEMVERILOG_FILE", sv)) for name in files))


def top(source):
    source = replace(source, 'wire pr_req, pr_reset, pr_rsp_valid, pr_frozen, pr_running;',
        'wire pr_req, pr_reset, pr_rsp_valid, pr_frozen, pr_running;\nwire [45:0] zx81_request;\nwire [19:0] zx81_response;\nwire [7:0] zx81_peek;', "top declarations")
    source = replace(source, '.fes_pr_slot_req(pr_req),',
        '.fes_pr_diagnostic_memory_peek(zx81_peek),\n.fes_pr_slot_req(pr_req),', "peek export")
    source = replace(source, '.rsp_valid(pr_rsp_valid), .rsp_data(pr_rsp_data));',
        '.rsp_valid(pr_rsp_valid), .rsp_data(pr_rsp_data),\n.bus_request(zx81_request), .bus_response(zx81_response));\n'
        'fes_de25_zx81_static zx81_static (.clk(system_clk_100_internal), .running(pr_running),\n'
        '.diagnostic_address(pr_address), .bus_response(zx81_response), .bus_request(zx81_request),\n'
        '.peek_data(zx81_peek), .cpu_addr());', "static machine")
    return source


def verify_memories(revision, rows):
    slot = [r for r in rows if r.get("Name", "").startswith("slot_inst|")]
    if revision == "persona_b":
        if (sum(int(r["Logical Size"]) for r in slot) != 131072 or
                any(r.get("Type") != "M20K" or r.get("MLABs") != "0.000" for r in slot)):
            raise ValueError("RAM persona must contain exactly 16 KiB in M20K")
    elif slot:
        raise ValueError("empty persona must contain no memory")
    machine = [r for r in rows if r.get("Name", "").startswith("zx81_static|")]
    if not any("|rom|" in r["Name"] and int(r["Logical Size"]) == 131072 and
               r.get("Implementation Port A Depth") == "8192" and r.get("Implementation Port A Width") == "8" and
               r.get("Type") == "M20K" for r in machine):
        raise ValueError("static ZX81 8 KiB firmware M20K is missing")
    if not any(int(r["Logical Size"]) == 8192 and r.get("Type") == "M20K" for r in machine):
        raise ValueError("static ZX81 1 KiB internal RAM M20K is missing")
