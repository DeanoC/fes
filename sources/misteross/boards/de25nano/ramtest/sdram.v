// SPDX-License-Identifier: GPL-2.0-or-later
// DE25 onboard 2x64MiB SDRAM, 13 row/10 column/2 bank bits per chip.
// 50MHz fabric clock; forwarded SDRAM clock rises on the fabric falling edge.
// Conservative single-word commands, explicit precharge, refresh both chips.
module fes_de25_sdram #(
    parameter integer POWERUP_CYCLES=10000,
    parameter integer REFRESH_CYCLES=320
) (
    input wire clk,reset_n,
    input wire [31:0] address,
    input wire [4:0] burstcount,
    input wire read,write,
    input wire [15:0] writedata,
    input wire [1:0] byteenable,
    output wire waitrequest,
    output reg [15:0] readdata=0,
    output reg readdatavalid=0,writeresponsevalid=0,
    output wire [1:0] response,
    output reg initialized=0,
    output wire dram_clk,
    output reg dram_cke=0,
    output reg [12:0] dram_addr=0,
    output reg [1:0] dram_ba=0,dram_cs_n=3,dram_dqm=0,
    output reg dram_ras_n=1,dram_cas_n=1,dram_we_n=1,
    input wire [15:0] dq_i,
    output reg [15:0] dq_o=0,
    output reg dq_oe=0
);
    localparam [4:0] POWER=0,PRE_INIT=1,REF_INIT1=2,REF_INIT2=3,MODE=4,
        IDLE=5,ACT=6,RW=7,RD_WAIT=8,WR_WAIT=9,PRE=10,WORD_DONE=11,
        REFRESH=12,DELAY=13;
    reg [4:0] state=POWER,next_state=POWER;
    reg [31:0] timer=0,refresh_age=0;
    reg [25:0] word_addr=0,write_next=0;
    reg [4:0] read_left=0,write_left=0;
    reg is_write=0;
    reg [15:0] payload=0;
    reg [1:0] enables=0;
    wire refresh_due=refresh_age>=REFRESH_CYCLES;
    assign waitrequest=!initialized||state!=IDLE||refresh_due||
        (read&&write_left!=0);
    assign response=0;
    assign dram_clk=~clk;
    wire [1:0] selected_cs=word_addr[25]?2'b01:2'b10;
    always @(posedge clk or negedge reset_n) begin
        if(!reset_n) begin
            state<=POWER;next_state<=POWER;timer<=0;refresh_age<=0;
            initialized<=0;read_left<=0;write_left<=0;
            readdatavalid<=0;writeresponsevalid<=0;dq_oe<=0;
            dram_cke<=0;dram_cs_n<=3;dram_ras_n<=1;dram_cas_n<=1;dram_we_n<=1;
            dram_addr<=0;dram_ba<=0;dram_dqm<=0;dq_o<=0;
        end else begin
            readdatavalid<=0;writeresponsevalid<=0;dq_oe<=0;
            dram_cke<=1;dram_cs_n<=3;dram_ras_n<=1;dram_cas_n<=1;dram_we_n<=1;
            dram_addr<=0;dram_ba<=0;dram_dqm<=0;
            if(initialized&&refresh_age<REFRESH_CYCLES) refresh_age<=refresh_age+1;
            case(state)
                POWER:if(timer==POWERUP_CYCLES-1) begin timer<=0;state<=PRE_INIT;end
                      else timer<=timer+1;
                PRE_INIT:begin
                    dram_cs_n<=0;dram_ras_n<=0;dram_we_n<=0;dram_addr<=13'h400;
                    state<=DELAY;timer<=3;next_state<=REF_INIT1;
                end
                REF_INIT1,REF_INIT2:begin
                    dram_cs_n<=0;dram_ras_n<=0;dram_cas_n<=0;
                    state<=DELAY;timer<=6;next_state<=state==REF_INIT1?REF_INIT2:MODE;
                end
                MODE:begin
                    // CAS2, burst length1, programmed write burst.
                    dram_cs_n<=0;dram_ras_n<=0;dram_cas_n<=0;dram_we_n<=0;dram_addr<=13'h20;
                    state<=DELAY;timer<=3;next_state<=IDLE;
                    initialized<=1;refresh_age<=0;
                end
                IDLE:begin
                    if(refresh_due) state<=REFRESH;
                    else if((read||write)&&!waitrequest) begin
                        word_addr<=write&&write_left!=0?write_next:address[26:1];
                        is_write<=write;
                        payload<=writedata;enables<=byteenable;
                        if(write&&write_left==0) write_left<=burstcount;
                        if(read) read_left<=burstcount;
                        state<=ACT;
                    end
                end
                ACT:begin
                    dram_cs_n<=selected_cs;dram_ras_n<=0;
                    dram_ba<=word_addr[24:23];dram_addr<=word_addr[22:10];
                    state<=DELAY;timer<=2;next_state<=RW;
                end
                RW:begin
                    dram_cs_n<=selected_cs;dram_cas_n<=0;dram_we_n<=!is_write;
                    dram_ba<=word_addr[24:23];dram_addr<={3'b000,word_addr[9:0]};
                    if(is_write) begin
                        dq_oe<=1;dq_o<=payload;dram_dqm<=~enables;
                        state<=WR_WAIT;timer<=3;
                    end else begin state<=RD_WAIT;timer<=2;end
                end
                WR_WAIT:if(timer==0) state<=PRE;else timer<=timer-1;
                RD_WAIT:if(timer==0) begin
                    readdata<=dq_i;readdatavalid<=1;state<=PRE;
                end else timer<=timer-1;
                PRE:begin
                    dram_cs_n<=selected_cs;dram_ras_n<=0;dram_we_n<=0;dram_addr<=13'h400;
                    state<=DELAY;timer<=3;next_state<=WORD_DONE;
                end
                WORD_DONE:begin
                    if(is_write) begin
                        write_left<=write_left-1;write_next<=word_addr+1;state<=IDLE;
                        if(write_left==1) writeresponsevalid<=1;
                    end else if(read_left==1) begin read_left<=0;state<=IDLE;end
                    else begin
                        read_left<=read_left-1;word_addr<=word_addr+1;
                        if(refresh_due) begin
                            state<=REFRESH;next_state<=ACT;
                        end else state<=ACT;
                    end
                end
                REFRESH:begin
                    dram_cs_n<=0;dram_ras_n<=0;dram_cas_n<=0;refresh_age<=0;
                    state<=DELAY;timer<=6;next_state<=read_left!=0?ACT:IDLE;
                end
                DELAY:if(timer==0) state<=next_state;else timer<=timer-1;
                default:state<=POWER;
            endcase
        end
    end
endmodule
