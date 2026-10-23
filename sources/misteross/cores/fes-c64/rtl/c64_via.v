// SPDX-License-Identifier: GPL-2.0-or-later
// 6522 port/timer/handshake slice for the 1541. phi is a one-clock enable;
// access strobes a single CPU transaction independently of the timer clock.
// Serial shifting and NMOS subcycle pin timing are not implemented yet.
module c64_via (
    input wire clk, reset, phi, access, cs, we,
    input wire [3:0] rs,
    input wire [7:0] din,
    output reg [7:0] dout,
    input wire [7:0] pa_pin, pb_pin,
    input wire ca1, ca2, cb1, cb2,
    output reg [7:0] pa_reg, pb_reg, pa_ddr, pb_ddr,
    output wire [7:0] pb_output,
    output wire ca2_output, cb2_output, ca2_oe, cb2_oe,
    output wire irq
);
    reg [7:0] acr, pcr, sr, ira, irb;
    reg ira_valid, irb_valid;
    reg [6:0] flags, mask;
    reg [15:0] t1_latch, t1_count, t2_count;
    reg [7:0] t2_low;
    reg t1_delay, t1_reload, t1_armed, t2_delay, t2_armed, t1_pb7;
    reg ca1_old, ca2_old, cb1_old, cb2_old, pb6_old;
    reg ca2_handshake, cb2_handshake, ca2_pulse, cb2_pulse;
    wire transaction = access && cs;
    wire write_reg = transaction && we;
    wire read_reg = transaction && !we;
    wire pa_access = transaction && rs == 4'h1;
    wire pb_access = transaction && rs == 4'h0;
    wire [2:0] ca2_mode = pcr[3:1], cb2_mode = pcr[7:5];
    wire ca1_edge = ca1 != ca1_old && ca1 == pcr[0];
    wire cb1_edge = cb1 != cb1_old && cb1 == pcr[4];
    wire ca2_edge = !ca2_mode[2] && ca2 != ca2_old && ca2 == ca2_mode[1];
    wire cb2_edge = !cb2_mode[2] && cb2 != cb2_old && cb2 == cb2_mode[1];
    wire t1_load = write_reg && rs == 4'h5;
    wire t2_load = write_reg && rs == 4'h9;
    wire t1_under = phi && !t1_delay && !t1_reload && t1_count == 0 && !t1_load;
    wire t2_tick = phi && !t2_delay && (!acr[5] || (pb6_old && !pb_pin[6]));
    wire t2_under = t2_tick && t2_count == 0 && t2_armed && !t2_load;
    reg [6:0] clear_flags, set_flags;

    assign irq = |(flags & mask);
    assign pb_output = {acr[7] ? t1_pb7 : pb_reg[7], pb_reg[6:0]};
    assign ca2_oe = ca2_mode[2];
    assign cb2_oe = cb2_mode[2];
    assign ca2_output = ca2_mode == 3'd6 ? 1'b0 :
                        ca2_mode == 3'd7 ? 1'b1 : ca2_handshake;
    assign cb2_output = cb2_mode == 3'd6 ? 1'b0 :
                        cb2_mode == 3'd7 ? 1'b1 : cb2_handshake;

    always @* begin
        case (rs)
            4'h0: dout = ((acr[1] && irb_valid ? irb : pb_pin) & ~pb_ddr) |
                          (pb_output & pb_ddr);
            4'h1, 4'hf: dout = acr[0] && ira_valid ? ira : pa_pin;
            4'h2: dout = pb_ddr;
            4'h3: dout = pa_ddr;
            4'h4: dout = t1_count[7:0];
            4'h5: dout = t1_count[15:8];
            4'h6: dout = t1_latch[7:0];
            4'h7: dout = t1_latch[15:8];
            4'h8: dout = t2_count[7:0];
            4'h9: dout = t2_count[15:8];
            4'ha: dout = sr;
            4'hb: dout = acr;
            4'hc: dout = pcr;
            4'hd: dout = {irq, flags};
            4'he: dout = {1'b1, mask};
        endcase
        clear_flags = 0;
        if (pa_access) begin
            clear_flags[1] = 1;
            clear_flags[0] = !ca2_mode[0];
        end
        if (pb_access) begin
            clear_flags[4] = 1;
            clear_flags[3] = !cb2_mode[0];
        end
        if ((read_reg && rs == 4'h4) || (write_reg && (rs == 4'h5 || rs == 4'h7)))
            clear_flags[6] = 1;
        if ((read_reg && rs == 4'h8) || t2_load) clear_flags[5] = 1;
        if (transaction && rs == 4'ha) clear_flags[2] = 1;
        if (write_reg && rs == 4'hd) clear_flags = clear_flags | din[6:0];
        set_flags = {t1_under && (t1_armed || acr[6]), t2_under,
                     cb1_edge, cb2_edge, 1'b0, ca1_edge, ca2_edge};
    end

    always @(posedge clk) begin
        if (reset) begin
            pa_reg <= 0; pb_reg <= 0; pa_ddr <= 0; pb_ddr <= 0;
            acr <= 0; pcr <= 0; sr <= 0; flags <= 0; mask <= 0;
            ira <= 0; irb <= 0; ira_valid <= 0; irb_valid <= 0;
            t1_latch <= 16'hffff; t1_count <= 16'hffff;
            t2_low <= 8'hff; t2_count <= 16'hffff;
            t1_armed <= 0; t2_armed <= 0; t1_delay <= 0; t2_delay <= 0;
            t1_reload <= 0; t1_pb7 <= 1;
            ca1_old <= ca1; ca2_old <= ca2; cb1_old <= cb1; cb2_old <= cb2;
            pb6_old <= pb_pin[6];
            ca2_handshake <= 1; cb2_handshake <= 1; ca2_pulse <= 0; cb2_pulse <= 0;
        end else begin
            ca1_old <= ca1; ca2_old <= ca2; cb1_old <= cb1; cb2_old <= cb2;
            flags <= (flags & ~clear_flags) | set_flags;
            if (pa_access && !we) ira_valid <= 0;
            if (pb_access && !we) irb_valid <= 0;
            if (ca1_edge) begin ira <= pa_pin; ira_valid <= 1; end
            if (cb1_edge) begin irb <= pb_pin; irb_valid <= 1; end
            if (phi) begin
                pb6_old <= pb_pin[6];
                if (ca2_pulse) begin ca2_handshake <= 1; ca2_pulse <= 0; end
                if (cb2_pulse) begin cb2_handshake <= 1; cb2_pulse <= 0; end
                if (t1_delay) t1_delay <= 0;
                else if (t1_reload) begin t1_count <= t1_latch; t1_reload <= 0; end
                else begin
                    t1_count <= t1_count - 16'd1;
                    if (t1_under) begin
                        // The counter shows FFFF for a cycle, then reloads.
                        t1_reload <= 1;
                        if (t1_armed || acr[6]) begin
                            t1_pb7 <= acr[6] ? ~t1_pb7 : 1'b1;
                            t1_armed <= 0;
                        end
                    end
                end
                if (t2_delay) t2_delay <= 0;
                else if (t2_tick) begin
                    t2_count <= t2_count - 16'd1;
                    if (t2_under) t2_armed <= 0;
                end
            end
            if (ca1_edge && ca2_mode == 3'd4) ca2_handshake <= 1;
            if (cb1_edge && cb2_mode == 3'd4) cb2_handshake <= 1;
            if (pa_access && (ca2_mode == 3'd4 || ca2_mode == 3'd5)) begin
                ca2_handshake <= 0; ca2_pulse <= ca2_mode == 3'd5;
            end
            if (pb_access && we && (cb2_mode == 3'd4 || cb2_mode == 3'd5)) begin
                cb2_handshake <= 0; cb2_pulse <= cb2_mode == 3'd5;
            end
            if (write_reg) case (rs)
                4'h0: pb_reg <= din;
                4'h1, 4'hf: pa_reg <= din;
                4'h2: pb_ddr <= din;
                4'h3: pa_ddr <= din;
                4'h4, 4'h6: t1_latch[7:0] <= din;
                4'h5: begin
                    t1_latch[15:8] <= din; t1_count <= {din, t1_latch[7:0]};
                    t1_delay <= 1; t1_reload <= 0; t1_armed <= 1; t1_pb7 <= 0;
                end
                4'h7: t1_latch[15:8] <= din;
                4'h8: t2_low <= din;
                4'h9: begin t2_count <= {din, t2_low}; t2_delay <= 1; t2_armed <= 1; end
                4'ha: sr <= din;
                4'hb: acr <= din;
                4'hc: begin
                    pcr <= din; ca2_handshake <= 1; cb2_handshake <= 1;
                    ca2_pulse <= 0; cb2_pulse <= 0;
                end
                4'he: mask <= din[7] ? mask | din[6:0] : mask & ~din[6:0];
                default: begin end
            endcase
        end
    end
endmodule
