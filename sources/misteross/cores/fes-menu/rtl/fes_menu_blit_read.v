// SPDX-License-Identifier: GPL-2.0-or-later
// Reads total 64-bit words from base in bursts of up to 128 beats, issuing
// the next burst as soon as the port takes the last one. Each returned word
// comes out with its index; done is high once every word has returned and
// before the first start. start is only taken while done.
module fes_menu_blit_read (
    input wire clk, rst,
    input wire start,
    input wire [28:0] base,
    input wire [10:0] total,
    output wire done,
    output wire data_valid,
    output wire [9:0] data_index,
    output wire [63:0] data,

    output reg [28:0] address = 29'd0,
    output reg [7:0] burstcount = 8'd0,
    output reg read = 1'b0,
    input wire waitrequest,
    input wire [63:0] readdata,
    input wire readdatavalid
);
    reg [28:0] next = 29'd0;
    reg [10:0] unissued = 11'd0, received = 11'd0, expected = 11'd0;
    wire [7:0] length = unissued > 11'd128 ? 8'd128 : unissued[7:0];
    assign done = unissued == 11'd0 && !read && received == expected;
    assign data_valid = readdatavalid;
    assign data_index = received[9:0];
    assign data = readdata;
    always @(posedge clk) begin
        if (rst) begin
            read <= 1'b0; unissued <= 11'd0; received <= 11'd0; expected <= 11'd0;
        end else begin
            if (read) begin
                if (!waitrequest) read <= 1'b0;
            end else if (unissued != 11'd0) begin
                read <= 1'b1; address <= next; burstcount <= length;
                next <= next + {21'd0, length}; unissued <= unissued - {3'd0, length};
            end
            if (readdatavalid) received <= received + 11'd1;
            if (start && done) begin
                next <= base; unissued <= total; received <= 11'd0; expected <= total;
            end
        end
    end
endmodule
