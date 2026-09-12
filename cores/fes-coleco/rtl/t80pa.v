// SPDX-License-Identifier: MIT
// T80pa-compatible wrapper around TV80. Bus timing follows Sorgelig T80pa.vhd:
// CEN_p/CEN_n half-cycles, WAIT via CEN gating, M1 refresh MREQ.
// TV80 one-hot: tstate[1]=T1 .. tstate[4]=T4, mcycle[0]=M1.
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
    parameter Mode = 0;
    parameter T2Write = 1;
    parameter IOWait = 1;

    wire intcycle_n;
    wire no_read;
    wire write;
    wire iorq;
    wire [6:0] mcycle;
    wire [6:0] tstate;
    wire core_busak_n;
    reg [7:0] di_reg;
    reg cen_pol;
    reg [1:0] intcycle_d_n;

    tv80_core #(Mode, IOWait) i_tv80_core (
        .cen(CEN_p & ~cen_pol),
        .m1_n(M1_n),
        .iorq(iorq),
        .no_read(no_read),
        .write(write),
        .rfsh_n(RFSH_n),
        .halt_n(HALT_n),
        .wait_n(1'b1),
        .int_n(INT_n),
        .nmi_n(NMI_n),
        .reset_n(RESET_n),
        .busrq_n(BUSRQ_n),
        .busak_n(core_busak_n),
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

    assign BUSAK_n = core_busak_n;

    always @(posedge CLK or negedge RESET_n) begin
        if (!RESET_n) begin
            RD_n <= 1'b1;
            WR_n <= 1'b1;
            IORQ_n <= 1'b1;
            MREQ_n <= 1'b1;
            di_reg <= 8'h00;
            cen_pol <= 1'b0;
            intcycle_d_n <= 2'b11;
        end else if (CEN_p && !cen_pol) begin
            cen_pol <= 1'b1;
            if (mcycle[0]) begin
                if (tstate[2]) begin
                    IORQ_n <= 1'b1;
                    MREQ_n <= 1'b1;
                    RD_n <= 1'b1;
                end
            end else if (tstate[1] && iorq) begin
                WR_n <= ~write;
                RD_n <= write;
                IORQ_n <= 1'b0;
            end
        end else if (CEN_n && cen_pol) begin
            if (tstate[2])
                cen_pol <= ~WAIT_n;
            else
                cen_pol <= 1'b0;
            if (tstate[3] && core_busak_n)
                di_reg <= DI;
            if (mcycle[0]) begin
                if (tstate[1]) begin
                    intcycle_d_n <= {intcycle_d_n[0], intcycle_n};
                    RD_n <= ~intcycle_n;
                    MREQ_n <= ~intcycle_n;
                    IORQ_n <= intcycle_d_n[1];
                end
                if (tstate[3]) begin
                    intcycle_d_n <= 2'b11;
                    RD_n <= 1'b1;
                    MREQ_n <= 1'b0;
                end
                if (tstate[4])
                    MREQ_n <= 1'b1;
            end else begin
                if (!no_read && !iorq && tstate[1]) begin
                    RD_n <= write;
                    MREQ_n <= 1'b0;
                end
                if (tstate[2])
                    WR_n <= ~write;
                if (tstate[3]) begin
                    WR_n <= 1'b1;
                    RD_n <= 1'b1;
                    IORQ_n <= 1'b1;
                    MREQ_n <= 1'b1;
                end
            end
        end
    end
endmodule
