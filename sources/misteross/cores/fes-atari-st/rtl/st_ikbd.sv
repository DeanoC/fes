// SPDX-License-Identifier: GPL-3.0-or-later
// Original byte-protocol IKBD, without an HD6301 firmware image. Standard
// key make/break, relative/absolute mouse, joystick events/interrogation,
// mode inquiries and BCD clock registers follow Atari's protocol:
// https://www.kernel.org/doc/html/latest/input/devices/atarikbd.html
// Original Atari Corp. Intelligent Keyboard Protocol, 26 February 1985,
// sections 8 and 9.15-9.17, also scanned at:
// https://bitsavers.org/pdf/atari/ST/Atari_ST_GEM_Programming_1986/GEM_0883.pdf
// USB HID rows use the existing FES computer layout: usages 0..127 and
// modifier bits 128..135. Mouse buttons[0]=left, [1]=right. Mouse transport
// is an internal future-facing port, not a new FES host ABI.
// Monitor/keycode commands consume all parameters and select quiet modes;
// periodic joystick/fire sampling and accelerated cursor modes are absent.
module st_ikbd #(
    parameter integer SYSTEM_CLOCK_HZ = 52_224_000
) (
    input wire clk, reset,
    input wire command_valid,
    input wire [7:0] command_data,
    output wire command_ready,
    output wire response_valid,
    output wire [7:0] response_data,
    input wire response_ready,
    input wire [143:0] keyboard,
    input wire [15:0] controller_buttons,
    input wire mouse_valid,
    input wire signed [15:0] mouse_dx, mouse_dy,
    input wire [1:0] mouse_buttons,
    output wire mouse_ready
);
    localparam integer FIFO_DEPTH = 64;
    reg [7:0] fifo [0:FIFO_DEPTH-1];
    reg [5:0] head, tail;
    reg [6:0] count;
    reg [127:0] emitted_keys;
    reg [127:0] wanted_keys;
    reg [7:0] command;
    reg [2:0] remaining, parameter_index;
    reg [7:0] parameters [0:5];
    reg [7:0] args [0:5];
    reg paused, mouse_enabled, y_bottom, port0_joystick;
    reg [1:0] mouse_mode;
    reg [2:0] joystick_mode;
    reg [7:0] button_action, threshold_x, threshold_y, scale_x, scale_y;
    reg [7:0] mouse_key_x, mouse_key_y, joystick_rate;
    reg [7:0] joystick_key [0:5];
    reg [15:0] maximum_x, maximum_y, position_x, position_y;
    reg signed [31:0] relative_x, relative_y;
    reg [1:0] current_buttons, emitted_buttons;
    reg [3:0] button_events;
    reg [7:0] emitted_joystick0, emitted_joystick1;
    reg [7:0] clock_bcd [0:5];
    reg [31:0] second_phase;
    wire second_tick = second_phase == 32'(SYSTEM_CLOCK_HZ-1);
    // FES controller 0 remains ST joystick 1; FES controller 1 is ST
    // joystick 0. Each byte keeps up/down/left/right in bits 0..3 and
    // combines the two FES fire buttons into bit 7. Atari's default owns
    // port 0 as a mouse. Joystick commands select both joysticks; mouse
    // commands except Disable Mouse reclaim port 0 and suppress FE events.
    wire [7:0] joystick1 = {controller_buttons[4] | controller_buttons[5],
        3'b000, controller_buttons[3], controller_buttons[2],
        controller_buttons[1], controller_buttons[0]};
    wire [7:0] joystick0 = {controller_buttons[12] | controller_buttons[13],
        3'b000, controller_buttons[11], controller_buttons[10],
        controller_buttons[9], controller_buttons[8]};
    wire mouse_active = mouse_enabled && !port0_joystick;

    function automatic [6:0] hid_scancode(input integer usage);
        case (usage)
            4: hid_scancode='h1e; 5: hid_scancode='h30; 6: hid_scancode='h2e;
            7: hid_scancode='h20; 8: hid_scancode='h12; 9: hid_scancode='h21;
            10: hid_scancode='h22; 11: hid_scancode='h23; 12: hid_scancode='h17;
            13: hid_scancode='h24; 14: hid_scancode='h25; 15: hid_scancode='h26;
            16: hid_scancode='h32; 17: hid_scancode='h31; 18: hid_scancode='h18;
            19: hid_scancode='h19; 20: hid_scancode='h10; 21: hid_scancode='h13;
            22: hid_scancode='h1f; 23: hid_scancode='h14; 24: hid_scancode='h16;
            25: hid_scancode='h2f; 26: hid_scancode='h11; 27: hid_scancode='h2d;
            28: hid_scancode='h15; 29: hid_scancode='h2c;
            30,31,32,33,34,35,36,37,38: hid_scancode=7'(usage-28);
            39: hid_scancode='h0b; 40: hid_scancode='h1c;
            41: hid_scancode='h01; 42: hid_scancode='h0e;
            43: hid_scancode='h0f; 44: hid_scancode='h39;
            45: hid_scancode='h0c; 46: hid_scancode='h0d;
            47: hid_scancode='h1a; 48: hid_scancode='h1b;
            49: hid_scancode='h2b; 50,100: hid_scancode='h60;
            51: hid_scancode='h27; 52: hid_scancode='h28;
            53: hid_scancode='h29; 54: hid_scancode='h33;
            55: hid_scancode='h34; 56: hid_scancode='h35;
            57: hid_scancode='h3a;
            58,59,60,61,62,63,64,65,66,67: hid_scancode=7'(usage+1);
            68: hid_scancode='h61; 69,117: hid_scancode='h62;
            73: hid_scancode='h52; 74: hid_scancode='h47;
            76: hid_scancode='h53; 79: hid_scancode='h4d;
            80: hid_scancode='h4b; 81: hid_scancode='h50;
            82: hid_scancode='h48; 83: hid_scancode='h63;
            84: hid_scancode='h65; 85: hid_scancode='h66;
            86: hid_scancode='h4a; 87: hid_scancode='h4e;
            88: hid_scancode='h72; 89: hid_scancode='h6d;
            90: hid_scancode='h6e; 91: hid_scancode='h6f;
            92: hid_scancode='h6a; 93: hid_scancode='h6b;
            94: hid_scancode='h6c; 95: hid_scancode='h67;
            96: hid_scancode='h68; 97: hid_scancode='h69;
            98: hid_scancode='h70; 99: hid_scancode='h71;
            128,132: hid_scancode='h1d; 129: hid_scancode='h2a;
            133: hid_scancode='h36; 130,134: hid_scancode='h38;
            default: hid_scancode=0;
        endcase
    endfunction
    function automatic [2:0] parameter_count(input [7:0] value);
        case (value)
            8'h07,8'h17,8'h80: parameter_count=1;
            8'h0a,8'h0b,8'h0c: parameter_count=2;
            8'h09: parameter_count=4;
            8'h0e: parameter_count=5;
            8'h19,8'h1b: parameter_count=6;
            default: parameter_count=0;
        endcase
    endfunction
    function automatic signed [31:0] clip_motion(input signed [31:0] value);
        if (value > 65535) clip_motion=65535;
        else if (value < -65535) clip_motion=-65535;
        else clip_motion=value;
    endfunction
    function automatic [15:0] clip_position(input signed [31:0] value, input [15:0] maximum);
        if (value < 0) clip_position=0;
        else if (value > $signed({16'd0,maximum})) clip_position=maximum;
        else clip_position=value[15:0];
    endfunction
    function automatic signed [31:0] packet_motion(input signed [31:0] value);
        if (value > 127) packet_motion=127;
        else if (value < -128) packet_motion=-128;
        else packet_motion=value;
    endfunction
    function automatic [7:0] bcd_next(input [7:0] value);
        if (value[3:0] == 9) bcd_next={value[7:4]+4'd1,4'd0};
        else bcd_next=value+1'b1;
    endfunction
    function automatic [7:0] last_day(input [7:0] year, input [7:0] month);
        // IKBD retains a two-digit year. Its divisible-by-four convention
        // applies throughout that 100-year calendar, including year 00.
        case (month)
            8'h04,8'h06,8'h09,8'h11: last_day=8'h30;
            8'h02: last_day=((32'(year[7:4])*10+32'(year[3:0])) % 4) == 0 ? 8'h29 : 8'h28;
            default: last_day=8'h31;
        endcase
    endfunction

    integer i;
    reg key_found;
    reg [6:0] key_index;
    always @* begin
        wanted_keys=0;
        for (integer k=0;k<136;k=k+1)
            if (keyboard[k] && hid_scancode(k) != 0) wanted_keys[hid_scancode(k)]=1;
        key_found=0; key_index=0;
        // Modifiers are sent before the character from one host state update.
        if (wanted_keys['h1d] != emitted_keys['h1d]) begin key_found=1; key_index='h1d; end
        else if (wanted_keys['h2a] != emitted_keys['h2a]) begin key_found=1; key_index='h2a; end
        else if (wanted_keys['h36] != emitted_keys['h36]) begin key_found=1; key_index='h36; end
        else if (wanted_keys['h38] != emitted_keys['h38]) begin key_found=1; key_index='h38; end
        else for (integer k=1;k<128;k=k+1)
            if (!key_found && wanted_keys[k] != emitted_keys[k]) begin
                key_found=1; key_index=7'(k);
            end
        for (integer p=0;p<6;p=p+1) args[p]=parameters[p];
        if (remaining != 0 && parameter_index < 6) args[parameter_index]=command_data;
    end
    wire command_reply = command_data == 8'h0d || command_data == 8'h16 ||
                         command_data == 8'h1c || command_data == 8'h87 ||
                         command_data == 8'h88 || command_data == 8'h89 ||
                         command_data == 8'h8a || command_data == 8'h8b ||
                         command_data == 8'h8c || command_data == 8'h8f ||
                         command_data == 8'h90 || command_data == 8'h92 ||
                         command_data == 8'h94 || command_data == 8'h95 ||
                         command_data == 8'h99 || command_data == 8'h9a;
    // Resume/reset/mode commands remain available even with a full queue.
    // Commands needing an atomic reply wait for eight free byte slots.
    assign command_ready = !reset && (remaining != 0 || !command_reply || count <= 56);
    wire command_accept = command_valid && command_ready;
    wire execute = command_accept && (remaining == 1 ||
                   (remaining == 0 && parameter_count(command_data) == 0));
    wire [7:0] executing_command = remaining == 0 ? command_data : command;
    wire soft_reset = execute && executing_command == 8'h80 && args[0] == 1;
    assign response_valid = count != 0 && !paused && !reset;
    assign response_data = fifo[head];
    wire pop = response_valid && response_ready;
    assign mouse_ready = !reset && !command_accept;
    wire mouse_accept = mouse_ready && mouse_valid;

    reg [7:0] packet [0:7];
    reg [3:0] packet_length;
    reg key_packet, mouse_packet, joystick0_packet, joystick1_packet;
    wire signed [31:0] delta_y = y_bottom ? -32'(mouse_dy) : 32'(mouse_dy);
    wire signed [31:0] report_x = packet_motion(relative_x);
    wire signed [31:0] report_y = packet_motion(relative_y);
    wire motion_due = relative_x >= $signed({24'd0,threshold_x}) ||
                      relative_x <= -$signed({24'd0,threshold_x}) ||
                      relative_y >= $signed({24'd0,threshold_y}) ||
                      relative_y <= -$signed({24'd0,threshold_y});
    always @* begin
        for (integer p=0;p<8;p=p+1) packet[p]=0;
        packet_length=0; key_packet=0; mouse_packet=0; joystick0_packet=0; joystick1_packet=0;
        if (execute) begin
            case (executing_command)
                8'h80: if (args[0] == 1) begin packet[0]=8'hf1; packet_length=1; end
                8'h0d: begin
                    packet_length=6; packet[0]=8'hf7; packet[1]={4'd0,button_events};
                    packet[2]=position_x[15:8]; packet[3]=position_x[7:0];
                    packet[4]=position_y[15:8]; packet[5]=position_y[7:0];
                end
                8'h16: begin packet_length=3; packet[0]=8'hfd; packet[1]=joystick0; packet[2]=joystick1; end
                8'h1c: begin
                    packet_length=7; packet[0]=8'hfc;
                    for (integer p=0;p<6;p=p+1) packet[p+1]=clock_bcd[p];
                end
                8'h87,8'h88,8'h89,8'h8a,8'h8b,8'h8c,8'h8f,8'h90,8'h92,
                8'h94,8'h95,8'h99,8'h9a: begin
                    packet_length=8; packet[0]=8'hf6;
                    case (executing_command)
                        8'h87: begin packet[1]=8'h07; packet[2]=button_action; end
                        8'h88,8'h89,8'h8a: begin
                            packet[1]=mouse_mode == 0 ? 8'h08 : mouse_mode == 1 ? 8'h09 : 8'h0a;
                            if (mouse_mode == 1) begin
                                packet[2]=maximum_x[15:8]; packet[3]=maximum_x[7:0];
                                packet[4]=maximum_y[15:8]; packet[5]=maximum_y[7:0];
                            end else if (mouse_mode == 2) begin packet[2]=mouse_key_x; packet[3]=mouse_key_y; end
                        end
                        8'h8b: begin packet[1]=8'h0b; packet[2]=threshold_x; packet[3]=threshold_y; end
                        8'h8c: begin packet[1]=8'h0c; packet[2]=scale_x; packet[3]=scale_y; end
                        8'h8f,8'h90: packet[1]=y_bottom ? 8'h0f : 8'h10;
                        8'h92: packet[1]=mouse_enabled ? 0 : 8'h12;
                        8'h94,8'h95,8'h99: begin
                            packet[1]=joystick_mode == 0 ? 8'h14 : joystick_mode == 1 ? 8'h15 :
                                      joystick_mode == 4 ? 8'h19 : 8'h1a;
                            if (joystick_mode == 4)
                                for (integer p=0;p<6;p=p+1) packet[p+2]=joystick_key[p];
                        end
                        8'h9a: packet[1]=joystick_mode == 5 ? 8'h1a : 0;
                        default: ;
                    endcase
                end
                default: ;
            endcase
        end else if (!command_accept && count <= 56) begin
            if (key_found && joystick_mode != 2 && joystick_mode != 3) begin
                packet_length=1; key_packet=1;
                packet[0]={!wanted_keys[key_index],key_index};
            end else if (mouse_active && mouse_mode == 0 && !mouse_accept &&
                         (motion_due || current_buttons != emitted_buttons)) begin
                packet_length=3; mouse_packet=1;
                packet[0]=8'hf8 | {6'd0,current_buttons[0],current_buttons[1]};
                packet[1]=report_x[7:0]; packet[2]=report_y[7:0];
            end else if (joystick_mode == 0 && port0_joystick && joystick0 != emitted_joystick0) begin
                packet_length=2; joystick0_packet=1; packet[0]=8'hfe; packet[1]=joystick0;
            end else if (joystick_mode == 0 && joystick1 != emitted_joystick1) begin
                packet_length=2; joystick1_packet=1; packet[0]=8'hff; packet[1]=joystick1;
            end
        end
    end

    always @(posedge clk) begin
        if (reset) begin
            head<=0; tail<=0; count<=0; emitted_keys<=0;
            command<=0; remaining<=0; parameter_index<=0;
            paused<=0; mouse_enabled<=1; y_bottom<=0; mouse_mode<=0; joystick_mode<=0; port0_joystick<=0;
            button_action<=0; threshold_x<=1; threshold_y<=1; scale_x<=1; scale_y<=1;
            mouse_key_x<=1; mouse_key_y<=1; joystick_rate<=0;
            maximum_x<=16'hffff; maximum_y<=16'hffff; position_x<=0; position_y<=0;
            relative_x<=0; relative_y<=0; current_buttons<=0; emitted_buttons<=0;
            button_events<=0; emitted_joystick0<=0; emitted_joystick1<=0; second_phase<=0;
            for (i=0;i<6;i=i+1) begin
                parameters[i]<=0; joystick_key[i]<=0;
                clock_bcd[i] <= i == 1 || i == 2 ? 8'h01 : 0;
            end
        end else begin
            second_phase <= second_tick ? 0 : second_phase+1'b1;
            // Advance the controller's independent two-digit BCD calendar.
            // Clock-set accepts each valid BCD field separately.
            if (second_tick) begin
                clock_bcd[5] <= clock_bcd[5] == 8'h59 ? 0 : bcd_next(clock_bcd[5]);
                if (clock_bcd[5] == 8'h59) begin
                    clock_bcd[4] <= clock_bcd[4] == 8'h59 ? 0 : bcd_next(clock_bcd[4]);
                    if (clock_bcd[4] == 8'h59) begin
                        clock_bcd[3] <= clock_bcd[3] == 8'h23 ? 0 : bcd_next(clock_bcd[3]);
                        if (clock_bcd[3] == 8'h23) begin
                            clock_bcd[2] <= clock_bcd[2] == last_day(clock_bcd[0],clock_bcd[1]) ? 8'h01 : bcd_next(clock_bcd[2]);
                            if (clock_bcd[2] == last_day(clock_bcd[0],clock_bcd[1])) begin
                                clock_bcd[1] <= clock_bcd[1] == 8'h12 ? 8'h01 : bcd_next(clock_bcd[1]);
                                if (clock_bcd[1] == 8'h12)
                                    clock_bcd[0] <= clock_bcd[0] == 8'h99 ? 0 : bcd_next(clock_bcd[0]);
                            end
                        end
                    end
                end
            end
            if (pop) head<=head+1'b1;
            for (i=0;i<8;i=i+1)
                if (i < packet_length) fifo[6'(tail+i)]<=packet[i];
            tail <= tail+6'(packet_length);
            count <= count+7'(packet_length)-7'(pop);
            if (key_packet) emitted_keys[key_index]<=wanted_keys[key_index];
            if (joystick0_packet) emitted_joystick0<=joystick0;
            if (joystick1_packet) emitted_joystick1<=joystick1;
            if (mouse_packet) begin
                relative_x<=relative_x-report_x; relative_y<=relative_y-report_y;
                emitted_buttons<=current_buttons;
            end
            if (mouse_accept && !port0_joystick) begin
                current_buttons<=mouse_buttons;
                button_events <= button_events |
                    {(!mouse_buttons[0] && current_buttons[0]),(mouse_buttons[0] && !current_buttons[0]),
                     (!mouse_buttons[1] && current_buttons[1]),(mouse_buttons[1] && !current_buttons[1])};
                if (mouse_active) begin
                    relative_x<=clip_motion(relative_x+32'(mouse_dx));
                    relative_y<=clip_motion(relative_y+delta_y);
                    position_x<=clip_position($signed({16'd0,position_x})+32'(mouse_dx)*$signed({24'd0,scale_x}),maximum_x);
                    position_y<=clip_position($signed({16'd0,position_y})+delta_y*$signed({24'd0,scale_y}),maximum_y);
                end
            end
            // Arrival of any command resumes output even if an inquiry is
            // waiting for FIFO space, so PAUSE cannot deadlock the channel.
            if (command_valid) paused<=0;
            if (command_accept) begin
                paused<=0; // Every command implicitly resumes output.
                if (remaining == 0) begin
                    command<=command_data; remaining<=parameter_count(command_data); parameter_index<=0;
                end else begin
                    parameters[parameter_index]<=command_data;
                    parameter_index<=parameter_index+1'b1; remaining<=remaining-1'b1;
                end
            end
            if (execute) begin
                if (executing_command >= 8'h14 && executing_command <= 8'h1a) begin
                    port0_joystick<=1;
                    relative_x<=0; relative_y<=0;
                end else if (executing_command >= 8'h07 && executing_command <= 8'h10)
                    port0_joystick<=0;
                case (executing_command)
                    8'h07: button_action<=args[0];
                    8'h08: begin mouse_enabled<=1; mouse_mode<=0; relative_x<=0; relative_y<=0; end
                    8'h09: begin
                        mouse_enabled<=1; mouse_mode<=1; position_x<=0; position_y<=0; button_events<=0;
                        maximum_x<={args[0],args[1]}; maximum_y<={args[2],args[3]};
                    end
                    8'h0a: begin mouse_enabled<=1; mouse_mode<=2; mouse_key_x<=args[0]; mouse_key_y<=args[1]; end
                    8'h0b: begin threshold_x<=args[0] == 0 ? 8'd1 : args[0]; threshold_y<=args[1] == 0 ? 8'd1 : args[1]; end
                    8'h0c: begin scale_x<=args[0] == 0 ? 8'd1 : args[0]; scale_y<=args[1] == 0 ? 8'd1 : args[1]; end
                    8'h0d: button_events<=0;
                    8'h0e: begin
                        position_x<=clip_position($signed({16'd0,args[1],args[2]}),maximum_x);
                        position_y<=clip_position($signed({16'd0,args[3],args[4]}),maximum_y);
                    end
                    8'h0f: y_bottom<=1;
                    8'h10: y_bottom<=0;
                    8'h12: mouse_enabled<=0;
                    8'h13: paused<=1;
                    8'h14: joystick_mode<=0;
                    8'h15: joystick_mode<=1;
                    8'h17: begin joystick_mode<=2; joystick_rate<=args[0]; end
                    8'h18: joystick_mode<=3;
                    8'h19: begin
                        joystick_mode<=4;
                        for (i=0;i<6;i=i+1) joystick_key[i]<=args[i];
                    end
                    8'h1a: joystick_mode<=5;
                    8'h1b: for (i=0;i<6;i=i+1)
                        if (args[i][3:0] < 10 && args[i][7:4] < 10) clock_bcd[i]<=args[i];
                    default: ;
                endcase
            end
            if (soft_reset) begin
                // Flush old packets and report firmware version 1. Clock stays.
                fifo[0]<=8'hf1; head<=0; tail<=1; count<=1; emitted_keys<=0;
                paused<=0; mouse_enabled<=1; mouse_mode<=0; joystick_mode<=0; y_bottom<=0; port0_joystick<=0;
                threshold_x<=1; threshold_y<=1; scale_x<=1; scale_y<=1; button_action<=0;
                relative_x<=0; relative_y<=0; position_x<=0; position_y<=0;
                maximum_x<=16'hffff; maximum_y<=16'hffff;
                mouse_key_x<=1; mouse_key_y<=1; joystick_rate<=0;
                current_buttons<=0; emitted_buttons<=0; button_events<=0; emitted_joystick0<=0; emitted_joystick1<=0;
            end
        end
    end
    // Rates are retained for inquiry/extension; no periodic monitor is claimed.
    wire unused_inputs = ^{joystick_rate,controller_buttons[15:14],controller_buttons[7:6]};
endmodule
