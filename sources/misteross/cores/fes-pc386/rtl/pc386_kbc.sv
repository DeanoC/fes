// SPDX-License-Identifier: MIT
// 8042 keyboard controller with a PS/2 keyboard and aux mouse
// (docs/pc386-machine-design.md, "8042 keyboard controller").
//
// An RTL state machine, not a microcontroller. 60h is data, 64h status and
// command. The command byte is controller RAM byte 0; commands 20h-3Fh read
// and 60h-7Fh write the 32-byte RAM. A20 is output port bit 1; driving output
// bit 0 low (D1 or an F0h-FEh pulse) requests a CPU reset. IBF is set by a
// write and cleared on the next clock, once its effect (A20 included) is in
// place. Controller responses are immediate. A device byte is delivered only
// one PS/2 byte time (BYTE_US) after the guest last read 60h or wrote a
// device, as a PS/2 link paces it. With XLAT set, keyboard bytes are
// translated from set 2 to set 1 with the standard 8042 table, an F0h prefix
// setting bit 7 of the next byte. The host keyboard model (pc386_hid_keyboard)
// turns fes.keyboard.hid's rows into the keyboard's set 2 codes, with the
// typematic byte the guest last sent the keyboard. A mouse packet is taken on
// the clock mouse_valid and mouse_ready are both high.
module pc386_kbc #(
    parameter integer US_N = 1,          // 1 MHz as N/D of the system clock
    parameter integer US_D = 100,
    parameter integer BYTE_US = 1000,
    parameter integer BAT_US = 5000,
    parameter integer MOUSE_DY_DOWN = 1  // the host's dy is screen-down positive
) (
    input wire clk, reset,
    input wire access, write, a2,        // a2: 0 port 60h, 1 port 64h
    input wire [7:0] wdata,
    output reg [7:0] rdata,
    output wire irq1, irq12,
    output wire a20,
    output reg cpu_reset,                // one clock
    // fes.keyboard.hid's nine rows
    input wire [143:0] keyboard_rows,
    // fes.mouse.relative packets
    input wire mouse_valid,
    input wire signed [7:0] mouse_dx, mouse_dy,
    input wire [1:0] mouse_buttons,      // bit 0 left, bit 1 right
    output wire mouse_ready
);
    wire us_tick;
    pc386_rate #(.N(US_N), .D(US_D)) us_rate (.clk(clk), .reset(reset), .tick(us_tick));
    assign mouse_ready = !reset;

    // Controller state
    reg [7:0] ram [0:31];
    wire [7:0] command = ram[0];
    reg obf, aux_obf, ibf, last_command;
    reg [7:0] output_byte;
    reg [1:0] output_port;               // bit 1 A20, bit 0 CPU running
    reg [2:0] pending;
    localparam NONE = 3'd0, WRITE_RAM = 3'd1, WRITE_OUTPUT = 3'd2, WRITE_KEYBOARD_BUFFER = 3'd3,
               WRITE_AUX_BUFFER = 3'd4, WRITE_AUX = 3'd5;
    reg [4:0] pending_index;
    reg response_valid, response_aux;
    reg [7:0] response;
    reg [15:0] pace;
    reg break_prefix;
    assign a20 = output_port[1];
    assign irq1 = obf && !aux_obf && command[0];
    assign irq12 = obf && aux_obf && command[1];

    // Devices
    reg keyboard_rx, aux_rx;
    reg [7:0] device_byte;
    wire keyboard_valid, aux_valid;
    wire [7:0] keyboard_byte, aux_byte;
    reg keyboard_take, aux_take;
    wire key_valid, key_ready, typematic_clear;
    wire [7:0] key_code;
    wire [6:0] typematic;
    pc386_hid_keyboard host_keyboard (
        .clk(clk), .reset(reset), .us_tick(us_tick), .rows(keyboard_rows),
        .typematic(typematic), .typematic_clear(typematic_clear),
        .key_valid(key_valid), .key_code(key_code), .key_ready(key_ready)
    );
    pc386_ps2_keyboard #(.BAT_US(BAT_US)) keyboard (
        .clk(clk), .reset(reset), .us_tick(us_tick),
        .rx_valid(keyboard_rx), .rx_byte(device_byte),
        .tx_valid(keyboard_valid), .tx_byte(keyboard_byte), .tx_take(keyboard_take),
        .key_valid(key_valid), .key_code(key_code), .key_ready(key_ready),
        .typematic(typematic), .typematic_clear(typematic_clear)
    );
    pc386_ps2_mouse #(.DY_DOWN(MOUSE_DY_DOWN)) mouse (
        .clk(clk), .reset(reset), .us_tick(us_tick),
        .rx_valid(aux_rx), .rx_byte(device_byte),
        .tx_valid(aux_valid), .tx_byte(aux_byte), .tx_take(aux_take),
        .move_valid(mouse_valid && mouse_ready), .move_dx(mouse_dx), .move_dy(mouse_dy),
        .move_buttons(mouse_buttons)
    );

    function automatic [7:0] translate(input [7:0] code);
        reg [7:0] table_value;
        case (code)
            8'h00: table_value = 8'hff; 8'h01: table_value = 8'h43; 8'h02: table_value = 8'h41; 8'h03: table_value = 8'h3f;
            8'h04: table_value = 8'h3d; 8'h05: table_value = 8'h3b; 8'h06: table_value = 8'h3c; 8'h07: table_value = 8'h58;
            8'h08: table_value = 8'h64; 8'h09: table_value = 8'h44; 8'h0a: table_value = 8'h42; 8'h0b: table_value = 8'h40;
            8'h0c: table_value = 8'h3e; 8'h0d: table_value = 8'h0f; 8'h0e: table_value = 8'h29; 8'h0f: table_value = 8'h59;
            8'h10: table_value = 8'h65; 8'h11: table_value = 8'h38; 8'h12: table_value = 8'h2a; 8'h13: table_value = 8'h70;
            8'h14: table_value = 8'h1d; 8'h15: table_value = 8'h10; 8'h16: table_value = 8'h02; 8'h17: table_value = 8'h5a;
            8'h18: table_value = 8'h66; 8'h19: table_value = 8'h71; 8'h1a: table_value = 8'h2c; 8'h1b: table_value = 8'h1f;
            8'h1c: table_value = 8'h1e; 8'h1d: table_value = 8'h11; 8'h1e: table_value = 8'h03; 8'h1f: table_value = 8'h5b;
            8'h20: table_value = 8'h67; 8'h21: table_value = 8'h2e; 8'h22: table_value = 8'h2d; 8'h23: table_value = 8'h20;
            8'h24: table_value = 8'h12; 8'h25: table_value = 8'h05; 8'h26: table_value = 8'h04; 8'h27: table_value = 8'h5c;
            8'h28: table_value = 8'h68; 8'h29: table_value = 8'h39; 8'h2a: table_value = 8'h2f; 8'h2b: table_value = 8'h21;
            8'h2c: table_value = 8'h14; 8'h2d: table_value = 8'h13; 8'h2e: table_value = 8'h06; 8'h2f: table_value = 8'h5d;
            8'h30: table_value = 8'h69; 8'h31: table_value = 8'h31; 8'h32: table_value = 8'h30; 8'h33: table_value = 8'h23;
            8'h34: table_value = 8'h22; 8'h35: table_value = 8'h15; 8'h36: table_value = 8'h07; 8'h37: table_value = 8'h5e;
            8'h38: table_value = 8'h6a; 8'h39: table_value = 8'h72; 8'h3a: table_value = 8'h32; 8'h3b: table_value = 8'h24;
            8'h3c: table_value = 8'h16; 8'h3d: table_value = 8'h08; 8'h3e: table_value = 8'h09; 8'h3f: table_value = 8'h5f;
            8'h40: table_value = 8'h6b; 8'h41: table_value = 8'h33; 8'h42: table_value = 8'h25; 8'h43: table_value = 8'h17;
            8'h44: table_value = 8'h18; 8'h45: table_value = 8'h0b; 8'h46: table_value = 8'h0a; 8'h47: table_value = 8'h60;
            8'h48: table_value = 8'h6c; 8'h49: table_value = 8'h34; 8'h4a: table_value = 8'h35; 8'h4b: table_value = 8'h26;
            8'h4c: table_value = 8'h27; 8'h4d: table_value = 8'h19; 8'h4e: table_value = 8'h0c; 8'h4f: table_value = 8'h61;
            8'h50: table_value = 8'h6d; 8'h51: table_value = 8'h73; 8'h52: table_value = 8'h28; 8'h53: table_value = 8'h74;
            8'h54: table_value = 8'h1a; 8'h55: table_value = 8'h0d; 8'h56: table_value = 8'h62; 8'h57: table_value = 8'h6e;
            8'h58: table_value = 8'h3a; 8'h59: table_value = 8'h36; 8'h5a: table_value = 8'h1c; 8'h5b: table_value = 8'h1b;
            8'h5c: table_value = 8'h75; 8'h5d: table_value = 8'h2b; 8'h5e: table_value = 8'h63; 8'h5f: table_value = 8'h76;
            8'h60: table_value = 8'h55; 8'h61: table_value = 8'h56; 8'h62: table_value = 8'h77; 8'h63: table_value = 8'h78;
            8'h64: table_value = 8'h79; 8'h65: table_value = 8'h7a; 8'h66: table_value = 8'h0e; 8'h67: table_value = 8'h7b;
            8'h68: table_value = 8'h7c; 8'h69: table_value = 8'h4f; 8'h6a: table_value = 8'h7d; 8'h6b: table_value = 8'h4b;
            8'h6c: table_value = 8'h47; 8'h6d: table_value = 8'h7e; 8'h6e: table_value = 8'h7f; 8'h6f: table_value = 8'h6f;
            8'h70: table_value = 8'h52; 8'h71: table_value = 8'h53; 8'h72: table_value = 8'h50; 8'h73: table_value = 8'h4c;
            8'h74: table_value = 8'h4d; 8'h75: table_value = 8'h48; 8'h76: table_value = 8'h01; 8'h77: table_value = 8'h45;
            8'h78: table_value = 8'h57; 8'h79: table_value = 8'h4e; 8'h7a: table_value = 8'h51; 8'h7b: table_value = 8'h4a;
            8'h7c: table_value = 8'h37; 8'h7d: table_value = 8'h49; 8'h7e: table_value = 8'h46; 8'h7f: table_value = 8'h54;
            8'h83: table_value = 8'h41; 8'h84: table_value = 8'h54;
            default: table_value = code;  // 80h-FFh pass through
        endcase
        translate = table_value;
    endfunction

    always @* begin
        rdata = a2 ? {2'b00, aux_obf, 1'b1, last_command, command[2], ibf, obf} : output_byte;
    end

    wire paced = pace == 0;
    integer n;
    always @(posedge clk) begin
        cpu_reset <= 0;
        keyboard_rx <= 0;
        aux_rx <= 0;
        keyboard_take <= 0;
        aux_take <= 0;
        if (reset) begin
            for (n = 1; n < 32; n = n + 1) ram[n] <= 0;
            // Keyboard interface and both interrupts enabled, aux disabled, XLAT.
            ram[0] <= 8'h63;
            obf <= 0; aux_obf <= 0; ibf <= 0; last_command <= 1;
            output_byte <= 0; output_port <= 2'b11;
            pending <= NONE; pending_index <= 0;
            response_valid <= 0; response_aux <= 0; response <= 0;
            pace <= 0; break_prefix <= 0; device_byte <= 0;
        end else begin
            if (us_tick && pace != 0) pace <= pace - 1'b1;
            ibf <= 0;

            // Output buffer: a controller response first, then paced device bytes.
            if (!obf && !keyboard_take && !aux_take) begin
                if (response_valid) begin
                    output_byte <= response;
                    aux_obf <= response_aux;
                    obf <= 1;
                    response_valid <= 0;
                end else if (paced && keyboard_valid && !command[4]) begin
                    keyboard_take <= 1;
                    if (command[6] && keyboard_byte == 8'hf0) break_prefix <= 1;
                    else begin
                        output_byte <= command[6] ? translate(keyboard_byte) | {break_prefix, 7'b0} : keyboard_byte;
                        break_prefix <= 0;
                        aux_obf <= 0;
                        obf <= 1;
                    end
                end else if (paced && aux_valid && !command[5]) begin
                    aux_take <= 1;
                    output_byte <= aux_byte;
                    aux_obf <= 1;
                    obf <= 1;
                end
            end

            if (access && !write && !a2) begin
                obf <= 0;
                aux_obf <= 0;
                pace <= 16'(BYTE_US);
            end

            if (access && write) begin
                ibf <= 1;
                last_command <= a2;
                if (a2) begin
                    pending <= NONE;
                    casez (wdata)
                        8'b001?????: begin response <= ram[wdata[4:0]]; response_aux <= 0; response_valid <= 1; end
                        8'b011?????: begin pending <= WRITE_RAM; pending_index <= wdata[4:0]; end
                        8'ha7: ram[0][5] <= 1;
                        8'ha8: ram[0][5] <= 0;
                        8'ha9, 8'hab, 8'he0: begin response <= 8'h00; response_aux <= 0; response_valid <= 1; end
                        8'haa: begin response <= 8'h55; response_aux <= 0; response_valid <= 1; ram[0][2] <= 1; end
                        8'had: ram[0][4] <= 1;
                        8'hae: ram[0][4] <= 0;
                        8'hc0: begin response <= 8'h80; response_aux <= 0; response_valid <= 1; end
                        8'hd0: begin
                            response <= {2'b00, irq12, irq1, 2'b00, output_port};
                            response_aux <= 0; response_valid <= 1;
                        end
                        8'hd1: pending <= WRITE_OUTPUT;
                        8'hd2: pending <= WRITE_KEYBOARD_BUFFER;
                        8'hd3: pending <= WRITE_AUX_BUFFER;
                        8'hd4: pending <= WRITE_AUX;
                        8'hdd: output_port[1] <= 0;
                        8'hdf: output_port[1] <= 1;
                        8'b1111????: if (!wdata[0]) cpu_reset <= 1;
                        default: begin end  // unknown commands are ignored
                    endcase
                end else begin
                    pending <= NONE;
                    case (pending)
                        WRITE_RAM: ram[pending_index] <= wdata;
                        WRITE_OUTPUT: begin
                            output_port <= wdata[1:0];
                            if (!wdata[0]) cpu_reset <= 1;
                        end
                        WRITE_KEYBOARD_BUFFER: begin response <= wdata; response_aux <= 0; response_valid <= 1; end
                        WRITE_AUX_BUFFER: begin response <= wdata; response_aux <= 1; response_valid <= 1; end
                        WRITE_AUX: begin
                            device_byte <= wdata;
                            aux_rx <= 1;
                            ram[0][5] <= 0;
                            pace <= 16'(BYTE_US);
                        end
                        default: begin
                            // A data byte with no command pending goes to the
                            // keyboard and enables its interface.
                            device_byte <= wdata;
                            keyboard_rx <= 1;
                            ram[0][4] <= 0;
                            pace <= 16'(BYTE_US);
                        end
                    endcase
                end
            end
        end
    end
endmodule

// 16-byte FIFO for the PS/2 devices' bytes to the controller.
module pc386_ps2_fifo (
    input wire clk, clear,             // clear empties it before this clock's push
    input wire push,
    input wire [7:0] push_byte,
    input wire pop,
    output wire valid,
    output wire [7:0] head,
    output wire [4:0] space
);
    reg [7:0] bytes [0:15];
    reg [3:0] read_at, write_at;
    reg [4:0] count;
    assign valid = count != 0;
    assign head = bytes[read_at];
    assign space = 5'd16 - count;
    always @(posedge clk) begin
        if (clear) begin
            // A byte pushed with the clear (a reset's ACK) is kept.
            read_at <= 0;
            write_at <= {3'b0, push};
            count <= {4'b0, push};
            if (push) bytes[0] <= push_byte;
        end else begin
            if (push && count != 16) begin
                bytes[write_at] <= push_byte;
                write_at <= write_at + 1'b1;
            end
            if (pop && count != 0) read_at <= read_at + 1'b1;
            count <= count + {4'b0, push && count != 16} - {4'b0, pop && count != 0};
        end
    end
endmodule

// PS/2 keyboard: commands from the controller, set 2 codes from the host.
// FF resets and reports BAT completion (AAh) BAT_US later (a recorded
// deviation from the half second some keyboards took). F0 accepts only set 2:
// asking for 1 or 3 is acknowledged and ignored. ED is acknowledged; the
// host's LEDs are not driven. F3's byte is the host keyboard model's
// typematic delay and rate; FF, F5 and F6 restore the default 2Bh (500 ms,
// 10.9 per second) and stop the repeat. Unknown commands get FEh.
module pc386_ps2_keyboard #(
    parameter integer BAT_US = 5000
) (
    input wire clk, reset, us_tick,
    input wire rx_valid,
    input wire [7:0] rx_byte,
    output wire tx_valid,
    output wire [7:0] tx_byte,
    input wire tx_take,
    input wire key_valid,
    input wire [7:0] key_code,
    output wire key_ready,             // a byte is taken while valid and ready
    output reg [6:0] typematic,
    output reg typematic_clear
);
    reg scanning;
    reg [1:0] argument;                  // 1 LEDs, 2 scan-code set, 3 typematic
    reg [7:0] last_sent;
    reg [15:0] bat;
    reg clear;
    reg [7:0] queue [0:2];
    wire [4:0] space;
    // One response of up to three bytes is pushed a byte per clock. Host
    // codes leave room for the largest response; a keyboard that is not
    // scanning drops them.
    reg [1:0] pushing;
    // Require four free slots before accepting a host byte, leaving three
    // for a command reply after the key is queued.
    assign key_ready = pushing == 0 && !rx_valid && space >= 4;
    wire push = (pushing != 0 && space != 0) || (key_valid && key_ready && scanning);
    wire [7:0] push_byte = pushing != 0 ? queue[3 - pushing] : key_code;
    pc386_ps2_fifo fifo (.clk(clk), .clear(clear || reset), .push(push), .push_byte(push_byte),
                         .pop(tx_take), .valid(tx_valid), .head(tx_byte), .space(space));

    task automatic respond(input [1:0] count, input [7:0] first, input [7:0] second, input [7:0] third);
        queue[0] <= count == 3 ? first : count == 2 ? 8'h00 : 8'h00;
        queue[1] <= count == 3 ? second : count == 2 ? first : 8'h00;
        queue[2] <= count == 3 ? third : count == 2 ? second : first;
        pushing <= count;
    endtask

    always @(posedge clk) begin
        clear <= 0;
        typematic_clear <= 0;
        if (reset) begin
            scanning <= 1; argument <= 0; last_sent <= 8'hfa; bat <= 0; pushing <= 0; typematic <= 7'h2b;
        end else begin
            if (pushing != 0 && space != 0) pushing <= pushing - 1'b1;
            if (tx_take) last_sent <= tx_byte;
            if (bat != 0 && us_tick) begin
                bat <= bat - 1'b1;
                if (bat == 1) respond(2'd1, 8'haa, 8'h00, 8'h00);
            end
            if (rx_valid) begin
                argument <= 0;
                if (argument == 2 && rx_byte == 8'h00) respond(2'd2, 8'hfa, 8'h02, 8'h00);
                else if (argument != 0) begin
                    if (argument == 3) typematic <= rx_byte[6:0];
                    respond(2'd1, 8'hfa, 8'h00, 8'h00);
                end else case (rx_byte)
                    8'hff: begin
                        clear <= 1;
                        scanning <= 1;
                        typematic <= 7'h2b; typematic_clear <= 1;
                        respond(2'd1, 8'hfa, 8'h00, 8'h00);
                        bat <= 16'(BAT_US);
                    end
                    8'hfe: respond(2'd1, last_sent, 8'h00, 8'h00);
                    8'hf6: begin typematic <= 7'h2b; typematic_clear <= 1; respond(2'd1, 8'hfa, 8'h00, 8'h00); end
                    8'hf5: begin
                        scanning <= 0;
                        typematic <= 7'h2b; typematic_clear <= 1;
                        respond(2'd1, 8'hfa, 8'h00, 8'h00);
                    end
                    8'hf4: begin scanning <= 1; respond(2'd1, 8'hfa, 8'h00, 8'h00); end
                    8'hf3: begin argument <= 3; respond(2'd1, 8'hfa, 8'h00, 8'h00); end
                    8'hed: begin argument <= 1; respond(2'd1, 8'hfa, 8'h00, 8'h00); end
                    8'hf0: begin argument <= 2; respond(2'd1, 8'hfa, 8'h00, 8'h00); end
                    8'hf2: respond(2'd3, 8'hfa, 8'hab, 8'h83);
                    8'hee: respond(2'd1, 8'hee, 8'h00, 8'h00);
                    default: respond(2'd1, 8'hfe, 8'h00, 8'h00);
                endcase
            end
        end
    end
endmodule

// PS/2 aux mouse: two buttons, ID 00h, stream mode by default. Host movement
// accumulates in quarter counts, scaled by 2^(resolution-2), and is carried
// into the next packet beyond +-255 and below one count, so the guest receives
// exactly the host's sum; the overflow bits stay zero. 2:1 scaling applies the
// PS/2 table to stream packets. Wrap mode echoes every byte but ECh and FFh.
module pc386_ps2_mouse #(
    parameter integer DY_DOWN = 1
) (
    input wire clk, reset, us_tick,
    input wire rx_valid,
    input wire [7:0] rx_byte,
    output wire tx_valid,
    output wire [7:0] tx_byte,
    input wire tx_take,
    input wire move_valid,
    input wire signed [7:0] move_dx, move_dy,
    input wire [1:0] move_buttons
);
    reg enabled, remote, wrap, scaling;
    reg [1:0] resolution;
    reg [7:0] sample_rate;
    reg [1:0] argument;                  // 1 sample rate, 2 resolution
    reg [7:0] last_sent;
    reg [1:0] buttons;
    reg buttons_changed;
    reg signed [19:0] x, y;              // quarter counts
    reg [16:0] sample_timer;
    reg [2:0] pushing;
    reg [7:0] queue [0:3];
    reg clear, read_data;
    wire [4:0] space;
    pc386_ps2_fifo fifo (.clk(clk), .clear(clear || reset), .push(pushing != 0), .push_byte(queue[4 - pushing]),
                         .pop(tx_take), .valid(tx_valid), .head(tx_byte), .space(space));

    // This packet's counts: whole counts, clamped to +-255.
    // Arithmetic right shift rounds negative fractions down. Add one back
    // when a negative quarter-count has a remainder, giving truncation
    // toward zero and keeping the fraction for a later packet.
    wire signed [19:0] whole_x = (x >>> 2) + ((x < 0 && x[1:0] != 0) ? 20'sd1 : 20'sd0);
    wire signed [19:0] whole_y = (y >>> 2) + ((y < 0 && y[1:0] != 0) ? 20'sd1 : 20'sd0);
    wire signed [9:0] report_x = whole_x > 255 ? 10'sd255 : whole_x < -255 ? -10'sd255 : 10'(whole_x);
    wire signed [9:0] report_y = whole_y > 255 ? 10'sd255 : whole_y < -255 ? -10'sd255 : 10'(whole_y);
    function automatic signed [9:0] scale21(input signed [9:0] value);
        reg [9:0] magnitude;
        reg [9:0] scaled;
        magnitude = value < 0 ? 10'(-value) : 10'(value);
        case (magnitude)
            10'd0: scaled = 0; 10'd1: scaled = 1; 10'd2: scaled = 1; 10'd3: scaled = 3;
            10'd4: scaled = 6; 10'd5: scaled = 9;
            default: scaled = magnitude * 2 > 255 ? 10'd255 : magnitude * 2;
        endcase
        scale21 = value < 0 ? -$signed(scaled) : $signed(scaled);
    endfunction
    wire signed [9:0] sent_x = scaling && !read_data ? scale21(report_x) : report_x;
    wire signed [9:0] sent_y = scaling && !read_data ? scale21(report_y) : report_y;
    wire moved = report_x != 0 || report_y != 0 || buttons_changed;
    // The PS/2 rates 10, 20, 40, 60, 80, 100 and 200 per second; others round down.
    wire [16:0] sample_us = sample_rate >= 200 ? 17'd5000 : sample_rate >= 100 ? 17'd10000 :
                            sample_rate >= 80 ? 17'd12500 : sample_rate >= 60 ? 17'd16667 :
                            sample_rate >= 40 ? 17'd25000 : sample_rate >= 20 ? 17'd50000 : 17'd100000;

    task automatic respond(input [2:0] count, input [7:0] b0, input [7:0] b1, input [7:0] b2, input [7:0] b3);
        // queue[4 - pushing] is pushed while pushing counts down from count.
        queue[4 - count] <= b0;
        if (count > 1) queue[5 - count] <= b1;
        if (count > 2) queue[6 - count] <= b2;
        if (count > 3) queue[3] <= b3;
        pushing <= count;
    endtask
    task automatic defaults;
        enabled <= 0; remote <= 0; scaling <= 0; resolution <= 2; sample_rate <= 100;
    endtask
    wire [7:0] packet_head = {2'b00, sent_y[9], sent_x[9], 1'b1, 1'b0, buttons[1], buttons[0]};

    always @(posedge clk) begin
        clear <= 0;
        if (reset) begin
            defaults(); wrap <= 0; argument <= 0; last_sent <= 8'hfa; buttons <= 0; buttons_changed <= 0;
            x <= 0; y <= 0; sample_timer <= 0; pushing <= 0; read_data <= 0;
        end else begin
            if (pushing != 0) pushing <= pushing - 1'b1;
            if (tx_take) last_sent <= tx_byte;
            if (us_tick && sample_timer != 0) sample_timer <= sample_timer - 1'b1;
            if (move_valid) begin
                x <= x + (20'(move_dx) <<< resolution);
                y <= y + (DY_DOWN != 0 ? -(20'(move_dy) <<< resolution) : (20'(move_dy) <<< resolution));
                if (move_buttons != buttons) buttons_changed <= 1;
                buttons <= move_buttons;
            end
            // Stream packets at the sample rate; EB (read_data) reports once.
            // A stream packet uses three slots; keep four for the largest
            // command reply (E9) that may arrive before the guest reads it.
            if (pushing == 0 && !rx_valid && !move_valid && space >= 7 &&
                (read_data || (enabled && !remote && !wrap && sample_timer == 0 && moved))) begin
                respond(3'd3, packet_head, sent_x[7:0], sent_y[7:0], 8'h00);
                x <= x - (20'(report_x) <<< 2);
                y <= y - (20'(report_y) <<< 2);
                buttons_changed <= 0;
                sample_timer <= sample_us;
                read_data <= 0;
            end
            if (rx_valid) begin
                argument <= 0;
                if (wrap && rx_byte != 8'hec && rx_byte != 8'hff) respond(3'd1, rx_byte, 0, 0, 0);
                else if (argument == 1) begin sample_rate <= rx_byte; respond(3'd1, 8'hfa, 0, 0, 0); end
                else if (argument == 2) begin resolution <= rx_byte[1:0]; respond(3'd1, 8'hfa, 0, 0, 0); end
                else case (rx_byte)
                    8'hff: begin
                        clear <= 1; defaults(); wrap <= 0; x <= 0; y <= 0;
                        respond(3'd3, 8'hfa, 8'haa, 8'h00, 0);
                    end
                    8'hfe: respond(3'd1, last_sent, 0, 0, 0);
                    8'hf6: begin defaults(); respond(3'd1, 8'hfa, 0, 0, 0); end
                    8'hf5: begin enabled <= 0; respond(3'd1, 8'hfa, 0, 0, 0); end
                    8'hf4: begin enabled <= 1; respond(3'd1, 8'hfa, 0, 0, 0); end
                    8'hf3: begin argument <= 1; respond(3'd1, 8'hfa, 0, 0, 0); end
                    8'hf2: respond(3'd2, 8'hfa, 8'h00, 0, 0);
                    8'hf0: begin remote <= 1; respond(3'd1, 8'hfa, 0, 0, 0); end
                    8'hee: begin wrap <= 1; respond(3'd1, 8'hfa, 0, 0, 0); end
                    8'hec: begin wrap <= 0; respond(3'd1, 8'hfa, 0, 0, 0); end
                    8'heb: begin read_data <= 1; respond(3'd1, 8'hfa, 0, 0, 0); end
                    8'hea: begin remote <= 0; respond(3'd1, 8'hfa, 0, 0, 0); end
                    8'he9: respond(3'd4, 8'hfa, {1'b0, remote, enabled, scaling, 1'b0, buttons[0], 1'b0, buttons[1]},
                                   {6'b0, resolution}, sample_rate);
                    8'he8: begin argument <= 2; respond(3'd1, 8'hfa, 0, 0, 0); end
                    8'he7: begin scaling <= 1; respond(3'd1, 8'hfa, 0, 0, 0); end
                    8'he6: begin scaling <= 0; respond(3'd1, 8'hfa, 0, 0, 0); end
                    default: respond(3'd1, 8'hfe, 0, 0, 0);
                endcase
            end
        end
    end
endmodule
