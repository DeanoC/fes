"""Bind a finite startup model to actual generated parent reset connections.

This adds the parent reset controllers to the generated transport simulation.
It does not qualify a whole reset boundary, physical timing or boot/DMA epochs.
"""
from hashlib import sha256
from pathlib import Path
import re

PHASES = (1, 17, 296, 297, 399, 400, 593, 799)
DELAYS = (0, 1, 2, 3, 7, 31, 127, 4095, 100512)
CASES = [f"startup_phase_{phase}_delay_{delay}" for phase in PHASES for delay in DELAYS]
TOP = "qsys_top/synth/qsys_top.v"
QIP = "qsys_top/qsys_top.qip"
BRIDGES = ("ip/qsys_top/rst_in/synth/rst_in.v",
           "ip/qsys_top/qsys_top_fes_scanout_top_reset/synth/qsys_top_fes_scanout_top_reset.v")
GENERICS = tuple("qsys_top/altera_reset_controller_1925/synth/" + name + ".v"
                 for name in ("altera_reset_controller", "altera_reset_synchronizer"))


def require(condition, message):
    if not condition:
        raise ValueError("generated startup reset model: " + message)


def uncomment(source):
    return re.sub(r"/\*.*?\*/|//[^\n]*", "", source, flags=re.S)


def fields(source):
    pairs = re.findall(r"\.(\w+)\s*\(([^()]*)\)", source)
    require(len(pairs) == len(dict(pairs)), "duplicate parameter/port")
    return {name: value.strip() for name, value in pairs}


class Model:
    def __init__(self, project):
        self.project = Path(project)
        self.inputs = {}
        for name in (TOP, QIP, *BRIDGES, *GENERICS):
            path = (self.project / name).resolve(strict=True)
            path.relative_to(self.project.resolve(strict=True))
            self.inputs[name] = path.read_bytes()
        qip = self.inputs[QIP].decode()
        require(len(re.findall(r'-name VERILOG_FILE \[file join \$::quartus\(qip_path\) "synth/qsys_top\.v"\]', qip)) == 1,
                "parent top is not selected exactly once by its QIP")
        for name in BRIDGES:
            body = uncomment(self.inputs[name].decode())
            module = re.search(r"\bmodule\s+\w+\s*\([^;]+\);\s*(.*?)\s*endmodule", body, re.S)
            require(module is not None and re.sub(r"\s+", "", module[1]) == "assignout_reset_n=in_reset_n;",
                    "parent reset bridge is not the reviewed logical passthrough: " + name)
        top = uncomment(self.inputs[TOP].decode())
        pattern = r"(?ms)^\taltera_reset_controller #\(\n(?P<parameters>.*?)^\t\) (?P<name>rst_controller(?:_\d+)?) \(\n(?P<ports>.*?)^\t\);"
        controllers = list(re.finditer(pattern, top))
        names = ["rst_controller", *("rst_controller_%03d" % n for n in range(1, 5))]
        require([c["name"] for c in controllers] == names, "parent reset controller inventory changed")
        for index, controller in enumerate(controllers):
            parameters = {"NUM_RESET_INPUTS": "1", "OUTPUT_RESET_SYNC_EDGES": '"deassert"' if index < 3 else '"both"',
                          "SYNC_DEPTH": "2", "RESET_REQUEST_PRESENT": "1" if index == 0 else "0",
                          "RESET_REQ_WAIT_TIME": "1", "MIN_RST_ASSERTION_TIME": "3",
                          "RESET_REQ_EARLY_DSRT_TIME": "1", "ADAPT_RESET_REQUEST": "0"}
            parameters.update({"USE_RESET_REQUEST_IN" + str(n): "0" for n in range(16)})
            require(fields(controller["parameters"]) == parameters, "parent controller parameters changed: " + names[index])
            ports = {"reset_in0": "~rst_in_out_reset_reset" if index in (0, 4) else "~fes_scanout_top_reset_out_reset_reset",
                     "clk": "fes_scanout_pixel_clock_out_clk_clk" if index == 1 else "clk_100_out_clk_clk",
                     "reset_out": names[index] + "_reset_out_reset",
                     "reset_req": "rst_controller_reset_out_reset_req" if index == 0 else ""}
            ports.update({"reset_req_in" + str(n): "1'b0" for n in range(16)})
            ports.update({"reset_in" + str(n): "1'b0" for n in range(1, 16)})
            require(fields(controller["ports"]) == ports, "parent controller connections changed: " + names[index])
        self.blocks = "\n".join(controller[0] for controller in controllers)

        def connections(instance):
            blocks = re.findall(r"(?ms)^\t\w+ " + instance + r" \(\n(.*?)^\t\);", top)
            require(len(blocks) == 1, "missing/duplicate parent instance: " + instance)
            return fields(blocks[0])

        periph, memory = connections("subsys_periph"), connections("mm_interconnect_0")
        self.periph = {name: value for name, value in periph.items() if "reset" in name}
        self.memory = {name: value for name, value in memory.items() if "reset" in name}
        require(self.periph == {"reset_reset_n": "rst_in_out_reset_reset",
                "fes_scanout_pixel_reset_reset_n": "~rst_controller_001_reset_out_reset",
                "fes_scanout_system_reset_reset_n": "~rst_controller_002_reset_out_reset"}, "peripheral reset bindings changed")
        require(self.memory == {
            "subsys_periph_fes_scanout_system_reset_reset_bridge_in_reset_reset": "rst_controller_003_reset_out_reset",
            "subsys_periph_fes_scanout_mem_translator_reset_reset_bridge_in_reset_reset": "rst_controller_003_reset_out_reset",
            "subsys_hps_f2sdram_adapter_rst_reset_bridge_in_reset_reset": "rst_controller_004_reset_out_reset",
            "subsys_hps_f2sdram_adapter_axi4_sub_translator_clk_reset_reset_bridge_in_reset_reset": "rst_controller_004_reset_out_reset"},
            "memory translator reset bindings changed")
        require(periph.get("clk_clk") == memory.get("clk_100_out_clk_clk") == "clk_100_out_clk_clk" and
                periph.get("fes_scanout_pixel_clk_clk") == "fes_scanout_pixel_clock_out_clk_clk", "parent clock bindings changed")

    def wrapper(self, source):
        def replace(before, after):
            nonlocal source
            require(source.count(before) == 1, "simulation wrapper connection changed: " + before)
            source = source.replace(before, after)
        before = "input wire clk_mem, clk_pixel, reset_n, clock_fault, display_ready"
        replace(before, before + ", scanout_source_n,\n    output wire control_released, pixel_released, system_released, memory_source_released, memory_target_released")
        for name, value in self.periph.items():
            replace(f" .{name}(reset_n)", f" .{name}({value})")
        for name, value in self.memory.items():
            replace(f" .{name}(~reset_n)", f" .{name}({value})")
        declarations = """
wire clk_100_out_clk_clk = clk_mem;
wire fes_scanout_pixel_clock_out_clk_clk = clk_pixel;
wire rst_in_out_reset_reset = reset_n;
wire fes_scanout_top_reset_out_reset_reset = scanout_source_n;
wire rst_controller_reset_out_reset, rst_controller_reset_out_reset_req;
wire rst_controller_001_reset_out_reset, rst_controller_002_reset_out_reset;
wire rst_controller_003_reset_out_reset, rst_controller_004_reset_out_reset;
assign control_released = ~periph.rst_controller_reset_out_reset;
assign pixel_released = ~rst_controller_001_reset_out_reset;
assign system_released = ~rst_controller_002_reset_out_reset;
assign memory_source_released = ~rst_controller_003_reset_out_reset;
assign memory_target_released = ~rst_controller_004_reset_out_reset;
"""
        replace("endmodule", declarations + self.blocks + "\nendmodule")
        return source

    def metadata(self):
        return {"input_sha256": {name: sha256(data).hexdigest() for name, data in self.inputs.items()},
                "parent_blocks_sha256": sha256(self.blocks.encode()).hexdigest(),
                "peripheral_reset_bindings": self.periph, "memory_reset_bindings": self.memory,
                "release_delay_system_cycles": list(DELAYS),
                "scanout_source": "modeled first_lock_seen; physical PLL/qualifier outside this proof",
                "global_reset_or_boot_epoch_acceptance": False, "timing_acceptance": False}

    def verify(self):
        require(all((self.project / name).read_bytes() == data for name, data in self.inputs.items()),
                "bound parent reset input changed during simulation")
