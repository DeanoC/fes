// SPDX-License-Identifier: MIT
// Cycle-boundary collision latches, deferred read clear and interrupt sources.
module c64_vic_collisions(input wire clk,reset,group_tick,read_sample,read_bg,
 input wire ack,input wire [3:0] ack_mask,other_events,enable,
 input wire [7:0] ss_events,bg_events,
 output reg [7:0] ss_mask,bg_mask,output reg [3:0] flags,
 output wire [7:0] read_value,status,output wire irq);
 reg [1:0] clear_pending;
 wire [1:0] selected_clear=read_sample?(read_bg?2'd2:2'd1):clear_pending;
 reg [7:0] next_ss,next_bg;reg [3:0] next_flags;
 assign read_value=read_bg?bg_mask:ss_mask;
 assign irq=|(flags&enable);assign status={irq,3'b111,flags};
 always @*begin
  next_ss=ss_mask;next_bg=bg_mask;next_flags=(ack?(flags&~ack_mask):flags)|other_events;
  if(group_tick)begin
   next_ss=ss_mask|ss_events;next_bg=bg_mask|bg_events;
   if(selected_clear==1)next_ss=0;
   if(selected_clear==2)next_bg=0;
   if(ss_mask==0&&next_ss!=0)next_flags[2]=1;
   if(bg_mask==0&&next_bg!=0)next_flags[1]=1;
  end
 end
 always @(posedge clk)begin
  if(reset)begin ss_mask<=0;bg_mask<=0;flags<=0;clear_pending<=0;end
  else begin
   ss_mask<=next_ss;bg_mask<=next_bg;flags<=next_flags;
   if(group_tick)clear_pending<=0;
   else if(read_sample)clear_pending<=selected_clear;
  end
 end
endmodule
