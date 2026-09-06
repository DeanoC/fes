// SPDX-License-Identifier: GPL-2.0-or-later
// Standalone 320x240 game. Coordinates are the top-left of each object.
// Board clocks, raster timing and the MiSTer transport belong to the wrapper.
module pong_game #(
    parameter integer CLOCK_HZ = 50000000
) (
    input logic clk, reset, frame_tick, up, down, start,
    input logic [9:0] pixel_x, pixel_y,
    output logic [7:0] red, green, blue,
    output logic tone, playing,
    output integer ball_x, ball_y, player_y, ai_y,
    output logic [3:0] player_score, ai_score
);
    localparam integer HALF_PERIOD = CLOCK_HZ / 2000;
    localparam integer TONE_LENGTH = CLOCK_HZ / 10;
    logic rightward, downward, start_held;
    integer vertical_speed;
    integer tone_left, tone_phase;
    integer next_x, next_y;

    always_comb begin
        next_x = ball_x + (rightward ? 3 : -3);
        next_y = ball_y + (downward ? vertical_speed : -vertical_speed);
    end

    always_ff @(posedge clk) begin
        if (reset) begin
            ball_x <= 158; ball_y <= 118;
            player_y <= 104; ai_y <= 104;
            player_score <= 0; ai_score <= 0;
            playing <= 0; rightward <= 0; downward <= 1;
            tone <= 0; tone_left <= 0; tone_phase <= 0;
            start_held <= 0;
            vertical_speed <= 2;
        end else begin
            if (tone_left > 0) begin
                tone_left <= tone_left - 1;
                if (tone_phase == HALF_PERIOD - 1) begin
                    tone <= ~tone;
                    tone_phase <= 0;
                end else tone_phase <= tone_phase + 1;
            end else begin
                tone <= 0; tone_phase <= 0;
            end
            if (frame_tick) begin
                start_held <= start;
                if (up && !down) player_y <= (player_y < 4) ? 0 : player_y - 4;
                if (down && !up) player_y <= (player_y > 204) ? 208 : player_y + 4;
                if (!playing) begin
                    if (start && !start_held) playing <= 1;
                end else begin
                    if (ai_y + 16 < ball_y + 2 && ai_y < 208) ai_y <= ai_y + 1;
                    if (ai_y + 16 > ball_y + 2 && ai_y > 0) ai_y <= ai_y - 1;
                    ball_x <= next_x;
                    ball_y <= next_y;
                    if (next_y <= 0 || next_y >= 236) begin
                        ball_y <= (next_y <= 0) ? 0 : 236;
                        downward <= !downward;
                        tone_left <= TONE_LENGTH;
                    end
                    if (!rightward && ball_x >= 16 && next_x <= 16 &&
                        next_y + 4 > player_y && next_y < player_y + 32) begin
                        ball_x <= 16; rightward <= 1;
                        downward <= (next_y + 2 >= player_y + 16);
                        vertical_speed <= (next_y + 2 < player_y + 8 || next_y + 2 >= player_y + 24) ? 3 : 1;
                        tone_left <= TONE_LENGTH;
                    end
                    if (rightward && ball_x + 4 <= 304 && next_x + 4 >= 304 &&
                        next_y + 4 > ai_y && next_y < ai_y + 32) begin
                        ball_x <= 300; rightward <= 0;
                        downward <= (next_y + 2 >= ai_y + 16);
                        vertical_speed <= (next_y + 2 < ai_y + 8 || next_y + 2 >= ai_y + 24) ? 3 : 1;
                        tone_left <= TONE_LENGTH;
                    end
                    if (next_x < 0 || next_x > 316) begin
                        if (next_x < 0)
                            ai_score <= (ai_score == 9) ? 0 : ai_score + 1'b1;
                        else player_score <= (player_score == 9) ? 0 : player_score + 1'b1;
                        ball_x <= 158; ball_y <= 118;
                        rightward <= 0; downward <= 1; playing <= 0; vertical_speed <= 2;
                        tone_left <= TONE_LENGTH;
                    end
                end
            end
        end
    end

    // Seven-segment decimal score, 6x10 pixels. Scores wrap after nine.
    function automatic logic digit(input logic [3:0] value, input integer x, y);
        logic [6:0] segments;
        begin
            case (value)
                0: segments=7'b1111110; 1: segments=7'b0110000;
                2: segments=7'b1101101; 3: segments=7'b1111001;
                4: segments=7'b0110011; 5: segments=7'b1011011;
                6: segments=7'b1011111; 7: segments=7'b1110000;
                8: segments=7'b1111111; 9: segments=7'b1111011;
                default: segments=0;
            endcase
            digit = x>=0 && x<6 && y>=0 && y<10 && (
                (segments[6] && y==0) || (segments[3] && y==9) ||
                (segments[0] && y==4) ||
                (segments[5] && x==5 && y<5) ||
                (segments[4] && x==5 && y>=5) ||
                (segments[2] && x==0 && y>=5) ||
                (segments[1] && x==0 && y<5));
        end
    endfunction

    always_comb begin
        red=0; green=0; blue=0;
        if (pixel_x < 320 && pixel_y < 240 && (
            (pixel_x>=12 && pixel_x<16 && int'(pixel_y)>=player_y && int'(pixel_y)<player_y+32) ||
            (pixel_x>=304 && pixel_x<308 && int'(pixel_y)>=ai_y && int'(pixel_y)<ai_y+32) ||
            (int'(pixel_x)>=ball_x && int'(pixel_x)<ball_x+4 && int'(pixel_y)>=ball_y && int'(pixel_y)<ball_y+4) ||
            digit(player_score, int'(pixel_x)-140, int'(pixel_y)-8) ||
            digit(ai_score, int'(pixel_x)-174, int'(pixel_y)-8))) begin
            red=255; green=255; blue=255;
        end
    end
endmodule
