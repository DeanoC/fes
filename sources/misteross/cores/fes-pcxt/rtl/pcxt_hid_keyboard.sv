// SPDX-License-Identifier: MIT
// FES HID snapshots to IBM XT set-1 bytes. The 83-key XT has one Ctrl and
// Alt and shares navigation keys with the keypad, so equivalent HID usages
// are combined before diffing. Unsupported enhanced-keyboard usages are ignored.
// A change is accepted only once all nine rows have settled. An eight-entry
// snapshot FIFO preserves settled changes while the cable is busy. If it fills,
// intermediate states coalesce; once space is available the latest stable
// snapshot is queued, so the guest always converges to the host's current keys.
// Releases precede modifier releases, and modifier makes precede key makes.
module pcxt_hid_keyboard #(
    parameter integer STABLE_CYCLES = 100000,
    parameter integer REPEAT_DELAY_CYCLES = 50000000,
    parameter integer REPEAT_PERIOD_CYCLES = 9170000
)(
    input wire clk, reset,
    input wire [143:0] rows,
    output reg code_valid,
    output reg [7:0] code,
    input wire code_ready
);
    localparam integer AGE_W = $clog2(STABLE_CYCLES + 1);
    localparam integer REPEAT_MAX = REPEAT_DELAY_CYCLES > REPEAT_PERIOD_CYCLES ?
                                    REPEAT_DELAY_CYCLES : REPEAT_PERIOD_CYCLES;
    localparam integer REPEAT_W = $clog2(REPEAT_MAX + 1);
    function automatic [135:0] normalize(input [135:0] raw);
        reg [135:0] k;
        begin
            k = raw[135:0];
            // XT has only one physical control and alternate key.
            k[128] = raw[128] | raw[132]; k[132] = 0;
            k[130] = raw[130] | raw[134]; k[134] = 0;
            // The original XT keyboard shares these keys with the keypad.
            k[8'h62] = raw[8'h62] | raw[8'h49]; // insert
            k[8'h5f] = raw[8'h5f] | raw[8'h4a]; // home
            k[8'h61] = raw[8'h61] | raw[8'h4b]; // page up
            k[8'h63] = raw[8'h63] | raw[8'h4c]; // delete
            k[8'h59] = raw[8'h59] | raw[8'h4d]; // end
            k[8'h5b] = raw[8'h5b] | raw[8'h4e]; // page down
            k[8'h5e] = raw[8'h5e] | raw[8'h4f]; // right
            k[8'h5c] = raw[8'h5c] | raw[8'h50]; // left
            k[8'h5a] = raw[8'h5a] | raw[8'h51]; // down
            k[8'h60] = raw[8'h60] | raw[8'h52]; // up
            k[8'h28] = raw[8'h28] | raw[8'h58]; // keypad enter
            k[8'h52:8'h49] = 0; k[8'h58] = 0;
            normalize = k;
        end
    endfunction
    function automatic [7:0] set1(input [7:0] usage);
        begin
            case (usage)
                8'h04:set1=8'h1e; 8'h05:set1=8'h30; 8'h06:set1=8'h2e; 8'h07:set1=8'h20;
                8'h08:set1=8'h12; 8'h09:set1=8'h21; 8'h0a:set1=8'h22; 8'h0b:set1=8'h23;
                8'h0c:set1=8'h17; 8'h0d:set1=8'h24; 8'h0e:set1=8'h25; 8'h0f:set1=8'h26;
                8'h10:set1=8'h32; 8'h11:set1=8'h31; 8'h12:set1=8'h18; 8'h13:set1=8'h19;
                8'h14:set1=8'h10; 8'h15:set1=8'h13; 8'h16:set1=8'h1f; 8'h17:set1=8'h14;
                8'h18:set1=8'h16; 8'h19:set1=8'h2f; 8'h1a:set1=8'h11; 8'h1b:set1=8'h2d;
                8'h1c:set1=8'h15; 8'h1d:set1=8'h2c;
                8'h1e:set1=8'h02; 8'h1f:set1=8'h03; 8'h20:set1=8'h04; 8'h21:set1=8'h05;
                8'h22:set1=8'h06; 8'h23:set1=8'h07; 8'h24:set1=8'h08; 8'h25:set1=8'h09;
                8'h26:set1=8'h0a; 8'h27:set1=8'h0b; 8'h28:set1=8'h1c; 8'h29:set1=8'h01;
                8'h2a:set1=8'h0e; 8'h2b:set1=8'h0f; 8'h2c:set1=8'h39; 8'h2d:set1=8'h0c;
                8'h2e:set1=8'h0d; 8'h2f:set1=8'h1a; 8'h30:set1=8'h1b; 8'h31:set1=8'h2b;
                8'h32:set1=8'h00; 8'h33:set1=8'h27; 8'h34:set1=8'h28; 8'h35:set1=8'h29;
                8'h36:set1=8'h33; 8'h37:set1=8'h34; 8'h38:set1=8'h35; 8'h39:set1=8'h3a;
                8'h3a:set1=8'h3b; 8'h3b:set1=8'h3c; 8'h3c:set1=8'h3d; 8'h3d:set1=8'h3e;
                8'h3e:set1=8'h3f; 8'h3f:set1=8'h40; 8'h40:set1=8'h41; 8'h41:set1=8'h42;
                8'h42:set1=8'h43; 8'h43:set1=8'h44; 8'h47:set1=8'h46; 8'h53:set1=8'h45;
                8'h55:set1=8'h37; 8'h56:set1=8'h4a; 8'h57:set1=8'h4e;
                8'h59:set1=8'h4f; 8'h5a:set1=8'h50; 8'h5b:set1=8'h51; 8'h5c:set1=8'h4b;
                8'h5d:set1=8'h4c; 8'h5e:set1=8'h4d; 8'h5f:set1=8'h47; 8'h60:set1=8'h48;
                8'h61:set1=8'h49; 8'h62:set1=8'h52; 8'h63:set1=8'h53;
                8'h80:set1=8'h1d; 8'h81:set1=8'h2a; 8'h82:set1=8'h38; 8'h85:set1=8'h36;
                default:set1=8'h00;
            endcase
        end
    endfunction

    wire [135:0] normalized = normalize(rows[135:0]);
    wire unused_reserved = &{1'b0, rows[143:136]};
    reg [135:0] observed, stable, sent;
    // Last snapshot accepted into FIFO, not the last raw observation: on
    // overflow a later free slot must still receive the newest settled state.
    reg [135:0] recorded;
    reg [135:0] pending [0:7];
    reg [2:0] fifo_head, fifo_tail;
    reg [3:0] fifo_count;
    reg [AGE_W-1:0] age;
    reg diffing;
    reg [1:0] pass;
    reg [6:0] position;
    wire modifier_pass = pass == 2'd1 || pass == 2'd2;
    wire [7:0] bit_index = modifier_pass ? {5'b10000, position[2:0]} : {1'b0, position};
    wire want_press = pass[1];
    wire differs = stable[bit_index] != sent[bit_index] && stable[bit_index] == want_press;
    wire pass_end = modifier_pass ? position[2:0] == 3'd7 : position == 7'd127;
    wire [7:0] key = set1(bit_index);
    wire dequeue = !code_valid && !diffing && fifo_count != 0;
    wire enqueue = age == AGE_W'(STABLE_CYCLES) && normalized == observed &&
                   observed != recorded && (fifo_count < 4'd8 || dequeue);
    reg [7:0] sending_bit;
    reg sending_make, sending_repeat;
    reg repeat_active, repeat_due, repeat_delay;
    reg [7:0] repeat_bit;
    reg [REPEAT_W-1:0] repeat_count;
    wire [REPEAT_W-1:0] repeat_limit = repeat_delay ? REPEAT_W'(REPEAT_DELAY_CYCLES) : REPEAT_W'(REPEAT_PERIOD_CYCLES);
    wire repeat_start = !code_valid && !diffing && !dequeue && !enqueue && repeat_due;
    always @(posedge clk) begin
        if (reset) begin
            observed <= 0; stable <= 0; sent <= 0; recorded <= 0; age <= 0;
            fifo_head <= 0; fifo_tail <= 0; fifo_count <= 0;
            diffing <= 0; pass <= 0; position <= 0;
            code_valid <= 0; code <= 0; sending_bit <= 0; sending_make <= 0; sending_repeat <= 0;
            repeat_active <= 0; repeat_due <= 0; repeat_delay <= 0; repeat_bit <= 0; repeat_count <= 0;
        end else begin
            if (normalized != observed) begin observed <= normalized; age <= 0; end
            else if (age != AGE_W'(STABLE_CYCLES)) age <= age + 1'b1;
            if (repeat_active) begin
                if (repeat_count + 1'b1 >= repeat_limit) begin
                    repeat_count <= 0; repeat_delay <= 0; repeat_due <= 1;
                end else repeat_count <= repeat_count + 1'b1;
            end
            if (code_valid && code_ready) begin
                code_valid <= 0;
                if (!sending_repeat) begin
                    sent[sending_bit] <= sending_make;
                    if (!sending_bit[7] && sending_make) begin
                        repeat_active <= 1; repeat_bit <= sending_bit;
                        repeat_due <= 0; repeat_delay <= 1; repeat_count <= 0;
                    end else if (!sending_make && sending_bit == repeat_bit) begin
                        repeat_active <= 0; repeat_due <= 0;
                    end
                end
            end
            if (!code_valid && diffing) begin
                if (differs) begin
                    if (key != 0) begin
                        code <= key | (want_press ? 8'h00 : 8'h80);
                        code_valid <= 1; sending_bit <= bit_index;
                        sending_make <= want_press; sending_repeat <= 0;
                    end else sent[bit_index] <= want_press;
                end
                position <= pass_end ? 7'd0 : position + 7'd1;
                if (pass_end) begin
                    pass <= pass + 1'b1;
                    if (pass == 2'd3) diffing <= 0;
                end
            end
            if (enqueue) begin
                pending[fifo_tail] <= observed; recorded <= observed; fifo_tail <= fifo_tail + 1'b1;
            end
            if (dequeue) begin
                stable <= pending[fifo_head]; fifo_head <= fifo_head + 1'b1;
                diffing <= 1; pass <= 0; position <= 0;
            end
            case ({enqueue,dequeue})
                2'b10: fifo_count <= fifo_count + 1'b1;
                2'b01: fifo_count <= fifo_count - 1'b1;
                default: ;
            endcase
            if (repeat_start) begin
                code <= set1(repeat_bit); code_valid <= 1; sending_repeat <= 1;
                sending_bit <= repeat_bit; sending_make <= 1; repeat_due <= 0;
            end
        end
    end
endmodule
