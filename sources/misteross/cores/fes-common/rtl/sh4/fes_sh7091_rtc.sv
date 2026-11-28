// SH7091 calendar and alarm clock driven by a 32.768 kHz clock enable.
module fes_sh7091_rtc (
    input  logic        clk, reset, power_reset, tick,
    input  logic        request,
    input  logic [31:2] address,
    input  logic [3:0]  rmask, wstrb,
    input  logic [15:0] wdata,
    output logic        selected, rvalid,
    output logic [31:0] rdata,
    output logic [2:0]  irq,
    output logic        tmu_tick, clock_out
);
    // The calendar and divider persist across processor resets.
    logic [15:0] phase = 0, year = 16'h2000;
    logic [7:0] calendar [0:5] = '{0, 0, 0, 0, 8'h01, 8'h01};
    logic [7:0] alarm [0:5] = '{default:0};
    logic [7:0] control1, control2;
    localparam logic [7:0] FIELD_MASK [0:5] =
        '{8'h7f, 8'h7f, 8'h3f, 8'h07, 8'h3f, 8'h1f};
    logic [3:0] reg_index;
    logic write_byte, write_rcr2, read_phase, phase_reset, count_tick, second_carry;
    logic periodic, adjust, alarm_compare, leap, end_of_month;
    logic minute_carry, hour_carry, day_carry, month_carry;
    logic [7:0] days_in_month;
    logic [5:0] enabled, match_field;

    function automatic logic [7:0] increment_bcd(input logic [7:0] value);
        if (value[3:0] == 9)
            return {value[7:4] + 4'd1, 4'b0};
        return value + 8'd1;
    endfunction

    assign selected = address[31:6] == 26'h3ff2000;
    assign reg_index = address[5:2];
    assign write_byte = !reset && request && selected && wstrb == 4'h1;
    assign write_rcr2 = write_byte && reg_index == 15;
    assign read_phase = request && selected && reg_index == 0 && wstrb == 0 && rmask == 4'h1;
    assign adjust = write_rcr2 && wdata[2];
    assign phase_reset = write_rcr2 && (|wdata[2:1]);
    assign clock_out = control2[3] && phase[0];
    assign tmu_tick = count_tick && phase[0];
    assign count_tick = tick && control2[3];
    assign second_carry = count_tick && (&phase[14:0]);

    // Both BCD digit pairs are divisible by four when the low two binary
    // bits of their decimal value (twice the tens bit, plus ones) are zero.
    assign leap = (({year[4], 1'b0} + year[1:0]) == 0) &&
                  (year[7:0] != 0 || ({year[12], 1'b0} + year[9:8]) == 0);
    always_comb begin
        case (calendar[5])
            8'h02: days_in_month = leap ? 8'h29 : 8'h28;
            8'h04, 8'h06, 8'h09, 8'h11: days_in_month = 8'h30;
            default: days_in_month = 8'h31;
        endcase
        case (control2[6:4])
            1: periodic = &phase[6:0];
            2: periodic = &phase[8:0];
            3: periodic = &phase[10:0];
            4: periodic = &phase[12:0];
            5: periodic = &phase[13:0];
            6: periodic = &phase[14:0];
            7: periodic = &phase;
            default: periodic = 0;
        endcase
    end
    assign end_of_month = calendar[4] == days_in_month;
    assign minute_carry = (second_carry && control2[0] && !phase_reset && calendar[0] == 8'h59) ||
                          (adjust && calendar[0] >= 8'h30);
    assign hour_carry = minute_carry && calendar[1] == 8'h59;
    assign day_carry = hour_carry && calendar[2] == 8'h23;
    assign month_carry = day_carry && end_of_month;

    for (genvar i = 0; i < 6; i++) begin : alarm_match
        assign enabled[i] = alarm[i][7];
        assign match_field[i] = !enabled[i] || alarm[i][6:0] == calendar[i][6:0];
    end
    assign alarm_compare = (|enabled) && (&match_field);
    assign irq = {control1[7] && control1[4], control2[7] && (|control2[6:4]),
                  control1[0] && control1[3]};

    always_ff @(posedge clk) begin
        if (reset) begin
            rvalid <= 0;
            control1 <= 0;
            control2 <= power_reset ? 8'h09 : control2 & 8'h09;
        end else begin
            rvalid <= request && selected && wstrb == 0;
            if (request && selected && wstrb == 0) begin
                rdata <= 0;
                if (rmask == 4'h1) case (reg_index)
                    0: rdata[6:0] <= phase[14:8];
                    1, 2, 3, 4, 5, 6: rdata[7:0] <= calendar[reg_index[2:0] - 3'd1];
                    8, 9, 10, 11, 12, 13: rdata[7:0] <= alarm[reg_index[2:0]];
                    14: rdata[7:0] <= control1;
                    15: rdata[7:0] <= control2;
                    default: ;
                endcase
                if (rmask == 4'h3 && reg_index == 7)
                    rdata[15:0] <= year;
            end

            if (write_byte && reg_index == 14)
                control1 <= (wdata[7:0] & 8'h98) | (control1 & wdata[7:0] & 8'h01);
            if (write_rcr2)
                control2 <= wdata[7:0] & 8'hf9;
            if (count_tick && !phase_reset && periodic)
                control2[7] <= 1;
            if ((second_carry && control2[0]) || (read_phase && count_tick && (&phase[7:0])))
                control1[7] <= 1;
            if (alarm_compare)
                control1[0] <= 1;

        end
    end

    always_ff @(posedge clk) begin
        if (phase_reset)
            phase <= 0;
        else if (count_tick)
            phase <= phase + 16'd1;

        if (adjust || (second_carry && control2[0] && !phase_reset))
            calendar[0] <= adjust || calendar[0] == 8'h59 ? 0 : increment_bcd(calendar[0]);
        if (minute_carry)
            calendar[1] <= hour_carry ? 0 : increment_bcd(calendar[1]);
        if (hour_carry)
            calendar[2] <= day_carry ? 0 : increment_bcd(calendar[2]);
        if (day_carry) begin
            calendar[3] <= calendar[3] == 6 ? 0 : calendar[3] + 8'd1;
            calendar[4] <= end_of_month ? 8'h01 : increment_bcd(calendar[4]);
        end
        if (month_carry) begin
            calendar[5] <= calendar[5] == 8'h12 ? 8'h01 : increment_bcd(calendar[5]);
            if (calendar[5] == 8'h12) begin
                year[7:0] <= year[7:0] == 8'h99 ? 0 : increment_bcd(year[7:0]);
                if (year[7:0] == 8'h99)
                    year[15:8] <= year[15:8] == 8'h99 ? 0 : increment_bcd(year[15:8]);
            end
        end

        if (write_byte) begin
            if (reg_index inside {[1:6]})
                calendar[reg_index[2:0] - 3'd1] <= wdata[7:0] & FIELD_MASK[reg_index[2:0] - 3'd1];
            if (reg_index inside {[8:13]})
                alarm[reg_index[2:0]] <= wdata[7:0] & (FIELD_MASK[reg_index[2:0]] | 8'h80);
        end
        if (!reset && request && selected && reg_index == 7 && wstrb == 4'h3)
            year <= wdata;
        if (reset && power_reset)
            for (int i = 0; i < 6; i++)
                alarm[i][7] <= 0;
    end
endmodule
