module pipeline_top(input wire clk,clk_forward,clk_capture,clk_handoff,cold_reset,reset,memory_valid,write_cycle,
 input wire [19:0] address,input wire [7:0] wdata,input wire [15:0] dq_sample,
 output wire initialized,memory_ready,memory_done,output wire [7:0] memory_rdata,
 output wire sdram_clk,sdram_cke,sdram_ncs,sdram_nras,sdram_ncas,sdram_nwe,dq_oe,
 output wire [12:0] sdram_a,output wire [15:0] dq_out,
 output wire sample_enable,
 output wire [4:0] state,output wire [15:0] captured,staged,consumed);
 reg [15:0] pin_capture;
 always @(posedge clk_capture) pin_capture <= dq_sample;
 pcxt_memory #(.SDRAM_CAPTURE_PIPELINE(1)) memory(.clk(clk),.clk_pin(clk_forward),.clk_handoff(clk_handoff),
 .cold_reset(cold_reset),.reset(reset),.memory_valid(memory_valid),
 .memory_ready(memory_ready),.address(address),.wdata(wdata),.write_cycle(write_cycle),
 .memory_done(memory_done),.memory_rdata(memory_rdata),.initialized(initialized),
 .firmware_valid(1'b0),.firmware_address(16'b0),.firmware_data(8'b0),
 .disk_load_valid(1'b0),.disk_load_address(19'b0),.disk_load_data(8'b0),
 .media_read_valid(1'b0),.media_read_reset_safe(1'b0),.media_read_addr(19'b0),
 .dq_rise(pin_capture),.dq_fall(pin_capture),
 .sdram_clk(sdram_clk),.sdram_cke(sdram_cke),.sdram_ncs(sdram_ncs),.sdram_nras(sdram_nras),
 .sdram_ncas(sdram_ncas),.sdram_nwe(sdram_nwe),.sdram_a(sdram_a),.dq_out(dq_out),.dq_oe(dq_oe));
 assign state=memory.controller.state;
 assign captured=pin_capture;
 assign staged=memory.capture_handoff.pipeline.read_word;
 assign sample_enable=memory.capture_handoff.pipeline.read_window[3];
 assign consumed=memory.controller_data;
endmodule
