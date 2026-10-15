// SPDX-License-Identifier: MIT
// Exercise clock enables in the actual motherboard, including adjacent pulses.
module clock_sim_top(input wire clk, output wire [5:0] enables, output wire [5:0] prepared, output wire [5:0] envelope_prepared);
    for (genvar i=0;i<6;i=i+1) begin : clocks
        localparam SYS=i==0?52224000:i==1?100:i<5?10:5;
        localparam CPU=i==0?985248:i==1?17:i==2?5:i==3?6:i==4?10:2;
        c64_machine #(.SYSTEM_CLOCK_HZ(SYS),.CPU_CLOCK_HZ(CPU)) machine (
            .clk_sys(clk),.video_clk(clk),.reset(1'b1),.keyboard_rows(144'd0),
            .controller_buttons(16'd0),.media_write_addr(18'd0),.media_write_data(16'd0),
            .media_write_enable(2'd0),.disk_ready(1'b0),.slot1_response(0),.slot2_response(0),
            .slot1_request(),.slot2_request(),.red(),.green(),.blue(),.de(),.hsync(),.vsync(),
            .audio_sample(),.debug_stage(),.debug_error(),.debug_iec(),.debug_pc()
        );
        assign enables[i]=machine.cpu_ce;
        assign prepared[i]=machine.sid.PREPARE_PHASE;
        assign envelope_prepared[i]=machine.sid.PREPARE_ENVELOPE;
    end
endmodule
