// SPDX-License-Identifier: GPL-2.0-or-later
`include "generated/fes_de25_ramtest.vh"
`ifndef FES_DE25_RAMTEST_BUILD_ID
`define FES_DE25_RAMTEST_BUILD_ID 128'h0
`endif
module fes_de25_ramtest #(
    parameter integer DATA_W=128, CHANNEL=0, LOCAL_MEMORY=0,
    parameter [31:0] CAPACITY_BYTES=32'h40000000,
    parameter integer CLOCK_HZ=100000000,
    parameter [127:0] BUILD_ID=`FES_DE25_RAMTEST_BUILD_ID
) (
    input wire clk, reset_n, calibrated, memory_ready,
    input wire [7:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output reg [31:0] readdata=0,
    output reg readdatavalid=0, writeresponsevalid=0,
    output wire waitrequest,
    output reg [1:0] response=0,
    output wire [31:0] avm_address,
    output wire [4:0] avm_burstcount,
    output wire avm_read, avm_write,
    output wire [DATA_W-1:0] avm_writedata,
    output wire [DATA_W/8-1:0] avm_byteenable,
    input wire avm_waitrequest, avm_readdatavalid, avm_writeresponsevalid,
    input wire [DATA_W-1:0] avm_readdata,
    input wire [1:0] avm_response
);
    reg first_seen=0, healthy=0;
    wire reset_fault=first_seen&&!healthy;
    always @(posedge clk) if(reset_n) first_seen<=1;
    always @(posedge clk or negedge reset_n)
        if(!reset_n) healthy<=0; else if(!first_seen) healthy<=1;
    assign waitrequest=!reset_n||!first_seen;
    (* altera_attribute="-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] calibration=0;
    always @(posedge clk) calibration<={calibration[0],calibrated};
    wire cal_ready=calibration[1]&&memory_ready;
    reg [31:0] staged_base=0, staged_base_hi=0;
    reg [31:0] staged_span=LOCAL_MEMORY?CAPACITY_BYTES:0, staged_span_hi=0;
    reg [31:0] staged_offset=0, staged_length=LOCAL_MEMORY?CAPACITY_BYTES:0;
    reg [63:0] staged_generation=LOCAL_MEMORY?64'd1:64'd0;
    reg [31:0] staged_sequence=LOCAL_MEMORY?32'd1:32'd0;
    reg [63:0] active_generation=0;
    reg [31:0] active_sequence=0, active_base=0, active_length=0;
    reg [31:0] control_faults=0, serial=0;
    reg start_scan=0, stop_scan=0, clear_scan=0;
    wire busy,done,stopped;
    wire [31:0] scan_faults, cursor, errors, first_addr;
    wire [2:0] pattern;
    wire [DATA_W-1:0] first_expected,first_actual,bad_bits;
    wire [127:0] expected128={{(128-DATA_W){1'b0}},first_expected};
    wire [127:0] actual128={{(128-DATA_W){1'b0}},first_actual};
    wire [127:0] bad128={{(128-DATA_W){1'b0}},bad_bits};
    wire [63:0] written_bytes,read_bytes,write_cycles,read_cycles,first_sum,last_sum;
    wire [31:0] samples,first_min,first_max,last_min,last_max;
    wire running=busy||start_scan;
    wire [31:0] faults=control_faults|scan_faults|
        (reset_fault?`FES_DE25_RAMTEST_FAULT_TRANSPORT_RESET:32'd0);
    wire [31:0] status=(running?`FES_DE25_RAMTEST_STATUS_BUSY:32'd0)|
        (done&&!running&&!reset_fault?`FES_DE25_RAMTEST_STATUS_DONE:32'd0)|
        (stopped?`FES_DE25_RAMTEST_STATUS_STOPPED:32'd0)|
        ((faults!=0||errors!=0)?`FES_DE25_RAMTEST_STATUS_FAULT:32'd0)|
        (cal_ready?`FES_DE25_RAMTEST_STATUS_CALIBRATED:32'd0)|
        (reset_fault?`FES_DE25_RAMTEST_STATUS_RESET_UNCERTAIN:32'd0);
    wire bounds_valid=staged_base_hi==0&&staged_span_hi==0&&staged_span!=0&&
        staged_span<=CAPACITY_BYTES&&staged_span[4:0]==0&&staged_base[4:0]==0&&
        staged_offset[4:0]==0&&staged_length>=32&&
        (staged_length&(staged_length-1))==0&&
        {1'b0,staged_offset}+{1'b0,staged_length}<={1'b0,staged_span}&&
        {1'b0,staged_base}+{1'b0,staged_span}<=33'h100000000&&
        staged_generation!=0&&staged_sequence!=0&&
        (staged_generation!=active_generation||staged_sequence>active_sequence)&&
        (!LOCAL_MEMORY||(staged_base==0&&staged_span==CAPACITY_BYTES));
    reg [31:0] snapshot[0:63];
    integer i;
    initial for(i=0;i<64;i=i+1) snapshot[i]=0;
    reg [31:0] read_value;
    reg read_valid;
    always @* begin
        read_valid=address[1:0]==0;
        read_value=0;
        case(address)
            `FES_DE25_RAMTEST_ID_OFFSET: read_value=`FES_DE25_RAMTEST_IDENTITY;
            `FES_DE25_RAMTEST_VERSION_OFFSET: read_value=`FES_DE25_RAMTEST_VERSION;
            `FES_DE25_RAMTEST_STATUS_OFFSET: read_value=status;
            `FES_DE25_RAMTEST_COMMAND_OFFSET: read_value=0;
            `FES_DE25_RAMTEST_FAULT_OFFSET: read_value=faults;
            `FES_DE25_RAMTEST_CHANNEL_OFFSET: read_value=CHANNEL;
            `FES_DE25_RAMTEST_CAPACITY_OFFSET: read_value=CAPACITY_BYTES;
            8'h1c: read_value=0;
            `FES_DE25_RAMTEST_BASE_OFFSET: read_value=staged_base;
            `FES_DE25_RAMTEST_BASE_HIGH_OFFSET: read_value=staged_base_hi;
            `FES_DE25_RAMTEST_SPAN_OFFSET: read_value=staged_span;
            `FES_DE25_RAMTEST_SPAN_HIGH_OFFSET: read_value=staged_span_hi;
            `FES_DE25_RAMTEST_TRANSFER_OFFSET: read_value=staged_offset;
            `FES_DE25_RAMTEST_LENGTH_OFFSET: read_value=staged_length;
            `FES_DE25_RAMTEST_GENERATION_OFFSET: read_value=staged_generation[31:0];
            `FES_DE25_RAMTEST_GENERATION_HIGH_OFFSET: read_value=staged_generation[63:32];
            `FES_DE25_RAMTEST_SEQUENCE_OFFSET: read_value=staged_sequence;
            `FES_DE25_RAMTEST_SERIAL_OFFSET: read_value=serial;
            `FES_DE25_RAMTEST_CLOCK_HZ_OFFSET: read_value=CLOCK_HZ;
            `FES_DE25_RAMTEST_DATA_BYTES_OFFSET: read_value=DATA_W/8;
            8'he8:read_value=BUILD_ID[31:0]; 8'hec:read_value=BUILD_ID[63:32];
            8'hf0:read_value=BUILD_ID[95:64]; 8'hf4:read_value=BUILD_ID[127:96];
            default: begin
                if(address>=8'h48&&address<=8'hdc) read_value=snapshot[address[7:2]];
                else read_valid=0;
            end
        endcase
    end
    reg [31:0] rejected;
    always @* begin
        rejected=0;
        if(write) begin
            if(reset_fault) rejected=`FES_DE25_RAMTEST_FAULT_TRANSPORT_RESET;
            else if(byteenable!=4'hf) rejected=`FES_DE25_RAMTEST_FAULT_PARTIAL;
            else if(read||address[1:0]!=0) rejected=`FES_DE25_RAMTEST_FAULT_COMMAND;
            else if(address==`FES_DE25_RAMTEST_COMMAND_OFFSET) begin
                case(writedata)
                    `FES_DE25_RAMTEST_COMMAND_START:
                        if(running||faults!=0||!cal_ready||!bounds_valid)
                            rejected=`FES_DE25_RAMTEST_FAULT_BOUNDS;
                    `FES_DE25_RAMTEST_COMMAND_CLEAR:
                        if(running) rejected=`FES_DE25_RAMTEST_FAULT_COMMAND;
                    `FES_DE25_RAMTEST_COMMAND_STOP: begin end
                    `FES_DE25_RAMTEST_COMMAND_SNAPSHOT:
                        if(serial==32'hffffffff) rejected=`FES_DE25_RAMTEST_FAULT_COMMAND;
                    default: rejected=`FES_DE25_RAMTEST_FAULT_COMMAND;
                endcase
            end else if(address<8'h20||address>8'h40)
                rejected=`FES_DE25_RAMTEST_FAULT_COMMAND;
        end
        if(read&&!read_valid) rejected=rejected|`FES_DE25_RAMTEST_FAULT_COMMAND;
    end
    always @(posedge clk or negedge reset_n) begin
        if(!reset_n) begin readdatavalid<=0;writeresponsevalid<=0;response<=0;readdata<=0;end
        else begin
            readdatavalid<=read&&!waitrequest;
            writeresponsevalid<=write&&!waitrequest;
            response<=rejected!=0?2'b10:2'b00;
            if(read&&!waitrequest) readdata<=read_value;
        end
    end
    always @(posedge clk) begin
        start_scan<=0;stop_scan<=0;clear_scan<=0;
        if(reset_fault) control_faults<=control_faults|`FES_DE25_RAMTEST_FAULT_TRANSPORT_RESET;
        if(!waitrequest) begin
            control_faults<=control_faults|rejected;
            if(write&&rejected==0) begin
                case(address)
                    8'h20:staged_base<=writedata; 8'h24:staged_base_hi<=writedata;
                    8'h28:staged_span<=writedata; 8'h2c:staged_span_hi<=writedata;
                    8'h30:staged_offset<=writedata; 8'h34:staged_length<=writedata;
                    8'h38:staged_generation[31:0]<=writedata;
                    8'h3c:staged_generation[63:32]<=writedata;
                    8'h40:staged_sequence<=writedata;
                    8'h0c:case(writedata)
                        1:begin
                            active_generation<=staged_generation; active_sequence<=staged_sequence;
                            active_base<=staged_base+staged_offset; active_length<=staged_length;
                            start_scan<=1;
                        end
                        2:stop_scan<=1;
                        4:begin control_faults<=0;clear_scan<=1;end
                        8:begin
                            serial<=serial+1;
                            snapshot[18]<=status; snapshot[19]<=faults;
                            snapshot[20]<=active_generation[31:0];snapshot[21]<=active_generation[63:32];
                            snapshot[22]<=active_sequence;snapshot[23]<={29'd0,pattern};
                            snapshot[24]<=cursor;snapshot[25]<=errors;snapshot[26]<=first_addr;
                            snapshot[27]<=written_bytes[31:0];snapshot[28]<=written_bytes[63:32];
                            snapshot[29]<=read_bytes[31:0];snapshot[30]<=read_bytes[63:32];
                            snapshot[31]<=write_cycles[31:0];snapshot[32]<=write_cycles[63:32];
                            snapshot[33]<=read_cycles[31:0];snapshot[34]<=read_cycles[63:32];
                            snapshot[35]<=expected128[31:0];snapshot[36]<=expected128[63:32];
                            snapshot[37]<=expected128[95:64];snapshot[38]<=expected128[127:96];
                            snapshot[39]<=actual128[31:0];snapshot[40]<=actual128[63:32];
                            snapshot[41]<=actual128[95:64];snapshot[42]<=actual128[127:96];
                            snapshot[43]<=bad128[31:0];snapshot[44]<=bad128[63:32];
                            snapshot[45]<=bad128[95:64];snapshot[46]<=bad128[127:96];
                            snapshot[47]<=samples;snapshot[48]<=first_min;snapshot[49]<=first_max;
                            snapshot[50]<=first_sum[31:0];snapshot[51]<=first_sum[63:32];
                            snapshot[52]<=last_min;snapshot[53]<=last_max;
                            snapshot[54]<=last_sum[31:0];snapshot[55]<=last_sum[63:32];
                        end
                        default:begin end
                    endcase
                    default:begin end
                endcase
            end
        end
    end
    fes_de25_ram_scan #(.DATA_W(DATA_W)) scan(
        .clk(clk),.reset_n(reset_n),.start(start_scan),
        .stop(stop_scan||control_faults!=0||reset_fault),.clear(clear_scan),.calibrated(cal_ready),
        .base(active_base),.length(active_length),.busy(busy),.done(done),.stopped(stopped),.faults(scan_faults),
        .pattern(pattern),.cursor(cursor),.errors(errors),.first_addr(first_addr),
        .first_expected(first_expected),.first_actual(first_actual),.bad_bits(bad_bits),
        .written_bytes(written_bytes),.read_bytes(read_bytes),.write_cycles(write_cycles),.read_cycles(read_cycles),
        .samples(samples),.first_min(first_min),.first_max(first_max),.last_min(last_min),.last_max(last_max),
        .first_sum(first_sum),.last_sum(last_sum),.address(avm_address),.burstcount(avm_burstcount),
        .read(avm_read),.write(avm_write),.writedata(avm_writedata),.byteenable(avm_byteenable),
        .waitrequest(avm_waitrequest),.readdatavalid(avm_readdatavalid),.writeresponsevalid(avm_writeresponsevalid),
        .readdata(avm_readdata),.response(avm_response));
endmodule
