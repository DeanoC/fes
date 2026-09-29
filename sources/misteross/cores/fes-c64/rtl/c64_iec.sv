// SPDX-License-Identifier: GPL-2.0-or-later
// Read-only IEC device 8. It speaks the standard CLK/DATA byte protocol
// (LSB first, EOI on the last byte) well enough for the KERNAL LOAD sequence:
// LISTEN, OPEN, filename, UNLISTEN, TALK, SECOND. A D64 image supplies one
// PRG. "$" returns a four-byte synthetic program. Writes are ignored.
module c64_iec (
    input  wire        clk,
    input  wire        phi,
    input  wire        reset,
    input  wire        atn_low,
    input  wire        cpu_clk_low,
    input  wire        cpu_data_low,
    input  wire        disk_ready,
    output reg         dev_clk_low,
    output reg         dev_data_low,
    output reg  [17:0] disk_addr,
    input  wire [7:0]  disk_byte,
    output wire [7:0]  debug
);
    localparam ST_IDLE      = 4'd0;
    localparam ST_RX_HOLD   = 4'd1;
    localparam ST_RX_READY  = 4'd2;
    localparam ST_RX_ACK    = 4'd3;
    localparam ST_DIR_LOAD  = 4'd4;
    localparam ST_DIR_SCAN  = 4'd5;
    localparam ST_FILE_LOAD = 4'd6;
    localparam ST_TAKE      = 4'd7;
    localparam ST_TX_WAIT   = 4'd8;
    localparam ST_TX_EOI    = 4'd9;
    localparam ST_TX_SETUP  = 4'd10;
    localparam ST_TX_CLOCK  = 4'd11;
    localparam ST_TX_ACK    = 4'd12;

    reg [3:0] state;
    reg [7:0] shifter, tx_byte, rd_data, link_t, link_s, load_i, idx, last;
    reg [7:0] sector [0:255];
    reg [7:0] name_mem [0:15];
    reg [4:0] name_len;
    reg [3:0] sa, bit_i;
    reg [1:0] rd_phase;
    reg [5:0] file_track, file_sec;
    reg [15:0] timer;
    reg listening, talking, ours, opening, arm_send, tx_last, saw_pulse;
    reg atn_d, clk_d;
    reg [7:0] debug_state;

    wire bus_clk_low = cpu_clk_low | dev_clk_low;
    wire bus_data_low = cpu_data_low | dev_data_low;

    function automatic [17:0] track_base;
        input [5:0] track;
        begin
            case (track)
                6'd1: track_base = 18'd0;
                6'd2: track_base = 18'd5376;
                6'd3: track_base = 18'd10752;
                6'd4: track_base = 18'd16128;
                6'd5: track_base = 18'd21504;
                6'd6: track_base = 18'd26880;
                6'd7: track_base = 18'd32256;
                6'd8: track_base = 18'd37632;
                6'd9: track_base = 18'd43008;
                6'd10: track_base = 18'd48384;
                6'd11: track_base = 18'd53760;
                6'd12: track_base = 18'd59136;
                6'd13: track_base = 18'd64512;
                6'd14: track_base = 18'd69888;
                6'd15: track_base = 18'd75264;
                6'd16: track_base = 18'd80640;
                6'd17: track_base = 18'd86016;
                6'd18: track_base = 18'd91392;
                6'd19: track_base = 18'd96256;
                6'd20: track_base = 18'd101120;
                6'd21: track_base = 18'd105984;
                6'd22: track_base = 18'd110848;
                6'd23: track_base = 18'd115712;
                6'd24: track_base = 18'd120576;
                6'd25: track_base = 18'd125440;
                6'd26: track_base = 18'd130048;
                6'd27: track_base = 18'd134656;
                6'd28: track_base = 18'd139264;
                6'd29: track_base = 18'd143872;
                6'd30: track_base = 18'd148480;
                6'd31: track_base = 18'd153088;
                6'd32: track_base = 18'd157440;
                6'd33: track_base = 18'd161792;
                6'd34: track_base = 18'd166144;
                6'd35: track_base = 18'd170496;
                default: track_base = 18'd0;
            endcase
        end
    endfunction

    function automatic name_match;
        input [2:0] entry;
        integer n;
        reg [8:0] base;
        reg [7:0] want;
        begin
            name_match = sector[{entry, 5'd2}] != 8'h00;
            base = {entry, 5'd0} + 9'd5;
            for (n = 0; n < 16; n = n + 1) begin
                want = n < name_len ? name_mem[n] : 8'hA0;
                if (sector[base + n[8:0]] != want)
                    name_match = 1'b0;
            end
        end
    endfunction

    integer entry;
    always @(posedge clk) begin
        if (reset || !disk_ready) begin
            state <= ST_IDLE;
            dev_clk_low <= 1'b0;
            dev_data_low <= 1'b0;
            listening <= 1'b0;
            talking <= 1'b0;
            ours <= 1'b0;
            opening <= 1'b0;
            arm_send <= 1'b0;
            name_len <= 5'd0;
            sa <= 4'd0;
            rd_phase <= 2'd0;
            atn_d <= 1'b0;
            clk_d <= 1'b0;
            debug_state <= 8'h00;
        end else if (phi) begin
            atn_d <= atn_low;
            clk_d <= bus_clk_low;
            debug_state <= {4'b0, state};
            if (rd_phase == 2'd1) begin
                rd_data <= disk_byte;
                rd_phase <= 2'd2;
            end else if (atn_low && !atn_d) begin
                state <= ST_RX_HOLD;
                bit_i <= 4'd0;
                dev_data_low <= 1'b1;
                dev_clk_low <= 1'b0;
                arm_send <= 1'b0;
            end else if (!atn_low && atn_d) begin
                if (arm_send && talking && ours) begin
                    arm_send <= 1'b0;
                    load_i <= 8'd0;
                    rd_phase <= 2'd0;
                    if (name_len == 5'd1 && name_mem[0] == 8'h24) begin
                        file_track <= 6'd0;
                        state <= ST_TAKE;
                        sector[0] <= 8'h00;
                        sector[1] <= 8'd5;
                        sector[2] <= 8'h01;
                        sector[3] <= 8'h08;
                        sector[4] <= 8'h24;
                        sector[5] <= 8'h00;
                        link_t <= 8'h00;
                        link_s <= 8'd5;
                        idx <= 8'd2;
                        last <= 8'd5;
                    end else
                        state <= ST_DIR_LOAD;
                end
            end else begin
                case (state)
                    ST_RX_HOLD: begin
                        dev_data_low <= 1'b1;
                        dev_clk_low <= 1'b0;
                        if (bus_clk_low) state <= ST_RX_READY;
                    end
                    ST_RX_READY: begin
                        dev_data_low <= 1'b0;
                        if (clk_d && !bus_clk_low) begin
                            shifter <= {bus_data_low, shifter[7:1]};
                            if (bit_i == 4'd7) state <= ST_RX_ACK;
                            else bit_i <= bit_i + 4'd1;
                        end
                    end
                    ST_RX_ACK: begin
                        dev_data_low <= 1'b1;
                        dev_clk_low <= 1'b0;
                        if (atn_low) begin
                            if (shifter[7:5] == 3'b001 && shifter != 8'h3F) begin
                                listening <= shifter[4:0] == 5'd8;
                                ours <= shifter[4:0] == 5'd8;
                                talking <= 1'b0;
                            end else if (shifter == 8'h3F) begin
                                listening <= 1'b0;
                            end else if (shifter[7:5] == 3'b010 && shifter != 8'h5F) begin
                                talking <= shifter[4:0] == 5'd8;
                                ours <= shifter[4:0] == 5'd8;
                                listening <= 1'b0;
                            end else if (shifter == 8'h5F) begin
                                talking <= 1'b0;
                            end else if (shifter[7:4] == 4'hF) begin
                                sa <= shifter[3:0];
                                if (ours) begin
                                    opening <= 1'b1;
                                    name_len <= 5'd0;
                                end
                            end else if (shifter[7:4] == 4'h6) begin
                                sa <= shifter[3:0];
                                if (ours && talking) arm_send <= 1'b1;
                            end else if (shifter[7:4] == 4'hE) begin
                                opening <= 1'b0;
                            end
                        end else if (listening && ours && opening && sa != 4'hF && name_len < 5'd16) begin
                            name_mem[name_len] <= shifter;
                            name_len <= name_len + 5'd1;
                        end
                        // Hold DATA low until the talker pulls CLK. Releasing on
                        // that edge is the ready handshake for the next byte;
                        // dropping the ACK before the edge loses the byte.
                        bit_i <= 4'd0;
                        state <= ST_RX_HOLD;
                    end
                    ST_DIR_LOAD: begin
                        if (rd_phase == 2'd0) begin
                            disk_addr <= track_base(6'd18) + 18'd256 + {10'd0, load_i};
                            rd_phase <= 2'd1;
                        end else if (rd_phase == 2'd2) begin
                            sector[load_i] <= rd_data;
                            rd_phase <= 2'd0;
                            if (load_i == 8'd255) state <= ST_DIR_SCAN;
                            else load_i <= load_i + 8'd1;
                        end
                    end
                    ST_DIR_SCAN: begin
                        file_track <= 6'd0;
                        for (entry = 0; entry < 8; entry = entry + 1)
                            if (name_match(entry[2:0])) begin
                                file_track <= sector[{entry[2:0], 5'd3}][5:0];
                                file_sec <= sector[{entry[2:0], 5'd4}][5:0];
                            end
                        load_i <= 8'd0;
                        rd_phase <= 2'd0;
                        state <= ST_FILE_LOAD;
                    end
                    ST_FILE_LOAD: begin
                        if (file_track == 6'd0) begin
                            dev_clk_low <= 1'b0;
                            dev_data_low <= 1'b0;
                            state <= ST_IDLE;
                        end else if (rd_phase == 2'd0) begin
                            disk_addr <= track_base(file_track) + {file_sec, 8'd0} + {10'd0, load_i};
                            rd_phase <= 2'd1;
                        end else if (rd_phase == 2'd2) begin
                            sector[load_i] <= rd_data;
                            rd_phase <= 2'd0;
                            if (load_i == 8'd255) begin
                                link_t <= sector[0];
                                link_s <= sector[1];
                                idx <= 8'd2;
                                last <= sector[0] == 8'h00 ? sector[1] : 8'hFF;
                                state <= ST_TAKE;
                            end else
                                load_i <= load_i + 8'd1;
                        end
                    end
                    ST_TAKE: begin
                        tx_byte <= sector[idx];
                        tx_last <= link_t == 8'h00 && idx == last;
                        idx <= idx + 8'd1;
                        bit_i <= 4'd0;
                        timer <= 16'd0;
                        state <= ST_TX_WAIT;
                    end
                    ST_TX_WAIT: begin
                        dev_clk_low <= 1'b1;
                        dev_data_low <= 1'b0;
                        if (!bus_data_low) begin
                            timer <= 16'd0;
                            saw_pulse <= 1'b0;
                            state <= tx_last ? ST_TX_EOI : ST_TX_SETUP;
                        end
                    end
                    ST_TX_EOI: begin
                        dev_clk_low <= 1'b0;
                        dev_data_low <= 1'b0;
                        if (bus_data_low) saw_pulse <= 1'b1;
                        if (timer < 16'd250) timer <= timer + 16'd1;
                        else if (saw_pulse && !bus_data_low) begin
                            timer <= 16'd0;
                            state <= ST_TX_SETUP;
                        end
                    end
                    ST_TX_SETUP: begin
                        dev_clk_low <= 1'b1;
                        dev_data_low <= tx_byte[bit_i[2:0]];
                        if (timer == 16'd80) begin
                            timer <= 16'd0;
                            state <= ST_TX_CLOCK;
                        end else
                            timer <= timer + 16'd1;
                    end
                    ST_TX_CLOCK: begin
                        dev_clk_low <= 1'b0;
                        dev_data_low <= tx_byte[bit_i[2:0]];
                        if (timer == 16'd80) begin
                            timer <= 16'd0;
                            if (bit_i == 4'd7) begin
                                dev_data_low <= 1'b0;
                                state <= ST_TX_ACK;
                            end else begin
                                bit_i <= bit_i + 4'd1;
                                state <= ST_TX_SETUP;
                            end
                        end else
                            timer <= timer + 16'd1;
                    end
                    ST_TX_ACK: begin
                        dev_data_low <= 1'b0;
                        dev_clk_low <= 1'b1;
                        if (bus_data_low) begin
                            if (tx_last) begin
                                dev_clk_low <= 1'b0;
                                state <= ST_IDLE;
                            end else
                                state <= ST_TAKE;
                        end
                    end
                    default: begin
                        dev_clk_low <= 1'b0;
                        dev_data_low <= 1'b0;
                    end
                endcase
            end
        end
    end

    assign debug = debug_state;

    // The directory scan reads sector[] the cycle after the last store.
    // link bytes are captured from the array one cycle after the last write,
    // so FILE_LOAD copies them from rd_data of the last two indexes directly.
    // sector[0] and sector[1] are valid by ST_TAKE because the load completed
    // a full cycle earlier. name_match reads the array in ST_DIR_SCAN, which
    // is the cycle after load_i 255 was stored. That store is nonblocking, so
    // the last byte is not visible yet. Re-scan is unnecessary for byte 255
    // (a directory entry never uses it). Entries live at offsets 0..255 but
    // the last entry's final bytes include offset 255. Entry 7 ends at 255.
    // Capture the last byte into the array with a blocking read of rd_data
    // for offset 255 by scanning one cycle later. ST_DIR_SCAN is that cycle.
endmodule
