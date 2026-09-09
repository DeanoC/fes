// SPDX-License-Identifier: GPL-2.0-or-later
// Standalone 320x240 game. Coordinates are the top-left of each object.
// Board clocks, raster timing and the MiSTer transport belong to the wrapper.
module pong_game #(
    parameter integer CLOCK_HZ = 50000000
) (
    input logic clk, reset, frame_tick, up, down, start,
    input logic freeze,
    input logic [1:0] paddle_speed,
    input logic [9:0] pixel_x, pixel_y,
    output logic [7:0] red, green, blue,
    output logic tone, playing,
    output logic signed [9:0] ball_x, ball_y, player_y, ai_y,
    output logic [3:0] player_score, ai_score,
    output logic player_return, point
);
    localparam integer HALF_PERIOD = CLOCK_HZ / 2000;
    localparam integer TONE_LENGTH = CLOCK_HZ / 10;
    logic rightward, downward, start_held;
    logic signed [2:0] vertical_speed;
    integer tone_left, tone_phase;
    logic signed [9:0] next_x, next_y, speed_extended;

    logic signed [9:0] paddle_step;
    always_comb begin
        case (paddle_speed)
            2'd0: paddle_step = 10'sd2;
            2'd2: paddle_step = 10'sd6;
            default: paddle_step = 10'sd4;
        endcase
        speed_extended = {{7{vertical_speed[2]}}, vertical_speed};
        next_x = ball_x + (rightward ? 10'sd3 : -10'sd3);
        next_y = ball_y + (downward ? speed_extended : -speed_extended);
    end

    always_ff @(posedge clk) begin
        player_return <= 1'b0;
        point <= 1'b0;
        if (reset) begin
            ball_x <= 10'sd158; ball_y <= 10'sd118;
            player_y <= 10'sd104; ai_y <= 10'sd104;
            player_score <= 0; ai_score <= 0;
            playing <= 0; rightward <= 0; downward <= 1;
            tone <= 0; tone_left <= 0; tone_phase <= 0;
            start_held <= 0;
            vertical_speed <= 3'sd2;
        end else if (!freeze) begin
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
                if (up && !down) player_y <= (player_y < paddle_step) ? 10'sd0 : player_y - paddle_step;
                if (down && !up) player_y <= (player_y > 10'sd208 - paddle_step) ? 10'sd208 : player_y + paddle_step;
                if (!playing) begin
                    if (start && !start_held) playing <= 1;
                end else begin
                    if (ai_y + 10'sd16 < ball_y + 10'sd2 && ai_y < 10'sd208) ai_y <= ai_y + 10'sd1;
                    if (ai_y + 10'sd16 > ball_y + 10'sd2 && ai_y > 10'sd0) ai_y <= ai_y - 10'sd1;
                    ball_x <= next_x;
                    ball_y <= next_y;
                    if (next_y <= 10'sd0 || next_y >= 10'sd236) begin
                        ball_y <= (next_y <= 10'sd0) ? 10'sd0 : 10'sd236;
                        downward <= !downward;
                        tone_left <= TONE_LENGTH;
                    end
                    if (!rightward && ball_x >= 10'sd16 && next_x <= 10'sd16 &&
                        next_y + 10'sd4 > player_y && next_y < player_y + 10'sd32) begin
                        player_return <= 1'b1;
                        ball_x <= 10'sd16; rightward <= 1;
                        downward <= (next_y + 10'sd2 >= player_y + 10'sd16);
                        vertical_speed <= (next_y + 10'sd2 < player_y + 10'sd8 ||
                            next_y + 10'sd2 >= player_y + 10'sd24) ? 3'sd3 : 3'sd1;
                        tone_left <= TONE_LENGTH;
                    end
                    if (rightward && ball_x + 10'sd4 <= 10'sd304 &&
                        next_x + 10'sd4 >= 10'sd304 && next_y + 10'sd4 > ai_y &&
                        next_y < ai_y + 10'sd32) begin
                        ball_x <= 10'sd300; rightward <= 0;
                        downward <= (next_y + 10'sd2 >= ai_y + 10'sd16);
                        vertical_speed <= (next_y + 10'sd2 < ai_y + 10'sd8 ||
                            next_y + 10'sd2 >= ai_y + 10'sd24) ? 3'sd3 : 3'sd1;
                        tone_left <= TONE_LENGTH;
                    end
                    if (next_x < 10'sd0 || next_x > 10'sd316) begin
                        point <= 1'b1;
                        if (next_x < 10'sd0)
                            ai_score <= (ai_score == 9) ? 0 : ai_score + 1'b1;
                        else player_score <= (player_score == 9) ? 0 : player_score + 1'b1;
                        ball_x <= 10'sd158; ball_y <= 10'sd118;
                        rightward <= 0; downward <= 1; playing <= 0; vertical_speed <= 3'sd2;
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
            (pixel_x>=12 && pixel_x<16 && $signed(pixel_y)>=player_y &&
                $signed(pixel_y)<player_y+10'sd32) ||
            (pixel_x>=304 && pixel_x<308 && $signed(pixel_y)>=ai_y &&
                $signed(pixel_y)<ai_y+10'sd32) ||
            ($signed(pixel_x)>=ball_x && $signed(pixel_x)<ball_x+10'sd4 &&
                $signed(pixel_y)>=ball_y && $signed(pixel_y)<ball_y+10'sd4) ||
            digit(player_score, int'(pixel_x)-140, int'(pixel_y)-8) ||
            digit(ai_score, int'(pixel_x)-174, int'(pixel_y)-8))) begin
            red=255; green=255; blue=255;
        end
    end
endmodule
