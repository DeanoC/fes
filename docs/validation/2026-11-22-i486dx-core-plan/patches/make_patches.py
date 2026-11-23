import subprocess, sys, pathlib
R = pathlib.Path("sources/misteross/cores/fes-common/rtl/i386")
CPU, FB = R/"fes_i386_cpu.sv", R/"fes_i386_fetchbuf.sv"
EDITS = {
 "dx": [(CPU, "    parameter bit I486 = 1'b0\n", "    parameter bit I486 = 1'b1\n")],
 "pred": [(CPU, "if (reset) pred_on <= !queue16;", "if (reset) pred_on <= 1'b0;  // ABLATION pred")],
 "ras": [(CPU, "(wb_s.dbr && (wb_s.taken || wb_s.bm) || wb_s.nret || wb_s.nind) && pred_on && !reset",
               "(wb_s.dbr && (wb_s.taken || wb_s.bm) || wb_s.nind) && pred_on && !reset  /* ABLATION ras */"),
         (CPU, "btb_wr.entry.ret <= wb_s.nret;", "btb_wr.entry.ret <= 1'b0;  // ABLATION ras"),
         (FB, "    assign btb_target = btb_entry.ret ? ras_top : btb_entry.target;\n", "    assign btb_target = btb_entry.target;  // ABLATION ras\n"),
         (FB, "    assign ras_pop = pred_now && btb_entry.ret;\n", "    assign ras_pop = 1'b0;  // ABLATION ras\n")],
 "ind": [(CPU, "(wb_s.dbr && (wb_s.taken || wb_s.bm) || wb_s.nret || wb_s.nind) && pred_on && !reset",
               "(wb_s.dbr && (wb_s.taken || wb_s.bm) || wb_s.nret) && pred_on && !reset  /* ABLATION ind */")],
 "stld": [(CPU, "    assign st_near = late != 2'd0 || !ag_slo_ok || d_xaddr || st_e[11:4] == 8'd0 && st_e[3:0] <= st_span;\n",
                "    assign st_near = 1'b1;  // ABLATION stld: every load behind a store in AG waits\n")],
 "ealu": [(CPU, "    assign dc_q.ealu = q_ealu;\n", "    assign dc_q.ealu = 1'b0;  // ABLATION ealu\n")],
 "lfwd": [(CPU, "        d_out.ldfwd = u.cls == fes_i386_pkg::UOP_ALU && (u.op == 5'(fes_i386_pkg::ALU_MOV) || u.op == 5'(fes_i386_pkg::ALU_MOVZX))\n                   && mkind == fes_i386_pkg::MEM_LOAD && !u.mem.in_a && u.size == fes_i386_pkg::SIZE_32 && !u.dst.hi8;\n",
                "        d_out.ldfwd = 1'b0;  // ABLATION lfwd\n")],
 "pg": [(CPU, "    assign pg = cr0_pg;\n", "    assign pg = 1'b0;  // ABLATION pg: area bound only, not functional\n")],
}
EDITS["speed"] = EDITS["pred"] + EDITS["stld"] + EDITS["ealu"] + EDITS["lfwd"]
out = pathlib.Path(sys.argv[1])
for name, edits in EDITS.items():
    saved = {}
    for f, a, b in edits:
        t = saved.setdefault(f, f.read_text())
        cur = f.read_text()
        assert cur.count(a) == 1, (name, a)
        f.write_text(cur.replace(a, b))
    d = subprocess.run(["git", "diff", "--", *map(str, saved)], capture_output=True, text=True, check=True).stdout
    (out/f"{name}.patch").write_text(d)
    for f, t in saved.items(): f.write_text(t)
    print(name, len(d.splitlines()))
