#!/usr/bin/env python3
"""Check tag propagation and layout against a C28 cores/fes-common/rtl/i386 directory."""
import argparse
from pathlib import Path
import tempfile

from retire_source import instrument_retire


def check(i386):
    files = [i386 / 'fes_i386_pkg.sv', i386 / 'fes_i386_cpu.sv']
    original = [path.read_bytes() for path in files]
    with tempfile.TemporaryDirectory() as temporary:
        pkg, cpu = instrument_retire(files, temporary)
        assert pkg != files[0] and cpu != files[1]
        assert [p.read_bytes() for p in files] == original
        package = pkg.read_text()
        # Strip only the new high members and compare every original packed
        # bit with the witness. stage.valid stays at MSB for cpu_profile.h.
        assert 'logic        valid;\n        logic [33:0] p5_tag;\n        logic [31:0] p5_linear;\n        uop_t' in package
        stripped = package.replace('        logic [33:0] p5_tag;\n', '').replace('        logic [31:0] p5_linear;\n', '')
        assert stripped.encode() == original[0]

        # These are the actual copy/hold points, including the decode skid
        # and dc0, the whole-packet capture() helper and the WB EOI gate.
        code = cpu.read_text()
        for line in (
            "assign dc_q.p5_tag = {1'b1, code32, q.prefix_count, q.length,",
            'q.bytes[2], q.bytes[1], q.bytes[0]};',
            "dc_exc = '0;",
            'if (exc_inject) dc_skid <= dc_exc;',
            'else if (!dc_skid_valid) dc_skid <= dc_q;',
            'if (dc_load) dc0 <= dc_skid_valid ? dc_skid : dc_q;',
            'assign dc_load = !dc_valid || (issue && !dc_more);',
            'd_out.p5_tag = dc0.p5_tag;',
            'd_out.p5_linear = dc0.lin;',
            'ag_s <= d_out;',
            'function automatic pipe_t capture(input pipe_t s',
            't = s;',
            'capture = t;',
            "ag_out = capture(ag_s, 3'd4, wb_s.result);",
            'm1_s <= ag_out;',
            "m1_out = capture(m1_s, 3'd3, wb_s.result);",
            'm2_s <= m1_out;',
            "m2_cap = capture(m2_s, 3'd2, wb_s.result);",
            'm2_out = m2_cap;',
            'ex_s <= m2_out;',
            'ex_out = ex_s;',
            'wb_s <= ex_out;',
            'trace_retire <= wb_eoi;\n                if (wb_eoi) begin\n                    p5_retire_tag <= wb_s.p5_tag;',
            'p5_retire_linear <= wb_s.p5_linear;',
        ):
            assert line in code, f'C28 pipeline no longer contains {line!r}'
        try:
            instrument_retire((pkg, cpu), temporary)
        except ValueError:
            pass
        else:
            raise AssertionError('instrumentation accepted an already instrumented source')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('i386', type=Path)
    check(parser.parse_args().i386)
