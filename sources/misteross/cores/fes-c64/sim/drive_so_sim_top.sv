// SPDX-License-Identifier: MIT
module drive_so_sim_top (
    input wire clk, reset, byte_ready_n, clear_overflow, adjacent_clv,
    output wire ref_ce, phi, cpu_reset, cpu_so_n, raw_ca1, overflow,
    output wire [3:0] divider,
    output wire [15:0] pc,
    output wire [5:0] state,
    output wire [7:0] marker
);
    wire [13:0] address;
    reg [7:0] data;
    function automatic [7:0] rom(input [13:0] a);
        if(adjacent_clv) begin
            case(a)
            0:rom=8'hb8; 1:rom=8'h50; 2:rom=8'hfe; // CLV; BVC self
            3:rom=8'ha9; 4:rom=8'ha5;
            5:rom=8'h8d; 6:rom=0; 7:rom=2;
            8:rom=8'h4c; 9:rom=8'h08; 10:rom=8'hc0;
            14'h3ffc:rom=0; 14'h3ffd:rom=8'hc0; default:rom=8'hea;
            endcase
        end else begin
        case(a)
        0:rom=8'hb8; 1,2:rom=8'hea; // CLV; NOP; NOP
        3:rom=8'h50; 4:rom=8'hfe; // BVC self
        5:rom=8'ha9; 6:rom=8'ha5;
        7:rom=8'h8d; 8:rom=0; 9:rom=2; // STA $0200
        10:rom=8'h4c; 11:rom=clear_overflow ? 8'h10 : 8'h0a;
        12:rom=8'hc0; // JMP self or CLV routine
        16:rom=8'hb8; 17:rom=8'h4c; 18:rom=8'h11; 19:rom=8'hc0;
        14'h3ffc:rom=0; 14'h3ffd:rom=8'hc0;
        default:rom=8'hea;
        endcase
        end
    endfunction
    always @(posedge clk) data <= rom(address);
    c64_drive1541 drive(.clk(clk),.reset(reset),
        .atn_low(1'b0),.host_clock_low(1'b0),.host_data_low(1'b0),
        .disk_data(8'hff),.disk_pb(8'hef),.byte_ready_n(byte_ready_n),
        .drive_clock_low(),.drive_data_low(),.disk_pa_out(),.disk_pa_ddr(),
        .disk_pb_out(),.disk_pb_ddr(),.byte_ready_enable(),.read_mode(),.debug_pc(),
        .rom_address(address),.rom_data(data),.ref_ce(ref_ce),.phi(phi));
    assign cpu_reset=drive.cpu_reset;
    assign cpu_so_n=drive.cpu_so_n;
    assign raw_ca1=drive.disk_via.ca1;
    assign overflow=drive.cpu.V;
    assign divider=drive.cpu_div;
    assign pc=drive.cpu.PC;
    assign state=drive.cpu.state;
    assign marker=drive.ram[512];
endmodule
