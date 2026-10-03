// SPDX-License-Identifier: GPL-3.0-or-later
// An original internal FPGA expansion part. Cartridge signature is read only;
// the test MMIO aperture demonstrates writable peripherals on the same bus.
module st_probe (
    input wire clk,
    input wire reset,
    input wire enable,
    input wire [7:0] wait_cycles,
    input wire req,
    input wire [23:1] addr,
    input wire [15:0] wdata,
    input wire [1:0] byte_enable,
    input wire write,
    output reg [15:0] rdata,
    output wire ack,
    output wire berr,
    output reg [15:0] scratch,
    output reg [31:0] long_value,
    output reg [31:0] write_count
);
    reg active, committed;
    reg [7:0] age;
    wire [23:0] address = {addr, 1'b0};
    wire selected = address == 24'hfa0000 || address == 24'hfa0002 ||
                    address == 24'hff9000 || address == 24'hff9008 ||
                    address == 24'hff900a;
    wire ready = enable && req && active && age >= wait_cycles;
    assign ack = ready && selected;
    assign berr = ready && address == 24'hff9004;
    always @* begin
        case (address)
            24'hfa0000: rdata = 16'h5205;
            24'hfa0002: rdata = 16'h6800;
            24'hff9000: rdata = scratch;
            24'hff9008: rdata = long_value[31:16];
            24'hff900a: rdata = long_value[15:0];
            default: rdata = 16'hffff;
        endcase
    end
    always @(posedge clk) begin
        if (reset) begin
            active <= 1'b0;
            committed <= 1'b0;
            age <= 8'd0;
            scratch <= 16'd0;
            long_value <= 32'd0;
            write_count <= 32'd0;
        end else if (!req) begin
            active <= 1'b0;
            committed <= 1'b0;
            age <= 8'd0;
        end else begin
            active <= 1'b1;
            if (age != 8'hff) age <= age + 1'b1;
            if (ack && write && !committed && address[23:16] == 8'hff) begin
                committed <= 1'b1;
                write_count <= write_count + 1'b1;
                case (address)
                    24'hff9000: begin
                        if (byte_enable[1]) scratch[15:8] <= wdata[15:8];
                        if (byte_enable[0]) scratch[7:0] <= wdata[7:0];
                    end
                    24'hff9008: begin
                        if (byte_enable[1]) long_value[31:24] <= wdata[15:8];
                        if (byte_enable[0]) long_value[23:16] <= wdata[7:0];
                    end
                    24'hff900a: begin
                        if (byte_enable[1]) long_value[15:8] <= wdata[15:8];
                        if (byte_enable[0]) long_value[7:0] <= wdata[7:0];
                    end
                    default: ;
                endcase
            end
        end
    end
endmodule
