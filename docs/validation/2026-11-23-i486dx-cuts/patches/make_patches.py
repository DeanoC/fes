"""Write this record's scratch ablation patches against a clean checkout of main dafabb6db.

Run from that checkout's root: python3 make_patches.py OUTDIR. Every anchor must occur exactly once;
dx.patch and lfwd.patch are copied unchanged from docs/validation/2026-11-22-i486dx-core-plan/patches/.
Measurement only: never merged.
"""
import subprocess, sys, pathlib
R = pathlib.Path("sources/misteross/cores/fes-common/rtl/i386")
L1I, L1D, QA = R/"fes_i386_l1i.sv", R/"fes_i386_l1d.sv", R/"fes_i386_qalign.sv"
MUL, CORE = R/"fes_i386_x87_mul.sv", R/"fes_x87_core.sv"
TB = pathlib.Path("sources/misteross/cores/fes-pc386/sim/machine_tb.cpp")
MUL_OLD = MUL.read_text()
MUL_BODY = MUL_OLD[MUL_OLD.index("    logic [63:0] ar, br;\n"):MUL_OLD.index("    assign done = v[3];\n") + len("    assign done = v[3];\n")]
MUL_SERIAL = """    // ABLATION xshift: a serial shifter in place of the nine-DSP product. The one
    // caller (fes_i386_x87 S_SHIFT) sets b = 1 << sh_pow, one-hot, so p = a << sh_pow:
    // shift a byte while b's low byte is zero, else a bit, until b's bit 0 is set.
    logic [127:0] acc;
    logic [63:0]  bm;
    logic         busy, d;
    always_ff @(posedge clk) begin
        d <= 1'b0;
        if (reset) busy <= 1'b0;
        else if (start) begin acc <= {64'd0, a}; bm <= b; busy <= 1'b1; end
        else if (busy) begin
            if (bm[0] || bm == 64'd0) begin busy <= 1'b0; d <= 1'b1; end
            else if (bm[7:0] == 8'd0) begin acc <= acc << 8; bm <= bm >> 8; end
            else begin acc <= acc << 1; bm <= bm >> 1; end
        end
    end
    assign p = acc;
    assign done = d;
"""
EDITS = {
 "l1i2": [(L1I, "free = rr;\n        for (int w = 3; w >= 0; w--) if (!vf2[w]) free = 2'(w);",
                "free = {1'b0, rr[0]};  // ABLATION l1i2: ways 0-1 only (8 KiB)\n        for (int w = 1; w >= 0; w--) if (!vf2[w]) free = 2'(w);"),
          (L1I, "if (reset) valid[k][w] <= 1'b0;", "if (reset || w >= 2) valid[k][w] <= 1'b0;  // ABLATION l1i2")],
 "l1d2": [(L1D, "free = rr;\n        for (int w = 3; w >= 0; w--) if (!vm2[w]) free = 2'(w);",
                "free = {1'b0, rr[0]};  // ABLATION l1d2: ways 0-1 only (8 KiB)\n        for (int w = 1; w >= 0; w--) if (!vm2[w]) free = 2'(w);"),
          (L1D, "if (reset) valid_bits[k][w] <= 1'b0;", "if (reset || w >= 2) valid_bits[k][w] <= 1'b0;  // ABLATION l1d2")],
 "xshift": [(MUL, MUL_BODY, MUL_SERIAL)],
 "xdisp": [(CORE, "    (* preserve, dont_retime *) request_t dispatch;\n",
                  "    request_t dispatch;  // ABLATION xdisp: no dispatch register\n    assign dispatch = request;\n    assign dispatch_valid = accept;\n"),
           (CORE, "        dispatch <= request;\n", ""),
           (CORE, "            dispatch_valid <= 0;\n", ""),
           (CORE, "            dispatch_valid <= accept;\n", "")],
 "qhalf": [(QA, "    assign issue = !redirect && ok[start] && (!run_here || too_long_run) && out_free;\n    assign consume = !redirect && ok[start] && run_here && !too_long_run;\n",
                "    logic thr;  // ABLATION qhalf: at most one Q advance per two clocks (CPI bound only)\n"
                "    always_ff @(posedge clk) thr <= reset || redirect ? 1'b1 : !(issue || consume);\n"
                "    assign issue = !redirect && thr && ok[start] && (!run_here || too_long_run) && out_free;\n"
                "    assign consume = !redirect && thr && ok[start] && run_here && !too_long_run;\n")],
 "bench-x87": [(TB, "        cpu.queue16 = 0;  // the coherent profile\n",
                    "        cpu.queue16 = 0;  // the coherent profile\n        cpu.x87_present = m.top.x87_present;  // SCRATCH bench-x87: --x87 1 reaches the RTL CPU\n")],
}
EDITS["l12"] = EDITS["l1i2"] + EDITS["l1d2"]
EDITS["x87"] = EDITS["xshift"] + EDITS["xdisp"]
out = pathlib.Path(sys.argv[1])
for name, edits in EDITS.items():
    saved = {}
    for f, a, b in edits:
        saved.setdefault(f, f.read_text())
        cur = f.read_text()
        assert cur.count(a) == 1, (name, a[:80])
        f.write_text(cur.replace(a, b))
    d = subprocess.run(["git", "diff", "--", *map(str, saved)], capture_output=True, text=True, check=True).stdout
    (out/f"{name}.patch").write_text(d)
    for f, t in saved.items(): f.write_text(t)
    print(name, len(d.splitlines()))
# cuts = l12 + x87 + lfwd: written by combining, since lfwd.patch is round 1's.
