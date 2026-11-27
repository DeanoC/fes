"""Carry Q's classification bytes to the C28 simulation's actual WB retirement."""
from pathlib import Path


def insert_once(source, anchor, replacement):
    if source.count(anchor) != 1:
        raise ValueError(f'expected exactly one C28 RTL anchor: {anchor!r}')
    return source.replace(anchor, replacement)


def instrument_retire(rtl, derived):
    """Write two simulation-only files alongside, but distinct from, the board derivation."""
    derived = Path(derived) / 'p5_retire'
    derived.mkdir(exist_ok=True, parents=True)
    replacements = {}
    for name, transform in (('fes_i386_pkg.sv', package), ('fes_i386_cpu.sv', processor)):
        matches = [path for path in rtl if Path(path).name == name]
        if len(matches) != 1:
            raise ValueError(f'expected one simulation input for {name}, found {len(matches)}')
        original = matches[0]
        target = derived / name
        target.write_text(transform(Path(original).read_text()))
        replacements[original] = target
    return tuple(replacements.get(path, path) for path in rtl)


def package(source):
    # All bit positions that existing profilers read remain intact. The CPU
    # profile tests stage.valid via the generated packed width (MSB).
    decoded = '    typedef struct packed {\n        gov_t        gov;\n        uop_t        uop;'
    source = insert_once(source, decoded,
                         decoded.replace('        gov_t', '        logic [33:0] p5_tag;\n        gov_t'))
    pipe = '    typedef struct packed {\n        logic        valid;\n        uop_t        uop;\n        gov_t        gov;'
    return insert_once(source, pipe, pipe.replace('        uop_t', '''        logic [33:0] p5_tag;
        logic [31:0] p5_linear;
        uop_t'''))


def processor(source):
    # q.bytes starts at the opcode. Prefixes have already been stripped by
    # the frontend. Our whitelist uses the first three bytes and the length,
    # never a displacement or immediate value. Capture mode on decode.
    anchor = '    assign dc_q.length = q.length;'
    source = insert_once(source, anchor, anchor + '''
    assign dc_q.p5_tag = {1'b1, code32, q.prefix_count, q.length,
                          q.bytes[2], q.bytes[1], q.bytes[0]};''')

    # dc0 is the ORIGINAL decoded instruction for the whole microsequence.
    # It follows the actual Q/skid acceptance, holds on blocked issue, and
    # is zero-tagged for a synthetic decode such as exception delivery.
    anchor = '        d_out.valid = issue;'
    source = insert_once(source, anchor, anchor + '''
        d_out.p5_tag = dc0.p5_tag;
        d_out.p5_linear = dc0.lin;''')

    anchor = "    always_ff @(posedge clk) begin\n        trace_retire <= 1'b0;"
    source = insert_once(source, anchor, '''    logic [33:0] p5_retire_tag /* verilator public_flat_rd */;
    logic [31:0] p5_retire_linear /* verilator public_flat_rd */;
''' + anchor)
    anchor = '                trace_retire <= wb_eoi;'
    return insert_once(source, anchor, anchor + '''
                if (wb_eoi) begin
                    p5_retire_tag <= wb_s.p5_tag;
                    p5_retire_linear <= wb_s.p5_linear;
                end''')
