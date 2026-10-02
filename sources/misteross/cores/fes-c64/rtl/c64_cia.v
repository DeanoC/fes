// SPDX-License-Identifier: GPL-2.0-or-later
// Enough of a 6526 for the C64 pathfinder: ports, both timers and IRQ flags.
// Time of day and the serial shift register read as zero. `we` is one system
// clock. Timers count on `phi` or timer A underflow. CNT is not exposed and is
// held high: external-edge modes do not count, gated timer A mode does.
// Reading ICR clears the flags on `read_sample`,
// which is the same edge the CPU samples the bus, so the read still sees them.
module c64_cia (
    input  wire       clk,
    input  wire       phi,
    input  wire       reset,
    input  wire       cs,
    input  wire       we,
    input  wire       reading,
    input  wire       read_sample,
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
    reg [15:0] ta_latch, ta_count, tb_latch, tb_count;
    reg [7:0] cra, crb;
    reg [4:0] icr_mask, icr_flags;

    wire write_reg = we && cs;
    wire ta_force = write_reg && rs == 4'hE && din[4];
    wire tb_force = write_reg && rs == 4'hF && din[4];
    wire ta_load_hi = write_reg && rs == 4'h5 && !cra[0];
    wire tb_load_hi = write_reg && rs == 4'h7 && !crb[0];
    wire ta_tick = phi && cra[0] && !cra[5] && !ta_force && !ta_load_hi;
    wire ta_under = ta_tick && ta_count == 16'h0000;
    wire tb_tick = crb[0] && !tb_force && !tb_load_hi &&
                   (crb[6] ? ta_under : (phi && !crb[5]));
    wire tb_under = tb_tick && tb_count == 16'h0000;

    assign irq = |(icr_flags & icr_mask);

    always @* begin
        case (rs)
            4'h0: dout = pa_pin;
            4'h1: dout = pb_pin;
            4'h2: dout = pa_ddr;
            4'h3: dout = pb_ddr;
            4'h4: dout = ta_count[7:0];
            4'h5: dout = ta_count[15:8];
            4'h6: dout = tb_count[7:0];
            4'h7: dout = tb_count[15:8];
            4'hD: dout = {irq, 2'b0, icr_flags};
            4'hE: dout = cra;
            4'hF: dout = crb;
            default: dout = 8'h00;
        endcase
    end

    always @(posedge clk) begin
        if (reset) begin
            pa_reg <= 8'h00;
            pb_reg <= 8'h00;
            pa_ddr <= 8'h00;
            pb_ddr <= 8'h00;
            ta_latch <= 16'hffff;
            ta_count <= 16'hffff;
            tb_latch <= 16'hffff;
            tb_count <= 16'hffff;
            cra <= 8'h00;
            crb <= 8'h00;
            icr_mask <= 5'd0;
            icr_flags <= 5'd0;
        end else begin
            if (write_reg) begin
                case (rs)
                    4'h0: pa_reg <= din;
                    4'h1: pb_reg <= din;
                    4'h2: pa_ddr <= din;
                    4'h3: pb_ddr <= din;
                    4'h4: ta_latch[7:0] <= din;
                    4'h5: begin
                        ta_latch[15:8] <= din;
                        if (ta_load_hi) ta_count <= {din, ta_latch[7:0]};
                    end
                    4'h6: tb_latch[7:0] <= din;
                    4'h7: begin
                        tb_latch[15:8] <= din;
                        if (tb_load_hi) tb_count <= {din, tb_latch[7:0]};
                    end
                    4'hD: begin
                        if (din[7]) icr_mask <= icr_mask | din[4:0];
                        else icr_mask <= icr_mask & ~din[4:0];
                    end
                    4'hE: begin
                        cra <= din & 8'hEF;
                        if (din[4]) ta_count <= ta_latch;
                    end
                    4'hF: begin
                        crb <= din & 8'hEF;
                        if (din[4]) tb_count <= tb_latch;
                    end
                    default: ;
                endcase
            end else if (reading && read_sample && rs == 4'hD && cs)
                icr_flags <= 5'd0;
            if (ta_tick) begin
                if (ta_under) begin
                    ta_count <= ta_latch;
                    icr_flags[0] <= 1'b1;
                    if (cra[3]) cra[0] <= 1'b0;
                end else
                    ta_count <= ta_count - 16'd1;
            end
            if (tb_tick) begin
                if (tb_under) begin
                    tb_count <= tb_latch;
                    icr_flags[1] <= 1'b1;
                    if (crb[3]) crb[0] <= 1'b0;
                end else
                    tb_count <= tb_count - 16'd1;
            end
        end
    end
endmodule
