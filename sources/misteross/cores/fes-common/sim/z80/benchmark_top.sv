// SPDX-License-Identifier: GPL-2.0-or-later
// Contained Cyclone V timing envelope, not firmware or an ISA test. Dynamic
// transaction data and observation registers keep the complete CPU datapath.
// Only clock, KEY0 and LED0 reach physical pads; no FPGA image is published.
module benchmark_top #(
    parameter bit NMOS = 1'b0
) (
    input wire FPGA_CLK1_50,
    input wire KEY0_N,
    output logic LED0
);
    wire clk = FPGA_CLK1_50;
    logic key_meta = 1'b1, key_reset = 1'b1;
    logic [31:0] stimulus = 32'h675a12e9;
    logic [11:0] restart_count = 0;
    logic phase = 0;
    wire reset = key_reset || restart_count == 0;
    wire int_n = |stimulus[11:8];
    wire nmi_n = |stimulus[15:12];
    wire [15:0] debug_pc, debug_sp, debug_af, debug_bc, debug_de, debug_hl;
    wire [15:0] debug_ix, debug_iy, debug_ir, retire_pc, observed_address;
    wire [15:0] observed_refresh, observed_control;
    wire [4:0] observed_delay;
    wire [7:0] observed_data;
    wire [2:0] debug_iff;
    wire illegal, retired;

    always @(posedge clk) begin
        key_meta <= !KEY0_N;
        key_reset <= key_meta;
        if (key_reset) begin
            stimulus <= 32'h675a12e9;
            restart_count <= 0;
            phase <= 0;
        end else begin
            // Fibonacci x^32 + x^22 + x^2 + x + 1 sequence.
            stimulus <= {stimulus[30:0], stimulus[31] ^ stimulus[21] ^ stimulus[1] ^ stimulus[0]};
            restart_count <= restart_count + 12'd1;
            phase <= !phase;
        end
    end

    generate if (NMOS) begin : faithful
        wire m1_n, mreq_n, iorq_n, rd_n, wr_n, rfsh_n, halt_n, busak_n;
        fes_z80_nmos cpu (
            .clk(clk), .reset(reset), .ce_p(phase), .ce_n(!phase),
            .wait_n(|stimulus[18:16]), .int_n(int_n), .nmi_n(nmi_n),
            .busrq_n(|stimulus[23:19]), .din(stimulus[7:0]),
            .m1_n(m1_n), .mreq_n(mreq_n), .iorq_n(iorq_n), .rd_n(rd_n),
            .wr_n(wr_n), .rfsh_n(rfsh_n), .halt_n(halt_n), .busak_n(busak_n),
            .a(observed_address), .dout(observed_data), .illegal(illegal),
            .retired(retired), .retire_pc(retire_pc), .debug_pc(debug_pc),
            .debug_sp(debug_sp), .debug_af(debug_af), .debug_bc(debug_bc),
            .debug_de(debug_de), .debug_hl(debug_hl), .debug_ix(debug_ix),
            .debug_iy(debug_iy), .debug_ir(debug_ir), .debug_iff(debug_iff)
        );
        assign observed_refresh = debug_ir;
        assign observed_delay = 0;
        assign observed_control = {3'b0, m1_n, mreq_n, iorq_n, rd_n, wr_n,
                                   rfsh_n, halt_n, busak_n, illegal, retired, debug_iff};
    end else begin : documented
        wire req, halted;
        wire [2:0] kind;
        wire [3:0] extra_t;
        fes_z80_fast cpu (
            .clk(clk), .reset(reset), .enable(|stimulus[26:25]),
            .int_n(int_n), .nmi_n(nmi_n), .bus_ready(|stimulus[18:16]),
            .bus_rdata(stimulus[7:0]), .bus_req(req), .bus_kind(kind),
            .bus_extra(extra_t), .bus_delay(observed_delay), .bus_addr(observed_address),
            .bus_wdata(observed_data), .refresh_addr(observed_refresh),
            .halted(halted), .illegal(illegal), .retired(retired),
            .retire_pc(retire_pc), .debug_pc(debug_pc), .debug_sp(debug_sp),
            .debug_af(debug_af), .debug_bc(debug_bc), .debug_de(debug_de),
            .debug_hl(debug_hl), .debug_ix(debug_ix), .debug_iy(debug_iy),
            .debug_ir(debug_ir), .debug_iff(debug_iff)
        );
        assign observed_control = {2'b0, halted, req, kind, extra_t, illegal, retired, debug_iff};
    end endgenerate

    // Each signature observes at most two 16-bit values per cycle. The LED
    // reduction is pipelined so an artificial wide XOR is not the timed CPU
    // path. These registers do not feed the CPU or change its bus contract.
    logic [15:0] signature [0:7];
    logic [15:0] fold_low, fold_high;
    logic [7:0] fold_byte;
    integer index;
    always @(posedge clk) begin
        if (key_reset) begin
            for (index = 0; index < 8; index = index + 1) signature[index] <= 0;
            fold_low <= 0;
            fold_high <= 0;
            fold_byte <= 0;
            LED0 <= 0;
        end else begin
            signature[0] <= {signature[0][14:0],signature[0][15]} ^ debug_pc ^ debug_sp;
            signature[1] <= {signature[1][14:0],signature[1][15]} ^ debug_af ^ debug_bc;
            signature[2] <= {signature[2][14:0],signature[2][15]} ^ debug_de ^ debug_hl;
            signature[3] <= {signature[3][14:0],signature[3][15]} ^ debug_ix ^ debug_iy;
            signature[4] <= {signature[4][14:0],signature[4][15]} ^ debug_ir ^ retire_pc;
            signature[5] <= {signature[5][14:0],signature[5][15]} ^ observed_address ^ observed_refresh;
            signature[6] <= {signature[6][14:0],signature[6][15]} ^ {observed_data,stimulus[7:0]};
            signature[7] <= {signature[7][14:0],signature[7][15]} ^ observed_control ^ {11'b0,observed_delay};
            fold_low <= signature[0] ^ signature[1] ^ signature[2] ^ signature[3];
            fold_high <= signature[4] ^ signature[5] ^ signature[6] ^ signature[7];
            fold_byte <= fold_low[7:0] ^ fold_low[15:8] ^ fold_high[7:0] ^ fold_high[15:8];
            LED0 <= ^fold_byte;
        end
    end
endmodule
