// SPDX-License-Identifier: MIT
// MC146818A-compatible clock with 128 bytes of CMOS
// (docs/pc386-machine-design.md, "RTC and CMOS").
//
// 70h selects a register (bit 7 is the NMI mask, stored); 71h reads and writes
// it. The time is kept in binary and converted to and from the guest's format
// (register B's DM and 24/12 bits) at each access. Only the 32.768 kHz divider
// (DV = 010) runs; DV = 11x holds the chain in reset and the first update
// follows half a second after it is released, as the data sheet says; any
// other DV stops it. UIP rises 244 us (eight ticks) before each once-a-second
// update. PF, AF and UF set IRQF, and IRQ8, when their enable is set; reading
// C clears them. DSE is stored but never adjusts the clock (recorded deviation).
//
// CMOS 0Eh-7Fh persists in the fes.pc386.machine record (fes_computer_storage):
// after a machine reset the RTC loads those bytes from the record's words
// (record_load, the pair at record_pair a clock later), writes the
// configuration bytes from the config inputs (10h, 12h, 14h-18h, 19h-23h,
// 2Dh, 30h-31h, 34h-35h, 38h-3Dh, 3Fh, 60h-62h), then the 10h-2Dh checksum
// into 2Eh and 2Fh. busy is high until that has finished. Every byte written
// to the RAM, by the setup, the guest or the century, goes to the record as
// well (record_write). 00h-0Dh are the clock's registers, not RAM: the record
// neither gives them nor takes them. The clock is battery-backed, as an
// MC146818 is: a machine reset clears only PIE, AIE, UIE and SQWE and the
// flags, as its RESET pin does, and the time, alarms, A and B carry over.
// The clock does not run while the machine is held; the wall clock sets it.
//
// CMOS is an M10K of 64 byte pairs with registered reads: 71h reads the pair
// at the index, re-read every clock, and the checksum reads one pair ahead
// (a 71h read during the checksum, or on the last configuration clock, returns
// the checksum's pair). It has one write port: setup first (a guest write
// while busy is lost), then the guest, then the century byte, which waits for
// a free clock and gives way to a guest write of 32h.
module pc386_rtc #(
    parameter integer RTC_N = 256,       // 32,768 Hz as N/D of the system clock
    parameter integer RTC_D = 781250
) (
    input wire clk, reset,
    input wire access, write, a0,
    input wire [7:0] wdata,
    output reg [7:0] rdata,
    output wire irq8,
    output reg nmi_mask,
    output wire busy,
    // Machine configuration, written into CMOS after reset.
    input wire [15:0] base_kib, extended_kib, above16m_64k,
    input wire disk_present,
    input wire [15:0] disk_cylinders,
    input wire [7:0] disk_heads, disk_spt,
    input wire [7:0] sb_base_high, sb_irq, sb_dma,
    // fes.rtc.wall-clock: binary fields, taken when valid pulses.
    input wire wall_valid,
    input wire [15:0] wall_year,
    input wire [7:0] wall_month, wall_day, wall_hour, wall_minute, wall_second, wall_weekday,
    // The record's CMOS words: the bytes written here, and the load after reset
    output wire record_write,
    output wire [6:0] record_index,
    output wire [7:0] record_byte,
    output wire record_load,
    output wire [5:0] record_pair,
    input wire [15:0] record_word
);
    reg [6:0] index;
    reg [5:0] second, minute;
    reg [4:0] hour, day;
    reg [3:0] month;
    reg [2:0] weekday;
    reg [6:0] year;
    reg [7:0] alarm_second, alarm_minute, alarm_hour;
    reg [2:0] divider;
    reg [3:0] rate;
    reg [7:0] control_b;
    reg pf, af, uf;
    reg [14:0] chain;
    wire tick;
    wire running = divider == 3'b010;
    wire uip = running && !control_b[7] && chain >= 15'd32760;
    wire irqf = (pf && control_b[6]) || (af && control_b[5]) || (uf && control_b[4]);
    assign irq8 = irqf;

    pc386_rate #(.N(RTC_N), .D(RTC_D)) rtc_rate (.clk(clk), .reset(reset), .tick(tick));

    function automatic [7:0] to_bcd(input [6:0] value);
        to_bcd = {value >= 7'd90 ? 4'd9 : value >= 7'd80 ? 4'd8 : value >= 7'd70 ? 4'd7 :
                  value >= 7'd60 ? 4'd6 : value >= 7'd50 ? 4'd5 : value >= 7'd40 ? 4'd4 :
                  value >= 7'd30 ? 4'd3 : value >= 7'd20 ? 4'd2 : value >= 7'd10 ? 4'd1 : 4'd0,
                  4'(value % 7'd10)};
    endfunction
    function automatic [6:0] from_bcd(input [7:0] value);
        from_bcd = 7'(value[7:4] * 4'd10) + 7'(value[3:0]);
    endfunction
    // One time field in the guest's encoding, and back.
    function automatic [7:0] encode(input [6:0] value);
        encode = control_b[2] ? {1'b0, value} : to_bcd(value);
    endfunction
    function automatic [6:0] decode(input [7:0] value);
        decode = control_b[2] ? value[6:0] : from_bcd(value);
    endfunction
    // Hours: 24-hour, or 1-12 with bit 7 for PM.
    function automatic [7:0] encode_hour(input [4:0] value);
        reg [4:0] twelve;
        reg [7:0] coded;
        begin
            twelve = value == 0 ? 5'd12 : value > 12 ? value - 5'd12 : value;
            coded = encode({2'b0, twelve});
            encode_hour = control_b[1] ? encode({2'b0, value}) : {value >= 12, coded[6:0]};
        end
    endfunction
    function automatic [4:0] decode_hour(input [7:0] value);
        reg [4:0] twelve;
        twelve = 5'(decode({1'b0, value[6:0]}));
        decode_hour = control_b[1] ? 5'(decode(value)) :
                      value[7] ? (twelve == 12 ? 5'd12 : twelve + 5'd12) : (twelve == 12 ? 5'd0 : twelve);
    endfunction
    function automatic [4:0] month_days(input [3:0] m, input [1:0] y);
        case (m)
            4'd2: month_days = y == 0 ? 5'd29 : 5'd28;
            4'd4, 4'd6, 4'd9, 4'd11: month_days = 5'd30;
            default: month_days = 5'd31;
        endcase
    endfunction
    // An alarm byte with both top bits set matches any value.
    function automatic alarm_match(input [7:0] alarm, input [7:0] now);
        alarm_match = alarm[7:6] == 2'b11 || alarm == now;
    endfunction

    // After reset: the record's pairs 07h-3Fh, the configuration writes, then
    // the checksum.
    localparam LOAD = 2'd0, CONFIGURE = 2'd1, CHECKSUM = 2'd2, READY = 2'd3;
    reg [1:0] setup = LOAD;
    reg [6:0] step = 7'd7;
    reg [15:0] sum;
    assign busy = setup != READY;
    assign record_load = !reset && setup == LOAD && step <= 7'd63;
    assign record_pair = step[5:0];
    reg [7:0] config_address, config_value;
    always @* begin
        config_address = 8'hff;
        config_value = 8'h00;
        case (step)
            7'd0: begin config_address = 8'h10; config_value = 8'h00; end          // no floppy drives
            7'd1: begin config_address = 8'h12; config_value = disk_present ? 8'hf0 : 8'h00; end
            7'd2: begin config_address = 8'h14; config_value = 8'h04; end          // VGA, PS/2 port, no x87 or floppy
            7'd3: begin config_address = 8'h15; config_value = base_kib[7:0]; end
            7'd4: begin config_address = 8'h16; config_value = base_kib[15:8]; end
            7'd5: begin config_address = 8'h17; config_value = extended_kib[7:0]; end
            7'd6: begin config_address = 8'h18; config_value = extended_kib[15:8]; end
            7'd7: begin config_address = 8'h19; config_value = disk_present ? 8'd47 : 8'h00; end  // user type
            7'd8: begin config_address = 8'h1a; config_value = 8'h00; end
            7'd9: begin config_address = 8'h1b; config_value = disk_present ? disk_cylinders[7:0] : 8'h00; end
            7'd10: begin config_address = 8'h1c; config_value = disk_present ? disk_cylinders[15:8] : 8'h00; end
            7'd11: begin config_address = 8'h1d; config_value = disk_present ? disk_heads : 8'h00; end
            7'd12: begin config_address = 8'h1e; config_value = disk_present ? 8'hff : 8'h00; end  // no precompensation
            7'd13: begin config_address = 8'h1f; config_value = disk_present ? 8'hff : 8'h00; end
            7'd14: begin config_address = 8'h20; config_value = disk_present ? {4'hc, disk_heads > 8, 3'b0} : 8'h00; end
            7'd15: begin config_address = 8'h21; config_value = disk_present ? disk_cylinders[7:0] : 8'h00; end  // landing zone
            7'd16: begin config_address = 8'h22; config_value = disk_present ? disk_cylinders[15:8] : 8'h00; end
            7'd17: begin config_address = 8'h23; config_value = disk_present ? disk_spt : 8'h00; end
            7'd18: begin config_address = 8'h2d; config_value = 8'h00; end          // boot C: before A:
            7'd19: begin config_address = 8'h30; config_value = extended_kib[7:0]; end
            7'd20: begin config_address = 8'h31; config_value = extended_kib[15:8]; end
            7'd21: begin config_address = 8'h34; config_value = above16m_64k[7:0]; end
            7'd22: begin config_address = 8'h35; config_value = above16m_64k[15:8]; end
            7'd23: begin config_address = 8'h38; config_value = 8'h00; end          // no third boot device
            7'd24: begin config_address = 8'h39; config_value = 8'h00; end          // ATA translation: none
            7'd25: begin config_address = 8'h3a; config_value = 8'h00; end
            7'd26: begin config_address = 8'h3d; config_value = 8'h02; end          // boot from the hard disk only
            7'd27: begin config_address = 8'h3f; config_value = 8'h01; end          // no boot menu
            7'd28: begin config_address = 8'h60; config_value = sb_base_high; end
            7'd29: begin config_address = 8'h61; config_value = sb_irq; end
            7'd30: begin config_address = 8'h62; config_value = sb_dma; end
            default: begin end
        endcase
    end

    wire selected_register = access && a0;
    wire guest_store = selected_register && write && index >= 7'h0e;
    reg century_store;
    reg [7:0] century_byte;
    // CMOS: the record's pairs, the configuration and checksum writes, then
    // the guest's, then the century byte's. Every write but the load is one
    // byte, at byte_index, and goes to the record.
    reg ram_write, byte_write;
    reg [5:0] ram_waddress, ram_raddress;
    reg [15:0] ram_wdata;
    reg [1:0] ram_lanes;
    reg [6:0] byte_index;
    reg [7:0] byte_value;
    assign record_write = byte_write;
    assign record_index = byte_index;
    assign record_byte = byte_value;
    wire [15:0] ram_pair;
    wire [7:0] ram_byte = ram_pair[8 * index[0] +: 8];
    pc386_ram #(.WIDTH(16), .DEPTH(64), .BYTES(2)) ram (
        .clk(clk), .write(ram_write), .waddress(ram_waddress), .wdata(ram_wdata), .wbyteenable(ram_lanes),
        .read(1'b1), .raddress(ram_raddress), .rdata(ram_pair)
    );
    always @* begin
        byte_write = 0;
        byte_index = 7'h00;
        byte_value = 8'h00;
        case (setup)
            LOAD: begin end
            CONFIGURE: begin
                byte_write = !config_address[7];
                byte_index = config_address[6:0];
                byte_value = config_value;
            end
            CHECKSUM: begin
                byte_write = step > 7'h2d;
                byte_index = step == 7'h2e ? 7'h2e : 7'h2f;  // 2Eh high byte, 2Fh low
                byte_value = step == 7'h2e ? sum[15:8] : sum[7:0];
            end
            default: begin
                byte_write = guest_store || century_store;
                byte_index = guest_store ? index : 7'h32;
                byte_value = guest_store ? wdata : century_byte;
            end
        endcase
        // The load writes each pair a clock after its record read.
        ram_write = setup == LOAD ? step >= 7'd8 : byte_write;
        ram_waddress = setup == LOAD ? 6'(step - 7'd1) : byte_index[6:1];
        ram_wdata = setup == LOAD ? record_word : {2{byte_value}};
        ram_lanes = setup == LOAD ? 2'b11 : byte_index[0] ? 2'b10 : 2'b01;
        // The checksum's first pair is read on the last configuration clock.
        ram_raddress = index[6:1];
        if (setup == CONFIGURE && step == 7'd30) ram_raddress = 6'h08;
        else if (setup == CHECKSUM) ram_raddress = 6'(7'(step + 1'b1) >> 1);
    end
    wire [7:0] wall_second_b = wall_second, unused_wall = wall_second_b;
    reg [15:0] century_rest;
    reg [6:0] century;
    reg century_pending;

    always @* begin
        rdata = 8'hff;  // 70h is write-only
        if (a0) case (index)
            7'h00: rdata = encode({1'b0, second});
            7'h01: rdata = alarm_second;
            7'h02: rdata = encode({1'b0, minute});
            7'h03: rdata = alarm_minute;
            7'h04: rdata = encode_hour(hour);
            7'h05: rdata = alarm_hour;
            7'h06: rdata = encode({4'b0, weekday});
            7'h07: rdata = encode({2'b0, day});
            7'h08: rdata = encode({3'b0, month});
            7'h09: rdata = encode(year);
            7'h0a: rdata = {uip, divider, rate};
            7'h0b: rdata = control_b;
            7'h0c: rdata = {irqf, pf, af, uf, 4'b0};
            7'h0d: rdata = 8'h80;  // VRT: the battery is good
            default: rdata = ram_byte;
        endcase
    end

    // The battery-backed state starts at configuration.
    initial begin
        second = 0; minute = 0; hour = 0; day = 1; month = 1; weekday = 7; year = 0;
        alarm_second = 0; alarm_minute = 0; alarm_hour = 0;
        divider = 3'b010; rate = 4'b0110; control_b = 8'h02; chain = 0;
    end

    always @(posedge clk) begin
        if (reset) begin
            index <= 0; nmi_mask <= 0;
            control_b[6:3] <= 4'b0000;  // PIE, AIE, UIE, SQWE
            pf <= 0; af <= 0; uf <= 0;
            setup <= LOAD; step <= 7'd7; sum <= 0;
            century_rest <= 0; century <= 0; century_pending <= 0;
            century_store <= 0; century_byte <= 0;
        end else begin
            case (setup)
                LOAD: begin
                    step <= step + 1'b1;
                    if (step == 7'd64) begin setup <= CONFIGURE; step <= 0; end
                end
                CONFIGURE: begin
                    step <= step + 1'b1;
                    if (step == 7'd30) begin setup <= CHECKSUM; step <= 7'h10; sum <= 0; end
                end
                CHECKSUM: begin
                    if (step <= 7'h2d) sum <= sum + {8'b0, ram_pair[8 * step[0] +: 8]};
                    step <= step + 1'b1;
                    if (step == 7'h2f) setup <= READY;
                end
                default: begin end
            endcase

            if (setup == READY && !guest_store) century_store <= 0;  // written this clock
            // The century byte (32h, BCD) from a wall-clock year.
            if (century_pending) begin
                if (century_rest >= 16'd100) begin
                    century_rest <= century_rest - 16'd100;
                    century <= century + 1'b1;
                end else begin
                    century_store <= 1;
                    century_byte <= to_bcd(century);
                    year <= century_rest[6:0];
                    century_pending <= 0;
                end
            end

            if (guest_store && index == 7'h32) century_store <= 0;

            // Divider chain, periodic flag and the once-a-second update.
            if (tick && running) begin
                chain <= chain + 1'b1;
                if (rate != 0 && (chain + 1'b1 & ((15'd1 << (rate < 3 ? rate + 6 : rate - 1)) - 1'b1)) == 0)
                    pf <= 1;
                if (chain == 15'h7fff && !control_b[7]) begin
                    uf <= 1;
                    if (alarm_match(alarm_second, encode({1'b0, second == 59 ? 6'd0 : second + 1'b1})) &&
                        alarm_match(alarm_minute, encode({1'b0, second == 59 ? (minute == 59 ? 6'd0 : minute + 1'b1) : minute})) &&
                        alarm_match(alarm_hour, encode_hour(second == 59 && minute == 59 ? (hour == 23 ? 5'd0 : hour + 1'b1) : hour)))
                        af <= 1;
                    if (second != 59) second <= second + 1'b1;
                    else begin
                        second <= 0;
                        if (minute != 59) minute <= minute + 1'b1;
                        else begin
                            minute <= 0;
                            if (hour != 23) hour <= hour + 1'b1;
                            else begin
                                hour <= 0;
                                weekday <= weekday == 7 ? 3'd1 : weekday + 1'b1;
                                if (day != month_days(month, year[1:0])) day <= day + 1'b1;
                                else begin
                                    day <= 1;
                                    if (month != 12) month <= month + 1'b1;
                                    else begin
                                        month <= 1;
                                        year <= year == 99 ? 7'd0 : year + 1'b1;
                                    end
                                end
                            end
                        end
                    end
                end
            end

            if (wall_valid) begin
                second <= wall_second[5:0]; minute <= wall_minute[5:0]; hour <= wall_hour[4:0];
                day <= wall_day[4:0]; month <= wall_month[3:0]; weekday <= wall_weekday[2:0];
                century_rest <= wall_year; century <= 0; century_pending <= 1;
                chain <= 0;
            end

            if (access && write && !a0) begin
                index <= wdata[6:0];
                nmi_mask <= wdata[7];
            end
            if (selected_register && !write && index == 7'h0c) begin
                pf <= 0; af <= 0; uf <= 0;
            end
            if (selected_register && write) case (index)
                7'h00: second <= 6'(decode(wdata));
                7'h01: alarm_second <= wdata;
                7'h02: minute <= 6'(decode(wdata));
                7'h03: alarm_minute <= wdata;
                7'h04: hour <= decode_hour(wdata);
                7'h05: alarm_hour <= wdata;
                7'h06: weekday <= 3'(decode(wdata));
                7'h07: day <= 5'(decode(wdata));
                7'h08: month <= 4'(decode(wdata));
                7'h09: year <= decode(wdata);
                7'h0a: begin
                    divider <= wdata[6:4];
                    rate <= wdata[3:0];
                    if (wdata[6:4] == 3'b010 && divider[2:1] == 2'b11) chain <= 15'h4000;
                    else if (wdata[6:5] == 2'b11) chain <= 0;
                end
                7'h0b: control_b <= wdata[7] ? {wdata[7:5], 1'b0, wdata[3:0]} : wdata;  // SET clears UIE
                default: begin end
            endcase
            if (divider[2:1] == 2'b11 &&
                !(selected_register && write && index == 7'h0a && wdata[6:4] == 3'b010)) chain <= 0;
        end
    end
endmodule
