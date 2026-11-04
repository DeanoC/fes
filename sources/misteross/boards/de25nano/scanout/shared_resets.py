"""Share the pinned generated FIFO reset pipelines without changing their timing.

Managed MENU and standard ZX81 board integration select this derivation. The original eleven generated
files are checked before any write, retained, and used to reproduce every
derived byte during generation and after compilation. Vendor SDCs are untouched.
"""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import re

POLICY = "shared-fifo-reset-release-v2"
# The standard component adds its computer slave and audio/expansion conduits
# to this parent. Three independent whole-GHRD generations produced these same
# bytes. The ten bridge/FIFO inputs and their reset derivation stay unchanged.
STANDARD_PARENT_SHA256 = "290fc7e68ed0f462928526008ee374672c7b1fd33ba4b14a4f973114f05baaa5"
ORIGINAL_SHA256 = {
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_ctrl_cdc/mm_ccb_1941/synth/peripheral_subsys_fes_ctrl_cdc_mm_ccb_1941_evnq7wy.v": "327a7d7dfb1182ca0eba406788c3978e07e572ac83243937ca4f84de21949e56",
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_ctrl_cdc/mm_ccb_1941/synth/peripheral_subsys_fes_ctrl_cdc_mm_ccb_st_dc_fifo_1941_gbaclly.v": "60cccc095ab3f6f3281b15f10b02ba2efe6cb52ade7a3c943d973eb7e416e91d",
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_ctrl_cdc/mm_ccb_1941/synth/peripheral_subsys_fes_ctrl_cdc_mm_ccb_st_dc_fifo_1941_l42a5jy.v": "7f419c9d9a34d3175e42e0b583e5572520479fe743e6e43a46c660591a582c2a",
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_ctrl_cdc/st_dc_fifo_1953/synth/peripheral_subsys_fes_ctrl_cdc_st_dc_fifo_1953_phmrs5y.v": "449c255ed9f935bb98818fda6aa687d2f8554cfcf661c2bd7b6e2ce0c65b3e2c",
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_ctrl_cdc/synth/peripheral_subsys_fes_ctrl_cdc.v": "3026fe44b4a07e889bc8f45a62de5651f770924d142e2b84482ed6bd8f5cbd43",
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_mem_cdc/mm_ccb_1941/synth/peripheral_subsys_fes_mem_cdc_mm_ccb_1941_l4pgxrq.v": "973f9ab0f79f699ffb0bffc5509aea38bfce2a5f01996e9e6a1e22614c58f19c",
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_mem_cdc/mm_ccb_1941/synth/peripheral_subsys_fes_mem_cdc_mm_ccb_st_dc_fifo_1941_l4vfgda.v": "a36ded98ec4479afdd80b31795a48f69c8d8b95fe1f2354ecb98877353ec13fd",
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_mem_cdc/mm_ccb_1941/synth/peripheral_subsys_fes_mem_cdc_mm_ccb_st_dc_fifo_1941_lxzjj3q.v": "51927637e0794c8db104ccdeabcccece314864f3a7d85c8e9f100d3fe9a693af",
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_mem_cdc/st_dc_fifo_1953/synth/peripheral_subsys_fes_mem_cdc_st_dc_fifo_1953_phmrs5y.v": "5011de57705fe87ca1dca46601c8caf166047bca4e71a1282e89b8b36464c8b6",
    "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_fes_mem_cdc/synth/peripheral_subsys_fes_mem_cdc.v": "6f5e133d0d28bc613136e3913326993bed11123effca9b98801d16d96510c44c",
    "peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v": "1d4a65fd2bf39dd40269135039440888ffa708353989499ea5a2e3a4c3c5cbc8"
}
PARENT = "peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v"
BRIDGE_PORTS = ("fes_shared_s0_reset", "fes_shared_m0_reset")
FIFO_PORTS = ("fes_shared_in_reset", "fes_shared_out_reset")


def sha(data):
    return hashlib.sha256(data).hexdigest()


def require(condition, message):
    if not condition:
        raise ValueError("shared FIFO reset: " + message)


def replace_once(text, old, new):
    require(text.count(old) == 1, "generated replacement target differs: " + old[:80])
    return text.replace(old, new)


def port_region(text):
    masked = re.sub(r'//[^\n]*|/\*.*?\*/|"(?:\\.|[^"\\])*"',
                    lambda match: " " * len(match[0]), text, flags=re.S)
    module = re.search(r"\bmodule\s+\w+\s*", masked)
    require(module is not None, "missing generated module")

    def closing(start):
        require(masked[start] == "(", "missing generated port/parameter list")
        depth, cursor = 1, start + 1
        while depth and cursor < len(masked):
            depth += (masked[cursor] == "(") - (masked[cursor] == ")")
            cursor += 1
        require(depth == 0, "unterminated generated port/parameter list")
        return cursor

    cursor = module.end()
    if masked[cursor] == "#":
        cursor = closing(masked.index("(", cursor))
        while masked[cursor].isspace():
            cursor += 1
    stop = closing(cursor)
    return cursor + 1, stop - 1, masked.index(";", stop) + 1, masked


def add_ports(text, names):
    start, stop, body, masked = port_region(text)
    require(all(not re.search(r"\b" + name + r"\b", text) for name in names),
            "shared ports already present")
    if re.search(r"\binput\b", masked[start:stop]):
        return text[:start] + "\n" + "".join("    input wire " + name + ",\n" for name in names) + text[start:]
    return (text[:start] + "\n" + "".join("    " + name + ",\n" for name in names) + text[start:body] +
            "\n" + "".join("    input wire " + name + ";\n" for name in names) + text[body:])


def connect(text, instance, connections):
    pattern = r"(\b" + re.escape(instance) + r"\s*\(\s*\n)"
    ports = "".join("        ." + name + " (" + signal + "),\n" for name, signal in connections)
    changed, count = re.subn(pattern, lambda match: match[0] + ports, text)
    require(count == 1, "instance differs: " + instance)
    return changed


def instance_ports(text, name):
    match = re.search(r"\b" + name + r"\s*\((.*?)\n\s*\);", text, re.S)
    require(match is not None, "missing instance: " + name)
    return {key: re.sub(r"\s+", "", value) for key, value in
            re.findall(r"\.(\w+)\s*\(([^()]*|~[^()]*)\)", match[1])}


def reset_functional_state_from_parent(text):
    """Keep each side's parent as its sole asynchronous functional reset.

    The shared five-edge flag then holds the original reset values synchronously.
    Pinned input hashes and the complete block inventory precede every write.
    """
    masked = re.sub(r'//[^\n]*|/\*.*?\*/|"(?:\\.|[^"\\])*"',
                    lambda match: " " * len(match[0]), text, flags=re.S)
    pattern = (r'always\s*@\(\s*posedge\s+(in_clk|out_clk)\s+or\s+negedge\s+'
               r'(internal_in_sclr_n|internal_out_sclr_n)\s*\)\s*begin\s*'
               r'(?P<initial_if>if\s*\(\s*[!~]\s*\2\s*\)\s*)')
    matches = list(re.finditer(pattern, masked))
    require([sum(m[1] == clock for m in matches) for clock in ("in_clk", "out_clk")] == [8, 8],
            "functional reset block inventory differs")
    for match in reversed(matches):
        clock, delayed = match[1], match[2]
        require(delayed == ("internal_in_sclr_n" if clock == "in_clk" else "internal_out_sclr_n"),
                "functional reset clock/polarity differs")
        parent = "in_reset_n" if clock == "in_clk" else "out_reset_n"
        cursor = match.end()
        while masked[cursor].isspace():
            cursor += 1
        if masked[cursor:cursor + 5] == "begin":
            depth, stop = 0, None
            for token in re.finditer(r'\bbegin\b|\bend\b', masked[cursor:]):
                depth += 1 if token[0] == "begin" else -1
                if depth == 0:
                    stop = cursor + token.end()
                    break
            require(stop is not None, "unterminated functional reset body")
        else:
            stop = masked.index(";", cursor) + 1
        reset_body = text[cursor:stop]
        prefix = text[match.start():match.start("initial_if")]
        prefix = re.sub(r'negedge\s+' + delayed, 'negedge ' + parent, prefix, count=1)
        replacement = (prefix + "if (!" + parent + ") " + reset_body + " else " +
                       text[match.start("initial_if"):match.end()])
        text = text[:match.start()] + replacement + text[match.end():]
    require(not re.search(r'negedge\s+internal_(?:in|out)_sclr_n', text),
            "delayed flag still drives an asynchronous functional reset")
    return text


def derive(originals, *, standard=False):
    """Return the eleven derived files from the exact pinned Quartus inputs."""
    require(set(originals) == set(ORIGINAL_SHA256), "generated file inventory differs")
    for name, digest in ORIGINAL_SHA256.items():
        if standard and name == PARENT:
            digest = STANDARD_PARENT_SHA256
        require(sha(originals[name]) == digest, "unreviewed generated input: " + name)
    changed = {}
    for bridge in ("fes_ctrl_cdc", "fes_mem_cdc"):
        directory = "peripheral_subsys/ip/peripheral_subsys/peripheral_subsys_" + bridge + "/"
        leaf = directory + "st_dc_fifo_1953/synth/peripheral_subsys_" + bridge + "_st_dc_fifo_1953_phmrs5y.v"
        text = add_ports(originals[leaf].decode(), FIFO_PORTS)
        require(text.count("    altera_reset_synchronizer #(") == 2, "FIFO reset instance count differs")
        match = re.search(r"    assign reset_merged = \( ~in_reset_n \| ~out_reset_n \) ;.*?"
                          r"    assign internal_out_sclr_n = ~\(internal_out_sclr\);", text, re.S)
        require(match is not None, "FIFO reset-source expression differs")
        require(len(re.findall(r"\.DEPTH\s*\(4\)", match[0])) == 2 and
                len(re.findall(r"\.ASYNC_RESET\s*\(1'b1\)", match[0])) == 2,
                "FIFO reset depth/mode differs")
        text = text[:match.start()] + '''    // The parent shares the original five-edge release pipeline per clock.
    // Its input is the same OR of both active-high bridge resets.
    assign internal_in_sclr = fes_shared_in_reset;
    assign internal_out_sclr = fes_shared_out_reset;
    assign internal_in_sclr_n = ~internal_in_sclr;
    assign internal_out_sclr_n = ~internal_out_sclr;''' + text[match.end():]
        text = reset_functional_state_from_parent(text)
        changed[leaf] = replace_once(text, "    wire reset_merged;", "").encode()
        wrappers = [name for name in originals if name.startswith(directory + "mm_ccb_1941/synth/")
                    and "_mm_ccb_st_dc_fifo_" in name]
        require(len(wrappers) == 2, "specialized FIFO wrapper count differs")
        for name in wrappers:
            original = originals[name].decode()
            roles = re.findall(r"\bmy_altera_avalon_dc_fifo_(cmd|rsp)_fifo\s*\(", original)
            require(len(roles) == 1, "specialized FIFO role differs")
            role = "my_altera_avalon_dc_fifo_" + roles[0] + "_fifo"
            mapped = instance_ports(original, role)
            require(mapped.get("in_reset_n") == "in_reset_n" and mapped.get("out_reset_n") == "out_reset_n",
                    "specialized FIFO reset polarity differs")
            changed[name] = connect(add_ports(original, FIFO_PORTS), role, [(p, p) for p in FIFO_PORTS]).encode()
        mains = [name for name in originals if name.startswith(directory + "mm_ccb_1941/synth/")
                 and "_mm_ccb_1941_" in name]
        require(len(mains) == 1, "bridge implementation count differs")
        name = mains[0]
        original = originals[name].decode()
        text = add_ports(original, BRIDGE_PORTS)
        for role, inside, outside in (("cmd_fifo", "s0", "m0"), ("rsp_fifo", "m0", "s0")):
            mapped = instance_ports(original, role)
            require(mapped.get("in_clk") == inside + "_clk" and mapped.get("out_clk") == outside + "_clk" and
                    mapped.get("in_reset_n") == "~" + inside + "_reset" and
                    mapped.get("out_reset_n") == "~" + outside + "_reset", "command/response clock/reset mapping differs")
            text = connect(text, role, [("fes_shared_in_reset", "fes_shared_" + inside + "_reset"),
                                       ("fes_shared_out_reset", "fes_shared_" + outside + "_reset")])
        changed[name] = text.encode()
        name = directory + "synth/peripheral_subsys_" + bridge + ".v"
        original = originals[name].decode()
        mapped = instance_ports(original, bridge)
        require(mapped.get("s0_reset") == "s0_reset" and mapped.get("m0_reset") == "m0_reset",
                "outer bridge reset mapping differs")
        changed[name] = connect(add_ports(original, BRIDGE_PORTS), bridge, [(p, p) for p in BRIDGE_PORTS]).encode()
    text = originals[PARENT].decode()
    for bridge, s0, m0 in (("fes_ctrl_cdc", "system", "pixel"), ("fes_mem_cdc", "pixel", "system")):
        mapped = instance_ports(text, bridge)
        for role, clock in (("s0", s0), ("m0", m0)):
            expected = "periph_clk_out_clk_clk" if clock == "system" else "fes_pixel_clk_out_clk_clk"
            require(mapped.get(role + "_clk") == expected and mapped.get(role + "_reset") ==
                    "~fes_scanout_" + clock + "_reset_out_reset_reset", "parent bridge clock/reset polarity differs")
        text = connect(text, bridge, [("fes_shared_s0_reset", "fes_shared_" + s0 + "_reset"),
                                     ("fes_shared_m0_reset", "fes_shared_" + m0 + "_reset")])
    _, _, body, _ = port_region(text)
    shared = '''
    // Every FIFO side uses its own existing parent as the sole asynchronous reset.
    // Shared flags retain the original five-edge effective reset release.
    wire fes_shared_system_reset, fes_shared_pixel_reset;
    fes_de25_fifo_reset_delay write_reset_sync (
        .clk(periph_clk_out_clk_clk), .local_reset(~fes_scanout_system_reset_out_reset_reset), .peer_reset(~fes_scanout_pixel_reset_out_reset_reset),
        .reset_out(fes_shared_system_reset));
    fes_de25_fifo_reset_delay read_reset_sync (
        .clk(fes_pixel_clk_out_clk_clk), .local_reset(~fes_scanout_pixel_reset_out_reset_reset), .peer_reset(~fes_scanout_system_reset_out_reset_reset),
        .reset_out(fes_shared_pixel_reset));
'''
    delay = '''
// Only peer reset enters the first data stage. Local parent assertion resets
// all delay flags and functional state together; its release stays timed.
module fes_de25_fifo_reset_delay(input wire clk,local_reset,peer_reset,output wire reset_out);
(* preserve, dont_merge, async_reg="true", altera_attribute="-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *)
reg [4:0] release_pipe=5'b11111;
always @(posedge clk or posedge local_reset) if(local_reset) release_pipe<=5'b11111;
    else release_pipe<={release_pipe[3:0],peer_reset};
assign reset_out=release_pipe[4];
endmodule
'''
    changed[PARENT] = (text[:body] + shared + text[body:] + delay).encode()
    require(set(changed) == set(originals), "derived file inventory differs")
    return changed


def selected_inputs(directory):
    data = (directory / "inputs.json").read_bytes()
    inputs = json.loads(data)
    scanout = inputs["scanout"]
    standard = (scanout.get("build_identity_scheme") == "standard-board-integration-source-profile" and
                inputs.get("standard_zx81", {}).get("scope") == "standard-core-board-integration" and
                inputs["standard_zx81"].get("runtime_admission") == "not-qualified")
    require(scanout.get("shared_fifo_resets") == POLICY and
            (scanout.get("build_identity_scheme") == "canonical-functional-record-v2" or standard) and
            scanout.get("hps_initialization") == "HPS FIRST", "canonical policy selection differs")
    return data


def checked_file(root, name):
    path = root / name
    require(not path.is_symlink() and path.resolve().is_relative_to(root.resolve()), "unsafe generated source path")
    return path


def receipt(inputs, originals, derived):
    return {"policy": POLICY, "inputs_sha256": sha(inputs), "receiving_clocks_hz": [100000000, 74250000],
            "release_edges": 5, "functional_async_reset": "existing local parent only",
            "delayed_reset": "five-edge flag; synchronous functional hold",
            "peer_reset_ingress": "first data stage only; local reset and later stages timed",
            "physical_sharing_acceptance": "separate fitted evidence required",
            "files": {name: {"before_sha256": sha(originals[name]), "after_sha256": sha(derived[name])}
                      for name in sorted(originals)}}


def apply(directory):
    directory = Path(directory)
    inputs = selected_inputs(directory)
    project = directory / "project"
    originals = {name: checked_file(project, name).read_bytes() for name in ORIGINAL_SHA256}
    standard = json.loads(inputs)["scanout"]["build_identity_scheme"] == "standard-board-integration-source-profile"
    derived = derive(originals, standard=standard)
    retained = directory / "shared-reset-originals"
    retained.mkdir(exist_ok=False)
    for name, data in originals.items():
        path = retained / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    for name, data in derived.items():
        checked_file(project, name).write_bytes(data)
    result = receipt(inputs, originals, derived)
    (directory / "shared-resets.json").write_text(json.dumps(result, indent=2, sort_keys=True) + "\n")
    return verify(directory)


def verify(directory):
    directory = Path(directory)
    inputs = selected_inputs(directory)
    retained = directory / "shared-reset-originals"
    require({p.relative_to(retained).as_posix() for p in retained.rglob("*") if p.is_file()} == set(ORIGINAL_SHA256),
            "retained generated source inventory differs")
    originals = {name: checked_file(retained, name).read_bytes() for name in ORIGINAL_SHA256}
    standard = json.loads(inputs)["scanout"]["build_identity_scheme"] == "standard-board-integration-source-profile"
    derived = derive(originals, standard=standard)
    for name, data in derived.items():
        require(checked_file(directory / "project", name).read_bytes() == data, "derived source changed: " + name)
    expected = receipt(inputs, originals, derived)
    require(json.loads((directory / "shared-resets.json").read_text()) == expected, "derivation receipt differs")
    return expected
