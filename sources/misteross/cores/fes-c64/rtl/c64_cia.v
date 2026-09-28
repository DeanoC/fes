// SPDX-License-Identifier: GPL-2.0-or-later
// Enough of a 6526 for the C64 pathfinder: ports, timer A, and the IRQ flag.
// Time of day and the serial shift register read as zero. `we` is one system
// clock. Timer A counts on `phi`.
module c64_cia (
    input  wire       clk,
    input  wire       phi,
    input  wire       reset,
    input  wire       cs,
    input  wire       we,
    input  wire [3:0] rs,
    input  wire [7:0] din,
    output reg  [7:0] dout,
    input  wire [7:0] pa_pin,
    input  wire [7:0] pb_pin,
    output reg  [7:0] pa_reg,
    output reg  [7:0] pb_reg,
    output reg  [7:0] pa_ddr,
    output reg  [7:0] pb_ddr,
    output wire       irq
);
    reg [15:0] ta_latch, ta_count, tb_latch;
    reg [7:0] cra, crb, icr_mask, icr_flags;
    reg ta_under;

    assign irq = |(icr_flags & icr_mask);

    always @* begin
        case (rs)
            4'h0: dout = pa_pin;
            4'h1: dout = pb_pin;
            4'h2: dout = pa_ddr;
            4'h3: dout = pb_ddr;
            4'h4: dout = ta_count[7:0];
            4'h5: dout = ta_count[15:8];
            4'h6: dout = tb_latch[7:0];
            4'h7: dout = tb_latch[15:8];
            4'hD: dout = {irq, 7'b0} | icr_flags;
            4'hE: dout = cra;
            4'hF: dout = crb;
            default: dout = 8'h00;
        endcase
    end

    always @(posedge clk) begin
        ta_under <= 1'b0;
        if (reset) begin
            pa_reg <= 8'h00;
            pb_reg <= 8'h00;
            pa_ddr <= 8'h00;
            pb_ddr <= 8'h00;
            ta_latch <= 16'hffff;
            ta_count <= 16'hffff;
            tb_latch <= 16'hffff;
            cra <= 8'h00;
            crb <= 8'h00;
            icr_mask <= 8'h00;
            icr_flags <= 8'h00;
        end else begin
            if (we && cs) begin
                case (rs)
                    4'h0: pa_reg <= din;
                    4'h1: pb_reg <= din;
                    4'h2: pa_ddr <= din;
                    4'h3: pb_ddr <= din;
                    4'h4: ta_latch[7:0] <= din;
                    4'h5: ta_latch[15:8] <= din;
                    4'h6: tb_latch[7:0] <= din;
                    4'h7: tb_latch[15:8] <= din;
                    4'hD: begin
                        if (din[7]) icr_mask <= icr_mask | din[6:0];
                        else icr_mask <= icr_mask & ~din[6:0];
                    end
                    4'hE: begin
                        cra <= din;
                        if (din[4]) ta_count <= ta_latch;
                    end
                    4'hF: crb <= din;
                    default: ;
                endcase
            end else if (rs == 4'hD && cs)
                icr_flags <= 8'h00;
            if (phi && cra[0]) begin
                if (ta_count == 16'h0000) begin
                    ta_count <= ta_latch;
                    ta_under <= 1'b1;
                    icr_flags[0] <= 1'b1;
                    if (cra[3]) cra[0] <= 1'b0;
                end else
                    ta_count <= ta_count - 16'd1;
            end
        end
    end
endmodule
