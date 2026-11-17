module write_contract(input wire clk);
 (* anyseq *) reg reset, start, write;
 (* anyseq *) reg [25:0] addr;
 (* anyseq *) reg [15:0] wdata, dq_rise, dq_fall;
 (* anyseq *) reg [1:0] mask;
 wire cke,ncs,nras,ncas,nwe,dqml,dqmh,oe;
 wire [15:0] returned; wire [12:0] a; wire [1:0] ba; wire [15:0] dq;
 sdram_addon_port #(.BYTE_MASK_ENABLED(1),.IO_OUTPUT_REGISTERS(1),.SINGLE_RANK_REFRESH(1)) dut(
 .clk(clk),.clk_pin(clk),.reset(reset),.start(start),.write(write),.addr(addr),.wdata(wdata),
 .write_byte_enable(mask),.rate(2'd3),.refresh_window(1'b0),.refresh_pending(),.initialized(),.done(),.rdata(returned),
 .sdram_clk(),.sdram_cke(cke),.sdram_ncs(ncs),.sdram_nras(nras),.sdram_ncas(ncas),.sdram_nwe(nwe),
 .sdram_ba(ba),.sdram_a(a),.sdram_dqml(dqml),.sdram_dqmh(dqmh),.dq_out(dq),.dq_oe(oe),.dq_rise(dq_rise),.dq_fall(dq_fall));
 reg [7:0] valid=0;
 reg [4:0] read_history=0;
 reg [5:0] reset_history=0;
 always @(posedge clk) begin
  valid <= {valid[6:0],1'b1};
  read_history <= {read_history[3:0],cke && nras && !ncas && nwe};
  reset_history <= {reset_history[4:0],reset};
  if (&valid && read_history[4] && !reset && !(|reset_history))
   assert(returned==$past(dq_fall));
  if (&valid && cke && nras && !ncas && !nwe) begin
   assert(oe && $past(oe) && $past(oe,2) && $past(oe,3));
   assert(dq==$past(dq) && dq==$past(dq,2) && dq==$past(dq,3));
   assert(a[12:11]==$past(a[12:11]) && a[12:11]==$past(a[12:11],2));
  end
 end
endmodule
module altddio_out #(parameter width=1,intended_device_family="Cyclone V",power_up_high="OFF",oe_reg="UNREGISTERED",extend_oe_disable="OFF",invert_output="OFF")
(input wire [width-1:0] datain_h,datain_l,input wire outclock,outclocken,aset,aclr,sset,sclr,oe,output wire [width-1:0]dataout,output wire oe_out);
assign dataout = outclock ? datain_h : datain_l; assign oe_out=1'b0;
endmodule
