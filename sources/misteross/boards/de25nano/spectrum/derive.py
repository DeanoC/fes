"""DE25 derivations of the unchanged DE10 ZX Spectrum machine sources."""
import hashlib

ROM_BEFORE = (b"    spectrum_rom #(.ROM_FILE(ROM_FILE)) rom (\n"
              b"        .clk(clk_sys), .address({cpu_map[1:0], cpu_a[13:0]}), .data(rom_data)\n"
              b"    );\n")
ROM_AFTER = (b"    fes_spectrum_firmware_rom rom (\n"
             b"        .clk(clk_sys), .address({cpu_map[1:0], cpu_a[13:0]}), .data(rom_data),\n"
             b"        .write_addr(firmware_write_addr), .write_data(firmware_write_data),\n"
             b"        .write_enable(firmware_write_enable)\n"
             b"    );\n")
PORTS_BEFORE = b"    output wire [15:0] cpu_pc\n);\n"
PORTS_AFTER = (b"    output wire [15:0] cpu_pc,\n"
               b"    // Launch-time firmware (fes.fabric.firmware-upload), written while held.\n"
               b"    input  wire [15:0] firmware_write_addr,\n"
               b"    input  wire [15:0] firmware_write_data,\n"
               b"    input  wire [1:0]  firmware_write_enable\n);\n")
PARAMETER_BEFORE = (b"module spectrum_machine #(\n"
                    b'    parameter ROM_FILE = "build/diagnostics/fes-spectrum/firmware.hex"\n'
                    b") (\n")
PARAMETER_AFTER = b"module spectrum_machine (\n"
# Inferred memories whose Cyclone V block hint is the only family dependence.
RAM_FILES = {"spectrum_ram.v": 1, "spectrum_video.v": 1, "spectrum_tape.v": 1}


def sha(data):
    return hashlib.sha256(data).hexdigest()


def _derive(source, replacements, label):
    result = source
    for before, after, count in replacements:
        if result.count(before) != count:
            raise ValueError(label + " adaptation contract changed")
        result = result.replace(before, after)
    return result, {"source_sha256": sha(source), "derived_sha256": sha(result),
                    "changes": [{"before": a.decode(), "after": b.decode(), "count": n}
                                for a, b, n in replacements]}


def retarget_machine(source):
    """Swap the linked ROM lanes for the uploaded firmware RAM and its write port."""
    return _derive(source, ((PARAMETER_BEFORE, PARAMETER_AFTER, 1), (ROM_BEFORE, ROM_AFTER, 1),
                            (PORTS_BEFORE, PORTS_AFTER, 1)), "Spectrum machine ROM")


def retarget_ram(name, source):
    return _derive(source, ((b'ramstyle = "M10K"', b'ramstyle = "M20K"', RAM_FILES[name]),),
                   "Spectrum " + name)
