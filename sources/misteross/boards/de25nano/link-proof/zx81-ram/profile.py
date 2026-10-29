"""ZX81 RAM-pack proof profile for the shared exact-device PR builder."""
from pathlib import Path
import re
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
        "reset": "running-low-resets-machine-and-registers-an-idle-CPU-memory-bus",
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
        if name == "zx8x.hex":
            words = data.split()
            if len(words) != 16384 or any(int(word, 16) != 255 for word in words[12288:]):
                raise ValueError("ZX81 source HEX depth or unreachable tail changed")
            data = b"\n".join(words[:12288]) + b"\n" + b"00\n" * 4096
        if name == "zx8x.mif":
            data = replace(data, b"DEPTH = 12288;", b"DEPTH = 16384;", "firmware depth")
            data = replace(data, b"END;", b"[3000..3FFF]: 00;\r\nEND;", "firmware unreachable tail")
        (local / name).write_bytes(data)
    machine = selected(FILES["zx81_machine.sv"])
    if machine.count(b".address_a({1'b0, rom_a[12], rom_a[11:0]}),") != 1:
        raise ValueError("ZX81 8 KiB reachable firmware window changed")
    static = selected(HERE / "static.v")
    # These exact drivers justify the six optional constant ingress FFs.
    for driver in (b"wire [15:0] peek_addr = {8'h40, diagnostic_address};",
                   b'bus_request <= {running, cpu_clock, peek_addr[13:0],'):
        if static.count(driver) != 1:
            raise ValueError("constant bus driver derivation changed")
    if static.count(b"running ? {rfsh_n, m1_n, wr_n, rd_n, iorq_n, mreq_n, d, a} : {6'b111111, 24'd0}") != 1:
        raise ValueError("frozen CPU bus fence derivation changed")
    persona = selected(HERE / "personas.v")
    if persona.count(b"bus_response <= {RAM != 0, 3'b000, peek_data, read_data};") != 1:
        raise ValueError("constant response driver derivation changed")
    for name in ("personas.v", "static.v"):
        (local / name).write_bytes(selected(HERE / name))
    for name in ("zx8x.hex", "zx8x.mif"):
        (project / name).write_bytes((local / name).read_bytes())
    shell = (local / "shell.v").read_bytes()
    shell = replace(shell, b'input wire rsp_valid,', b'input wire [7:0] memory_peek,\n    input wire rsp_valid,', "peek input")
    shell = replace(shell, b'last_data <= response_data_q;',
        b'last_data <= state == RUNNING ? {response_data_q[31:8], memory_peek} : response_data_q;', "peek retirement")
    (local / "shell.v").write_bytes(shell)
    hw = local / "fes_de25_pr_control_hw.tcl"
    hw.write_bytes(hw.read_bytes() + b'\nadd_interface_port diagnostic memory_peek memory_peek Input 8\n')


def qsf(build_id=None):
    verilog = ("static.v", "zx81_machine_clock.v", "zx81_dpram.v", "zx81_ram_pack.v")
    sv = ("zx81_machine.sv", "fes_z80_alu.sv", "fes_z80_engine.sv", "fes_z80_bus.sv", "fes_z80_nmos.sv")
    return ('set_global_assignment -name VERILOG_MACRO "QUARTUS=1"\n'
            'set_global_assignment -name SEARCH_PATH fes_link\n'
            'set_global_assignment -name NUMBER_OF_PROTECTED_REGISTERS_REPORTED 10000\n'
            'set_global_assignment -name NUMBER_OF_INVERTED_REGISTERS_REPORTED 10000\n' +
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


def verify_memories(revision, rows, static_root="zx81_static"):
    slot = [r for r in rows if r.get("Name", "").startswith("slot_inst|")]
    if revision == "persona_b":
        if (sum(int(r["Logical Size"]) for r in slot) != 131072 or
                any(r.get("Type") != "M20K" or r.get("MLABs") != "0.000" for r in slot)):
            raise ValueError("RAM persona must contain exactly 16 KiB in M20K")
    elif slot:
        raise ValueError("empty persona must contain no memory")
    machine = [r for r in rows if r.get("Name", "").startswith(static_root + "|")]
    if not any("|rom|" in r["Name"] and int(r["Logical Size"]) == 131072 and
               r.get("Implementation Port A Depth") == "8192" and r.get("Implementation Port A Width") == "8" and
               r.get("Type") == "M20K" for r in machine):
        raise ValueError("static ZX81 8 KiB firmware M20K is missing")
    if not any(int(r["Logical Size"]) == 8192 and r.get("Type") == "M20K" for r in machine):
        raise ValueError("static ZX81 1 KiB internal RAM M20K is missing")


def verify_synthesis(report, static_root="zx81_static"):
    text = report.read_text()
    names = re.findall(r"^;\s*(slot_inst\|core\|bus_response\[([0-9]+)\](?:~reg0)?)\s*;\s*yes\s*;\s*yes\s*;\s*yes\s*;", text, re.M)
    if len(names) != 20 or {int(bit) for name, bit in names} != set(range(20)):
        raise ValueError("synthesis must retain all 20 protected persona response registers")
    launch = re.findall(r"^;\s*(" + re.escape(static_root) + r"\|bus_request\[([0-9]+)\](?:~reg0)?)\s*;\s*yes\s*;\s*yes\s*;\s*yes\s*;", text, re.M)
    if report.name == "base.syn.rpt" and (len(launch) != 46 or {int(bit) for name, bit in launch} != set(range(46))):
        raise ValueError("base synthesis must retain all 46 protected static request launch registers")
    active = False
    for line in text.splitlines():
        if re.match(r"^;\s*Inverted Register Statistics\s*;", line): active = True
        elif not line.strip(): active = False
        elif active and re.search(r"slot_inst\|core\|(?:bus_response|bus_q|reset_q|request_q|address_q)|" + re.escape(static_root) + r"\|(?:bus_request|response_q)", line):
            raise ValueError("synthesis inverted a protected PR boundary register")
    return {"protected_response_registers": [name for name, bit in names],
            "protected_static_request_registers": [name for name, bit in launch], "boundary_inversions": 0}
