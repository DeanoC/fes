// SPDX-License-Identifier: MIT
// Read-only standard 35-track D64 rotating medium. The 1541 CPU/VIA/GCR reader remain external.
// A single GCR track is rebuilt after media insertion or integer-track movement.
// Motor angle and head position survive replacement/ejection. All time is
// advanced by the 16 MHz ref_ce supplied by the drive motherboard.
module c64_d64_medium (
 input wire clk, reset, ref_ce, disk_ready,
 input wire [7:0] drive_pins, drive_ddr,
 output reg [17:0] disk_addr,
 input wire [7:0] disk_data,
 output wire flux,
 output wire cache_ready,
 output reg [6:0] halftrack,
 output reg [21:0] rotation,
 output wire [12:0] debug_byte_addr,
 output wire [7:0] debug_byte
);
 function automatic [1:0] zone(input [5:0] t);
  zone=t<=17?2'd3:t<=24?2'd2:t<=30?2'd1:2'd0;
 endfunction
 function automatic [4:0] sector_count(input [1:0] z);
  case(z) 0:sector_count=17;1:sector_count=18;2:sector_count=19;default:sector_count=21;endcase
 endfunction
 function automatic [17:0] track_start(input [5:0] t);
  if(t<=17)track_start=18'((t-1)*5376);
  else if(t<=24)track_start=18'(91392+(t-18)*4864);
  else if(t<=30)track_start=18'(125440+(t-25)*4608);
  else track_start=18'(153088+(t-31)*4352);
 endfunction
 function automatic [12:0] track_length(input [1:0] z);
  case(z) 0:track_length=6250;1:track_length=6666;2:track_length=7142;default:track_length=7692;endcase
 endfunction
 function automatic [4:0] sector_gap(input [1:0] z);
  case(z) 0:sector_gap=9;1:sector_gap=12;2:sector_gap=17;default:sector_gap=8;endcase
 endfunction
 function automatic [4:0] code(input [3:0] n);
  case(n)
   0:code=10;1:code=11;2:code=18;3:code=19;4:code=14;5:code=15;6:code=22;7:code=23;
   8:code=9;9:code=25;10:code=26;11:code=27;12:code=13;13:code=29;14:code=30;15:code=21;
  endcase
 endfunction
 wire motor=drive_pins[2];
 wire [1:0] step_delta=drive_pins[1:0]-halftrack[1:0];
 wire [5:0] selected_track=halftrack[6:1];
 wire [1:0] selected_zone=zone(selected_track);
 // Four cheap phase counters avoid variable division of the spindle angle.
 reg [5:0] bit_phase[0:3];
 reg [15:0] bit_position[0:3];
 integer z;
 always @(posedge clk) begin
  if(reset)begin
   halftrack<=36;rotation<=0;
   for(z=0;z<4;z=z+1)begin bit_phase[z]<=0;bit_position[z]<=0;end
  end else begin
   if(motor && drive_ddr[2:0]==7)begin
    if(step_delta==1 && halftrack<70)halftrack<=halftrack+1'b1;
    else if(step_delta==3 && halftrack>2)halftrack<=halftrack-1'b1;
   end
   if(ref_ce && motor)begin
    if(rotation==3199999)begin
     rotation<=0;
     for(z=0;z<4;z=z+1)begin bit_phase[z]<=0;bit_position[z]<=0;end
    end else begin
     rotation<=rotation+1'b1;
     for(z=0;z<4;z=z+1)begin
      if(bit_phase[z]==6'(63-z*4))begin bit_phase[z]<=0;bit_position[z]<=bit_position[z]+1'b1;end
      else bit_phase[z]<=bit_phase[z]+1'b1;
     end
    end
   end
  end
 end
 reg [7:0] cache[0:8191];
 reg [7:0] cache_byte;
 assign debug_byte_addr=bit_position[selected_zone][15:3];
 assign debug_byte=cache_byte;
 always @(posedge clk)cache_byte<=cache[debug_byte_addr];
 reg valid;
 reg [5:0] build_track;
 wire [1:0] build_zone=zone(build_track);
 assign cache_ready=valid && disk_ready && build_track==selected_track;
 wire track_tail=debug_byte_addr>=track_length(selected_zone);
 assign flux=ref_ce && motor && cache_ready && bit_phase[selected_zone]==0 &&
             (track_tail ? bit_position[selected_zone][0] : cache_byte[7-bit_position[selected_zone][2:0]]);

 localparam ID_WAIT0=0,ID_GET0=1,ID_WAIT1=2,ID_GET1=3,
            HEADER_SYNC=4,RAW=5,READ_WAIT=6,READ_GET=7,PACK=8,WRITE_GCR=9,
            HEADER_GAP=10,DATA_SYNC=11,SECTOR_GAP=12,TAIL=13,DONE=14;
 reg [3:0] state;
 reg [12:0] write_addr;
 reg [4:0] run_count,sector;
 reg [17:0] sector_base;
 reg [7:0] id1,id2,checksum,raw_byte;
 reg header;
 reg [8:0] raw_index;
 reg [1:0] pack_index;
 reg [2:0] write_index;
 reg [39:0] packed_bits;
 always @(posedge clk)begin
  if(reset || !disk_ready || build_track!=selected_track)begin
   valid<=0;build_track<=selected_track;state<=ID_WAIT0;disk_addr<=18'd91554;
   write_addr<=0;sector<=0;sector_base<=track_start(selected_track);run_count<=0;
   raw_index<=0;pack_index<=0;write_index<=0;packed_bits<=0;checksum<=0;
   header<=1;id1<=0;id2<=0;raw_byte<=0;
  end else case(state)
   ID_WAIT0:state<=ID_GET0;
   ID_GET0:begin id1<=disk_data;disk_addr<=18'd91555;state<=ID_WAIT1;end
   ID_WAIT1:state<=ID_GET1;
   ID_GET1:begin id2<=disk_data;run_count<=5;state<=HEADER_SYNC;end
   HEADER_SYNC,HEADER_GAP,DATA_SYNC,SECTOR_GAP:begin
    cache[write_addr]<=state==HEADER_SYNC||state==DATA_SYNC?8'hff:8'h55;
    write_addr<=write_addr+1'b1;run_count<=run_count-1'b1;
    if(run_count==1)case(state)
     HEADER_SYNC:begin header<=1;raw_index<=0;pack_index<=0;state<=RAW;end
     HEADER_GAP:begin run_count<=5;state<=DATA_SYNC;end
     DATA_SYNC:begin header<=0;raw_index<=0;pack_index<=0;checksum<=0;state<=RAW;end
     SECTOR_GAP:if(sector+1'b1==sector_count(build_zone))state<=TAIL;
      else begin sector<=sector+1'b1;sector_base<=sector_base+18'd256;run_count<=5;state<=HEADER_SYNC;end
     default:state<=DONE;
    endcase
   end
   RAW:begin
    state<=PACK;
    if(header)case(raw_index)
     0:raw_byte<=8;1:raw_byte<={2'b0,build_track}^{3'b0,sector}^id1^id2;
     2:raw_byte<={3'b0,sector};3:raw_byte<={2'b0,build_track};
     4:raw_byte<=id2;5:raw_byte<=id1;default:raw_byte<=15;
    endcase
    else if(raw_index==0)raw_byte<=7;
    else if(raw_index<=256)begin disk_addr<=sector_base+18'(raw_index)-1'b1;state<=READ_WAIT;end
    else if(raw_index==257)raw_byte<=checksum;
    else raw_byte<=0;
   end
   READ_WAIT:state<=READ_GET;
   READ_GET:begin raw_byte<=disk_data;checksum<=checksum^disk_data;state<=PACK;end
   PACK:begin
    packed_bits<={packed_bits[29:0],code(raw_byte[7:4]),code(raw_byte[3:0])};
    raw_index<=raw_index+1'b1;pack_index<=pack_index+1'b1;
    if(pack_index==3)begin write_index<=0;state<=WRITE_GCR;end else state<=RAW;
   end
   WRITE_GCR:begin
    cache[write_addr]<=packed_bits[39:32];write_addr<=write_addr+1'b1;
    packed_bits<={packed_bits[31:0],8'b0};write_index<=write_index+1'b1;
    if(write_index==4)begin
     if(header && raw_index==8)begin run_count<=9;state<=HEADER_GAP;end
     else if(!header && raw_index==260)begin run_count<=sector_gap(build_zone);state<=SECTOR_GAP;end
     else state<=RAW;
    end
   end
   TAIL:if(write_addr==track_length(build_zone))begin valid<=1;state<=DONE;end
        else begin cache[write_addr]<=8'h55;write_addr<=write_addr+1'b1;end
   DONE:begin end
   default:valid<=0;
  endcase
 end
endmodule
