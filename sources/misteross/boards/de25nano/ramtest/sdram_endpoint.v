// SPDX-License-Identifier: GPL-2.0-or-later
module fes_de25_sdram_ramtest(
    input wire clk,reset_n,
    input wire [7:0] address,
    input wire read,write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output wire [31:0] readdata,
    output wire readdatavalid,writeresponsevalid,waitrequest,
    output wire [1:0] response,
    output wire dram_clk,dram_cke,dram_ras_n,dram_cas_n,dram_we_n,
    output wire [12:0] dram_addr,
    output wire [1:0] dram_ba,dram_cs_n,dram_dqm,
    inout wire [15:0] dram_dq
);
    wire [31:0] mem_address;
    wire [4:0] mem_burstcount;
    wire mem_read,mem_write,mem_wait,mem_rvalid,mem_bvalid,ready;
    wire [15:0] mem_wdata,mem_rdata,dq_o;
    wire [1:0] mem_byteenable,mem_response;
    wire dq_oe;
    assign dram_dq=dq_oe?dq_o:16'hzzzz;
    fes_de25_ramtest #(.DATA_W(16),.CHANNEL(2),.LOCAL_MEMORY(1),
        .CAPACITY_BYTES(32'h08000000),.CLOCK_HZ(50000000)) control(
        .clk(clk),.reset_n(reset_n),.calibrated(ready),.memory_ready(ready),.address(address),.read(read),.write(write),
        .byteenable(byteenable),.writedata(writedata),.readdata(readdata),.readdatavalid(readdatavalid),
        .writeresponsevalid(writeresponsevalid),.waitrequest(waitrequest),.response(response),
        .avm_address(mem_address),.avm_burstcount(mem_burstcount),.avm_read(mem_read),.avm_write(mem_write),
        .avm_writedata(mem_wdata),.avm_byteenable(mem_byteenable),.avm_waitrequest(mem_wait),
        .avm_readdata(mem_rdata),.avm_readdatavalid(mem_rvalid),.avm_writeresponsevalid(mem_bvalid),
        .avm_response(mem_response));
    fes_de25_sdram memory(
        .clk(clk),.reset_n(reset_n),.address(mem_address),.burstcount(mem_burstcount),
        .read(mem_read),.write(mem_write),.writedata(mem_wdata),.byteenable(mem_byteenable),
        .waitrequest(mem_wait),.readdata(mem_rdata),.readdatavalid(mem_rvalid),
        .writeresponsevalid(mem_bvalid),.response(mem_response),.initialized(ready),
        .dram_clk(dram_clk),.dram_cke(dram_cke),.dram_addr(dram_addr),.dram_ba(dram_ba),
        .dram_cs_n(dram_cs_n),.dram_ras_n(dram_ras_n),.dram_cas_n(dram_cas_n),.dram_we_n(dram_we_n),
        .dram_dqm(dram_dqm),.dq_i(dram_dq),.dq_o(dq_o),.dq_oe(dq_oe));
endmodule
