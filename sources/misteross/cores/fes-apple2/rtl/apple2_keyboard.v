// SPDX-License-Identifier: GPL-2.0-or-later
// USB HID key state (fes.keyboard.hid 1.0) to the Apple II ASCII keyboard.
//
// The Apple II keyboard encodes a key press as a 7-bit ASCII code with a
// strobe. This adapter produces the character printed on the host key: letters
// are always upper case (the II+ has no lower case), shifted symbols follow the
// US layout, and the brace/bar/tilde symbols fold onto the upper-case codes
// the II+ can display. Control with a letter or @[\]^_ gives codes $00-$1F.
// Return, Escape, Tab, Backspace (left arrow, $08), the arrows ($08, $15,
// $0B, $0A) and Delete ($7F) have their Apple codes. A held key repeats after
// 0.5 s at 15 Hz (the II+ REPT key). Control+F12 or Control+Pause is the
// RESET key; the Alt keys are the Open/Closed-Apple push buttons 0 and 1.
//
// The host writes a changed state as several row transactions, so the rows
// must be quiet for QUIET_CLOCKS (1 ms) before a change is interpreted; a key
// and its modifiers therefore always arrive together. Usages are then scanned
// one per clock (128 system clocks per pass). A newly pressed key is detected
// against the previous scan; two keys pressed in one update produce two
// events in usage order.
module apple2_keyboard #(
    parameter integer QUIET_CLOCKS = 52_224,
    parameter integer REPEAT_DELAY_CLOCKS = 26_112_000,
    parameter integer REPEAT_RATE_CLOCKS = 3_481_600
) (
    input  wire         clk,
    input  wire         reset,
    input  wire [143:0] rows,
    output reg          key_event,
    output reg  [6:0]   key_code,
    output wire         reset_key,
    output wire [1:0]   apple_keys
);
    wire [7:0] modifiers = rows[135:128];
    wire control = modifiers[0] | modifiers[4];
    wire shift = modifiers[1] | modifiers[5];
    assign apple_keys = {modifiers[6], modifiers[2]};
    assign reset_key = control && (rows[8'h45] || rows[8'h48]);

    // Returns {valid, code}.
    function [7:0] translate;
        input [6:0] usage;
        input shifted;
        input ctrl;
        reg [6:0] ascii;
        reg valid;
        begin
            valid = 1'b1;
            ascii = 7'h00;
            if (usage >= 7'h04 && usage <= 7'h1d)
                ascii = 7'h41 + (usage - 7'h04);
            else if (usage >= 7'h1e && usage <= 7'h26 && !shifted)
                ascii = 7'h31 + (usage - 7'h1e);
            else if (usage >= 7'h59 && usage <= 7'h61)
                ascii = 7'h31 + (usage - 7'h59);
            else begin
                case (usage)
                    7'h1e: ascii = 7'h21;                        // !
                    7'h1f: ascii = 7'h40;                        // @
                    7'h20: ascii = 7'h23;                        // #
                    7'h21: ascii = 7'h24;                        // $
                    7'h22: ascii = 7'h25;                        // %
                    7'h23: ascii = 7'h5e;                        // ^
                    7'h24: ascii = 7'h26;                        // &
                    7'h25: ascii = 7'h2a;                        // *
                    7'h26: ascii = 7'h28;                        // (
                    7'h27: ascii = shifted ? 7'h29 : 7'h30;      // 0 )
                    7'h62: ascii = 7'h30;                        // keypad 0
                    7'h28, 7'h58: ascii = 7'h0d;                 // Return
                    7'h29: ascii = 7'h1b;                        // Escape
                    7'h2a, 7'h50: ascii = 7'h08;                 // Backspace, left
                    7'h2b: ascii = 7'h09;                        // Tab
                    7'h2c: ascii = 7'h20;                        // Space
                    7'h2d: ascii = shifted ? 7'h5f : 7'h2d;      // - _
                    7'h2e: ascii = shifted ? 7'h2b : 7'h3d;      // = +
                    7'h2f: ascii = 7'h5b;                        // [ {
                    7'h30: ascii = 7'h5d;                        // ] }
                    7'h31: ascii = 7'h5c;                        // \ |
                    7'h33: ascii = shifted ? 7'h3a : 7'h3b;      // ; :
                    7'h34: ascii = shifted ? 7'h22 : 7'h27;      // ' "
                    7'h35: ascii = 7'h5e;                        // ` ~
                    7'h36: ascii = shifted ? 7'h3c : 7'h2c;      // , <
                    7'h37: ascii = shifted ? 7'h3e : 7'h2e;      // . >
                    7'h38: ascii = shifted ? 7'h3f : 7'h2f;      // / ?
                    7'h4c: ascii = 7'h7f;                        // Delete
                    7'h4f: ascii = 7'h15;                        // right arrow
                    7'h51: ascii = 7'h0a;                        // down arrow
                    7'h52: ascii = 7'h0b;                        // up arrow
                    7'h54: ascii = 7'h2f;                        // keypad /
                    7'h55: ascii = 7'h2a;                        // keypad *
                    7'h56: ascii = 7'h2d;                        // keypad -
                    7'h57: ascii = 7'h2b;                        // keypad +
                    7'h63: ascii = 7'h2e;                        // keypad .
                    default: valid = 1'b0;
                endcase
            end
            // Control folds @A-Z[\]^_ onto $00-$1F; other keys keep their code.
            if (ctrl && valid && ascii >= 7'h40 && ascii <= 7'h5f)
                ascii = ascii & 7'h1f;
            translate = {valid, ascii};
        end
    endfunction

    reg [143:0] observed = 144'd0;
    reg [16:0] quiet = 17'd0;
    wire settled = quiet == 17'd0;
    always @(posedge clk) begin
        observed <= rows;
        if (reset || observed != rows) quiet <= 17'(QUIET_CLOCKS);
        else if (!settled) quiet <= quiet - 17'd1;
    end

    reg [127:0] previous = 128'd0;
    reg [6:0] scan = 7'd0;
    reg [6:0] held_usage = 7'd0;
    reg held = 1'b0;
    reg [24:0] repeat_timer = 25'd0;
    wire [7:0] mapped = translate(scan, shift, control);
    wire [7:0] repeated = translate(held_usage, shift, control);

    always @(posedge clk) begin
        key_event <= 1'b0;
        if (reset) begin
            previous <= 128'd0;
            scan <= 7'd0;
            held <= 1'b0;
            repeat_timer <= 25'd0;
        end else if (settled) begin
            scan <= scan + 7'd1;
            previous[scan] <= rows[scan];
            if (held && !rows[held_usage])
                held <= 1'b0;
            if (rows[scan] && !previous[scan] && mapped[7] && !reset_key) begin
                key_event <= 1'b1;
                key_code <= mapped[6:0];
                held_usage <= scan;
                held <= 1'b1;
                repeat_timer <= 25'(REPEAT_DELAY_CLOCKS);
            end else if (held && rows[held_usage]) begin
                if (repeat_timer == 25'd0) begin
                    key_event <= repeated[7];
                    key_code <= repeated[6:0];
                    repeat_timer <= 25'(REPEAT_RATE_CLOCKS);
                end else begin
                    repeat_timer <= repeat_timer - 25'd1;
                end
            end
        end
    end

    initial begin
        key_event = 1'b0;
        key_code = 7'h00;
    end
endmodule
