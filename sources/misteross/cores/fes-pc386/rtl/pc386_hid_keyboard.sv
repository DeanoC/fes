// SPDX-License-Identifier: MIT
// The host keyboard model (docs/pc386-machine-design.md, "Keyboard model"):
// fes.keyboard.hid's nine rows in, the PS/2 keyboard's set 2 make and break
// codes out.
//
// rows bits 0-127 are Keyboard/Keypad usages 00h-7Fh and bits 128-135 the
// modifiers E0h-E7h (mister-packages docs/computer-io.md, "Keyboard"); bits
// 136-143 are zero. The rows count as one change once they have been
// unchanged for 1000 us_ticks (1 ms). A change is diffed against the state the
// guest has been sent, one bit a clock, in four passes: other keys' releases,
// modifiers' releases, modifiers' presses, other keys' presses, each in usage
// order. The next change is taken only once every byte of this one is out.
// Usages without a set 2 code are ignored.
//
// Typematic: the most recently pressed key other than a modifier or Pause
// repeats its make sequence after the delay and then at the rate of the
// keyboard's F3 byte (typematic[6:5] delay, [4:0] rate). Its release, a press
// of another such key or typematic_clear (the keyboard's FF, F5 and F6) stops
// it; modifiers neither repeat nor stop it. Time is kept in thirds of a
// microsecond, so the rate (8 + A) x 2^B / 240 s is exact on average. A
// repeat that falls due while bytes are waiting is sent once.
module pc386_hid_keyboard (
    input wire clk, reset, us_tick,
    input wire [143:0] rows,
    input wire [6:0] typematic,
    input wire typematic_clear,
    output reg key_valid,                // set 2 bytes, as pc386_ps2_keyboard takes them
    output wire [7:0] key_code,
    input wire key_ready
);
    localparam [1:0] NONE = 2'd0, PLAIN = 2'd1, EXTENDED = 2'd2, SPECIAL = 2'd3;
    localparam [7:0] PRINT_SCREEN = 8'h7c, PAUSE = 8'h77;   // SPECIAL's codes
    localparam [9:0] STABLE_US = 10'd1000;

    // Usages 00h-7Fh, then the modifiers E0h-E7h at 80h-87h: {kind, code}.
    // Microsoft's USB HID to PS/2 scan code translation table; Print Screen
    // and Pause are SPECIAL.
    function automatic [9:0] set2(input [7:0] usage);
        case (usage)
            8'h04: set2 = {PLAIN, 8'h1c}; 8'h05: set2 = {PLAIN, 8'h32}; 8'h06: set2 = {PLAIN, 8'h21};
            8'h07: set2 = {PLAIN, 8'h23}; 8'h08: set2 = {PLAIN, 8'h24}; 8'h09: set2 = {PLAIN, 8'h2b};
            8'h0a: set2 = {PLAIN, 8'h34}; 8'h0b: set2 = {PLAIN, 8'h33}; 8'h0c: set2 = {PLAIN, 8'h43};
            8'h0d: set2 = {PLAIN, 8'h3b}; 8'h0e: set2 = {PLAIN, 8'h42}; 8'h0f: set2 = {PLAIN, 8'h4b};
            8'h10: set2 = {PLAIN, 8'h3a}; 8'h11: set2 = {PLAIN, 8'h31}; 8'h12: set2 = {PLAIN, 8'h44};
            8'h13: set2 = {PLAIN, 8'h4d}; 8'h14: set2 = {PLAIN, 8'h15}; 8'h15: set2 = {PLAIN, 8'h2d};
            8'h16: set2 = {PLAIN, 8'h1b}; 8'h17: set2 = {PLAIN, 8'h2c}; 8'h18: set2 = {PLAIN, 8'h3c};
            8'h19: set2 = {PLAIN, 8'h2a}; 8'h1a: set2 = {PLAIN, 8'h1d}; 8'h1b: set2 = {PLAIN, 8'h22};
            8'h1c: set2 = {PLAIN, 8'h35}; 8'h1d: set2 = {PLAIN, 8'h1a};
            8'h1e: set2 = {PLAIN, 8'h16}; 8'h1f: set2 = {PLAIN, 8'h1e}; 8'h20: set2 = {PLAIN, 8'h26};
            8'h21: set2 = {PLAIN, 8'h25}; 8'h22: set2 = {PLAIN, 8'h2e}; 8'h23: set2 = {PLAIN, 8'h36};
            8'h24: set2 = {PLAIN, 8'h3d}; 8'h25: set2 = {PLAIN, 8'h3e}; 8'h26: set2 = {PLAIN, 8'h46};
            8'h27: set2 = {PLAIN, 8'h45};
            8'h28: set2 = {PLAIN, 8'h5a}; 8'h29: set2 = {PLAIN, 8'h76}; 8'h2a: set2 = {PLAIN, 8'h66};
            8'h2b: set2 = {PLAIN, 8'h0d}; 8'h2c: set2 = {PLAIN, 8'h29}; 8'h2d: set2 = {PLAIN, 8'h4e};
            8'h2e: set2 = {PLAIN, 8'h55}; 8'h2f: set2 = {PLAIN, 8'h54}; 8'h30: set2 = {PLAIN, 8'h5b};
            8'h31: set2 = {PLAIN, 8'h5d}; 8'h32: set2 = {PLAIN, 8'h5d}; 8'h33: set2 = {PLAIN, 8'h4c};
            8'h34: set2 = {PLAIN, 8'h52}; 8'h35: set2 = {PLAIN, 8'h0e}; 8'h36: set2 = {PLAIN, 8'h41};
            8'h37: set2 = {PLAIN, 8'h49}; 8'h38: set2 = {PLAIN, 8'h4a}; 8'h39: set2 = {PLAIN, 8'h58};
            // F1-F12
            8'h3a: set2 = {PLAIN, 8'h05}; 8'h3b: set2 = {PLAIN, 8'h06}; 8'h3c: set2 = {PLAIN, 8'h04};
            8'h3d: set2 = {PLAIN, 8'h0c}; 8'h3e: set2 = {PLAIN, 8'h03}; 8'h3f: set2 = {PLAIN, 8'h0b};
            8'h40: set2 = {PLAIN, 8'h83}; 8'h41: set2 = {PLAIN, 8'h0a}; 8'h42: set2 = {PLAIN, 8'h01};
            8'h43: set2 = {PLAIN, 8'h09}; 8'h44: set2 = {PLAIN, 8'h78}; 8'h45: set2 = {PLAIN, 8'h07};
            8'h46: set2 = {SPECIAL, PRINT_SCREEN}; 8'h47: set2 = {PLAIN, 8'h7e}; 8'h48: set2 = {SPECIAL, PAUSE};
            // Insert, Home, Page Up, Delete, End, Page Down, the arrows
            8'h49: set2 = {EXTENDED, 8'h70}; 8'h4a: set2 = {EXTENDED, 8'h6c}; 8'h4b: set2 = {EXTENDED, 8'h7d};
            8'h4c: set2 = {EXTENDED, 8'h71}; 8'h4d: set2 = {EXTENDED, 8'h69}; 8'h4e: set2 = {EXTENDED, 8'h7a};
            8'h4f: set2 = {EXTENDED, 8'h74}; 8'h50: set2 = {EXTENDED, 8'h6b}; 8'h51: set2 = {EXTENDED, 8'h72};
            8'h52: set2 = {EXTENDED, 8'h75};
            // Num Lock and the keypad
            8'h53: set2 = {PLAIN, 8'h77}; 8'h54: set2 = {EXTENDED, 8'h4a}; 8'h55: set2 = {PLAIN, 8'h7c};
            8'h56: set2 = {PLAIN, 8'h7b}; 8'h57: set2 = {PLAIN, 8'h79}; 8'h58: set2 = {EXTENDED, 8'h5a};
            8'h59: set2 = {PLAIN, 8'h69}; 8'h5a: set2 = {PLAIN, 8'h72}; 8'h5b: set2 = {PLAIN, 8'h7a};
            8'h5c: set2 = {PLAIN, 8'h6b}; 8'h5d: set2 = {PLAIN, 8'h73}; 8'h5e: set2 = {PLAIN, 8'h74};
            8'h5f: set2 = {PLAIN, 8'h6c}; 8'h60: set2 = {PLAIN, 8'h75}; 8'h61: set2 = {PLAIN, 8'h7d};
            8'h62: set2 = {PLAIN, 8'h70}; 8'h63: set2 = {PLAIN, 8'h71};
            // Non-US \, Application, Power, keypad =
            8'h64: set2 = {PLAIN, 8'h61}; 8'h65: set2 = {EXTENDED, 8'h2f}; 8'h66: set2 = {EXTENDED, 8'h37};
            8'h67: set2 = {PLAIN, 8'h0f};
            // F13-F24
            8'h68: set2 = {PLAIN, 8'h08}; 8'h69: set2 = {PLAIN, 8'h10}; 8'h6a: set2 = {PLAIN, 8'h18};
            8'h6b: set2 = {PLAIN, 8'h20}; 8'h6c: set2 = {PLAIN, 8'h28}; 8'h6d: set2 = {PLAIN, 8'h30};
            8'h6e: set2 = {PLAIN, 8'h38}; 8'h6f: set2 = {PLAIN, 8'h40}; 8'h70: set2 = {PLAIN, 8'h48};
            8'h71: set2 = {PLAIN, 8'h50}; 8'h72: set2 = {PLAIN, 8'h57}; 8'h73: set2 = {PLAIN, 8'h5f};
            8'h7f: set2 = {EXTENDED, 8'h23};                                        // Mute
            // Left Control, Shift, Alt, GUI; right Control, Shift, Alt, GUI
            8'h80: set2 = {PLAIN, 8'h14}; 8'h81: set2 = {PLAIN, 8'h12}; 8'h82: set2 = {PLAIN, 8'h11};
            8'h83: set2 = {EXTENDED, 8'h1f}; 8'h84: set2 = {EXTENDED, 8'h14}; 8'h85: set2 = {PLAIN, 8'h59};
            8'h86: set2 = {EXTENDED, 8'h11}; 8'h87: set2 = {EXTENDED, 8'h27};
            default: set2 = {NONE, 8'h00};
        endcase
    endfunction

    // The bytes of a key's make or break, and how many there are.
    function automatic [3:0] length(input [1:0] kind, input [7:0] code, input make);
        case (kind)
            PLAIN: length = make ? 4'd1 : 4'd2;
            EXTENDED: length = make ? 4'd2 : 4'd3;
            SPECIAL: length = code == PAUSE ? (make ? 4'd8 : 4'd0) : make ? 4'd4 : 4'd6;
            default: length = 4'd0;
        endcase
    endfunction
    function automatic [7:0] sequence_byte(input [1:0] kind, input [7:0] code, input make, input [2:0] step);
        reg [7:0] value;
        value = 8'h00;
        case (kind)
            PLAIN: value = make || step == 3'd1 ? code : 8'hf0;
            EXTENDED: value = step == 3'd0 ? 8'he0 : make || step == 3'd2 ? code : 8'hf0;
            SPECIAL:
                if (code == PAUSE) case (step)                                  // E1 14 77 E1 F0 14 F0 77
                    3'd0, 3'd3: value = 8'he1;
                    3'd1, 3'd5: value = 8'h14;
                    3'd4, 3'd6: value = 8'hf0;
                    default: value = 8'h77;
                endcase
                else if (make) case (step)                                      // E0 12 E0 7C
                    3'd1: value = 8'h12;
                    3'd3: value = 8'h7c;
                    default: value = 8'he0;
                endcase
                else case (step)                                                // E0 F0 7C E0 F0 12
                    3'd0, 3'd3: value = 8'he0;
                    3'd1, 3'd4: value = 8'hf0;
                    3'd2: value = 8'h7c;
                    default: value = 8'h12;
                endcase
            default: value = 8'h00;
        endcase
        sequence_byte = value;
    endfunction

    // Typematic limits in thirds of a microsecond: (D + 1) x 250 ms, and
    // (8 + A) x 2^B / 240 s.
    function automatic [21:0] delay_thirds(input [1:0] delay);
        case (delay)
            2'd0: delay_thirds = 22'd750000;
            2'd1: delay_thirds = 22'd1500000;
            2'd2: delay_thirds = 22'd2250000;
            default: delay_thirds = 22'd3000000;
        endcase
    endfunction
    function automatic [21:0] period_thirds(input [4:0] rate);
        period_thirds = (22'(4'd8 + {1'b0, rate[2:0]}) * 22'd12500) << rate[4:3];
    endfunction

    reg [135:0] observed, stable, sent;
    reg [9:0] age;
    // The diff: pass 0 key releases, 1 modifier releases, 2 modifier presses,
    // 3 key presses.
    reg diffing;
    reg [1:0] pass;
    reg [6:0] position;
    wire modifier_pass = pass == 2'd1 || pass == 2'd2;
    wire [7:0] bit_index = modifier_pass ? {5'b10000, position[2:0]} : {1'b0, position};
    wire want_press = pass[1];
    wire differs = stable[bit_index] != sent[bit_index] && stable[bit_index] == want_press;
    wire [9:0] entry = set2(bit_index);
    wire pass_end = modifier_pass ? position[2:0] == 3'd7 : position == 7'd127;
    wire start = !key_valid && diffing && differs && length(entry[9:8], entry[7:0], want_press) != 0;
    wire silent = !key_valid && diffing && differs && !start;   // no code, or Pause's release
    wire take = !key_valid && !diffing && age == STABLE_US && observed != stable;
    // The sequence being sent.
    reg [1:0] kind;
    reg [7:0] code;
    reg make, repeat_send;
    reg [7:0] sending_bit;
    reg [2:0] step;
    assign key_code = sequence_byte(kind, code, make, step);
    wire last_byte = key_valid && key_ready && 4'(step) + 4'd1 == length(kind, code, make);
    // Typematic
    reg repeating, waiting_delay, repeat_due;
    reg [7:0] repeat_bit;
    reg [21:0] elapsed;
    wire repeat_start = !key_valid && !diffing && !take && repeat_due;
    wire pressed = last_byte && !repeat_send && make && !sending_bit[7];
    wire released = (last_byte && !repeat_send && !make && sending_bit == repeat_bit) ||
                    (silent && !want_press && bit_index == repeat_bit);
    wire [21:0] limit = waiting_delay ? delay_thirds(typematic[6:5]) : period_thirds(typematic[4:0]);
    wire [21:0] elapsed_next = elapsed + 22'd3;

    always @(posedge clk) begin
        if (reset) begin
            observed <= 0; stable <= 0; sent <= 0; age <= 0;
            diffing <= 0; pass <= 0; position <= 0;
            key_valid <= 0; kind <= NONE; code <= 0; make <= 0; repeat_send <= 0; sending_bit <= 0; step <= 0;
        end else begin
            // Stability
            if (rows[135:0] != observed) begin
                observed <= rows[135:0];
                age <= 0;
            end else if (us_tick && age != STABLE_US) age <= age + 10'd1;

            if (key_valid && key_ready) begin
                step <= step + 3'd1;
                if (last_byte) begin
                    key_valid <= 0;
                    if (!repeat_send) sent[sending_bit] <= make;
                end
            end
            if (start) begin
                key_valid <= 1;
                {kind, code} <= entry;
                make <= want_press;
                repeat_send <= 0;
                sending_bit <= bit_index;
                step <= 0;
            end
            if (silent) sent[bit_index] <= want_press;
            if (!key_valid && diffing) begin
                position <= pass_end ? 7'd0 : position + 7'd1;
                if (pass_end) begin
                    pass <= pass + 2'd1;
                    if (pass == 2'd3) diffing <= 0;
                end
            end
            if (take) begin
                stable <= observed;
                diffing <= 1;
                pass <= 0;
                position <= 0;
            end
            if (repeat_start) begin
                key_valid <= 1;
                {kind, code} <= set2(repeat_bit);
                make <= 1;
                repeat_send <= 1;
                sending_bit <= repeat_bit;
                step <= 0;
            end
        end
    end

    // Typematic timing, from the end of the press's make. A stop wins over a
    // new press, which wins over the timer.
    always @(posedge clk) begin
        if (reset || typematic_clear || released) begin
            repeating <= 0; repeat_due <= 0;
            if (reset) begin waiting_delay <= 0; repeat_bit <= 0; elapsed <= 0; end
        end else if (pressed) begin
            // A key press: it repeats unless it is Pause.
            repeating <= !(kind == SPECIAL && code == PAUSE);
            repeat_bit <= sending_bit;
            waiting_delay <= 1;
            repeat_due <= 0;
            elapsed <= 0;
        end else begin
            if (repeat_start) repeat_due <= 0;
            if (repeating && us_tick) begin
                if (elapsed_next >= limit) begin
                    elapsed <= elapsed_next - limit;
                    waiting_delay <= 0;
                    repeat_due <= 1;
                end else elapsed <= elapsed_next;
            end
        end
    end
endmodule
