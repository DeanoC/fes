// SPDX-License-Identifier: GPL-2.0-or-later
// Original ROM-less game. All state advances on the shared video frame tick.
module fes_catch_game (
    input wire clk, reset, frame_tick,
    input wire [7:0] buttons,
    input wire [9:0] x, y,
    output wire [23:0] rgb,
    output reg sound_toggle = 0,
    output reg [8:0] paddle = 9'd140,
    output reg [8:0] drop_x = 9'd156,
    output reg [7:0] drop_y = 8'd24,
    output reg [5:0] score = 0,
    output reg [1:0] lives = 3
);
    reg [7:0] random_state = 8'h5a;
    reg restart_down = 0;
    wire restart = buttons[4] && !restart_down;
    wire [8:0] next_drop = 9'd16 + {1'b0, random_state};
    always @(posedge clk) begin
        if (reset) begin
            paddle <= 140; drop_x <= 156; drop_y <= 24;
            score <= 0; lives <= 3; random_state <= 8'h5a;
            restart_down <= 0; sound_toggle <= 0;
        end else if (frame_tick) begin
            restart_down <= buttons[4];
            if (restart) begin
                paddle <= 140; drop_x <= 156; drop_y <= 24;
                score <= 0; lives <= 3; random_state <= 8'h5a;
            end else if (lives != 0) begin
                // Opposing directions cancel; never wrap at either edge.
                if (buttons[2] && !buttons[3]) paddle <= paddle < 4 ? 9'd0 : paddle - 9'd4;
                if (buttons[3] && !buttons[2]) paddle <= paddle > 276 ? 9'd280 : paddle + 9'd4;
                if (drop_y >= 212) begin
                    if (drop_x + 9'd8 > paddle && drop_x < paddle + 9'd40) begin
                        if (score != 63) score <= score + 1'b1;
                        sound_toggle <= !sound_toggle;
                    end else lives <= lives - 1'b1;
                    drop_x <= next_drop; drop_y <= 24;
                    random_state <= {random_state[6:0], random_state[7] ^ random_state[5] ^ random_state[4] ^ random_state[3]};
                end else drop_y <= drop_y + 8'd2;
            end
        end
    end
    wire is_paddle = x >= {1'b0,paddle} && x < {1'b0,paddle}+10'd40 && y >= 220 && y < 228;
    wire is_drop = x >= {1'b0,drop_x} && x < {1'b0,drop_x}+10'd8 && y >= {2'b0,drop_y} && y < {2'b0,drop_y}+10'd8;
    // Six binary score lamps and three life bars keep the example font-free.
    wire score_lamp = x < 96 && y >= 4 && y < 12 && x[3:0] < 12 && score[x[6:4]];
    wire life_bar = x >= 272 && x < 320 && y >= 4 && y < 12 && x[3:0] < 12 && ((x-10'd272)>>4) < {8'b0,lives};
    assign rgb = reset ? 24'h000000 :
                 score_lamp ? 24'hffff40 : life_bar ? 24'h40ff80 :
                 lives == 0 ? 24'h600818 : is_paddle ? 24'h40c0ff :
                 is_drop ? 24'hffffff : 24'h081828;
endmodule
