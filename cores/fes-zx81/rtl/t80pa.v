// SPDX-License-Identifier: MIT
// T80pa-compatible wrapper around TV80. CEN_p is the Z80 T-state enable;
// CEN_n is unused (T80pa half-cycles are not modelled).
// TV80: https://github.com/hutch31/tv80 commit 66a131c38d05ef58b3d8c4f1507a72e6e4aa5d65

`define TV80DELAY

module T80pa (
    input  wire        RESET_n,
    input  wire        CLK,
    input  wire        CEN_p,
    input  wire        CEN_n,
    input  wire        WAIT_n,
    input  wire        INT_n,
    input  wire        NMI_n,
    input  wire        BUSRQ_n,
    output wire        M1_n,
    output reg         MREQ_n,
    output reg         IORQ_n,
    output reg         RD_n,
    output reg         WR_n,
    output wire        RFSH_n,
    output wire        HALT_n,
    output wire        BUSAK_n,
    output wire [15:0] A,
    input  wire [7:0]  DI,
    output wire [7:0]  DO
);
    /* verilator lint_off UNUSEDSIGNAL */
    wire unused_cen_n = CEN_n;
    /* verilator lint_on UNUSEDSIGNAL */

    parameter Mode = 0;
    parameter T2Write = 1;
    parameter IOWait = 1;

    wire intcycle_n;
    wire no_read;
    wire write;
    wire iorq;
    wire [6:0] mcycle;
    wire [6:0] tstate;
    reg [7:0] di_reg;

    tv80_core #(Mode, IOWait) i_tv80_core (
        .cen(CEN_p),
        .m1_n(M1_n),
        .iorq(iorq),
        .no_read(no_read),
        .write(write),
        .rfsh_n(RFSH_n),
        .halt_n(HALT_n),
        .wait_n(WAIT_n),
        .int_n(INT_n),
        .nmi_n(NMI_n),
        .reset_n(RESET_n),
        .busrq_n(BUSRQ_n),
        .busak_n(BUSAK_n),
        .clk(CLK),
        .IntE(),
        .stop(),
        .A(A),
        .dinst(DI),
        .di(di_reg),
        .dout(DO),
        .mc(mcycle),
        .ts(tstate),
        .intcycle_n(intcycle_n)
    );

    always @(posedge CLK or negedge RESET_n) begin
        if (!RESET_n) begin
            RD_n   <= 1'b1;
            WR_n   <= 1'b1;
            IORQ_n <= 1'b1;
            MREQ_n <= 1'b1;
            di_reg <= 8'h00;
        end else if (CEN_p) begin
            RD_n   <= 1'b1;
            WR_n   <= 1'b1;
            IORQ_n <= 1'b1;
            MREQ_n <= 1'b1;
            if (mcycle[0]) begin
                if (tstate[1] || (tstate[2] && WAIT_n == 1'b0)) begin
                    RD_n   <= ~intcycle_n;
                    MREQ_n <= ~intcycle_n;
                    IORQ_n <= intcycle_n;
                end
            end else begin
                if ((tstate[1] || (tstate[2] && WAIT_n == 1'b0)) && no_read == 1'b0 && write == 1'b0) begin
                    RD_n   <= 1'b0;
                    IORQ_n <= ~iorq;
                    MREQ_n <= iorq;
                end
                if (T2Write != 0) begin
                    if ((tstate[1] || (tstate[2] && WAIT_n == 1'b0)) && write == 1'b1) begin
                        WR_n   <= 1'b0;
                        IORQ_n <= ~iorq;
                        MREQ_n <= iorq;
                    end
                end else if (tstate[2] && write == 1'b1) begin
                    WR_n   <= 1'b0;
                    IORQ_n <= ~iorq;
                    MREQ_n <= iorq;
                end
            end
            if (tstate[2] && WAIT_n == 1'b1 && !write && !no_read)
                di_reg <= DI;
        end
    end
endmodule
