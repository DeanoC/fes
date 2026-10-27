// SPDX-License-Identifier: MIT
// Simple dual-port RAMs for the pc386 VGA and presenter: one write port and
// one registered read port. pc386_ram reads when read is high and holds its
// output otherwise; pc386_dcram writes in one clock and reads in another, for
// the presenter's line buffers. Byte enables cover BYTES equal lanes.
module pc386_ram #(
    parameter integer WIDTH = 32,
    parameter integer DEPTH = 256,
    parameter integer BYTES = 1
) (
    input wire clk,
    input wire write,
    input wire [$clog2(DEPTH)-1:0] waddress,
    input wire [WIDTH-1:0] wdata,
    input wire [BYTES-1:0] wbyteenable,
    input wire read,
    input wire [$clog2(DEPTH)-1:0] raddress,
    output reg [WIDTH-1:0] rdata
);
    localparam integer LANE = WIDTH / BYTES;
    (* ramstyle = "M10K" *) reg [WIDTH-1:0] memory [0:DEPTH-1];
    integer lane;
    always @(posedge clk) begin
        if (write)
            for (lane = 0; lane < BYTES; lane = lane + 1)
                if (wbyteenable[lane]) memory[waddress][lane * LANE +: LANE] <= wdata[lane * LANE +: LANE];
        if (read) rdata <= memory[raddress];
    end
    initial if (WIDTH % BYTES != 0) $error("pc386_ram: WIDTH must divide into BYTES lanes");
endmodule

module pc386_dcram #(
    parameter integer WIDTH = 36,
    parameter integer DEPTH = 1024
) (
    input wire wclk,
    input wire write,
    input wire [$clog2(DEPTH)-1:0] waddress,
    input wire [WIDTH-1:0] wdata,
    input wire rclk,
    input wire read,
    input wire [$clog2(DEPTH)-1:0] raddress,
    output reg [WIDTH-1:0] rdata
);
    (* ramstyle = "M10K" *) reg [WIDTH-1:0] memory [0:DEPTH-1];
    always @(posedge wclk) if (write) memory[waddress] <= wdata;
    always @(posedge rclk) if (read) rdata <= memory[raddress];
endmodule

// Show-ahead FIFO: dout is the head whenever !empty; pop takes it.
module pc386_fifo #(
    parameter integer WIDTH = 8,
    parameter integer DEPTH = 16
) (
    input wire clk, reset,
    input wire push,
    input wire [WIDTH-1:0] din,
    output wire full,
    input wire pop,
    output wire [WIDTH-1:0] dout,
    output wire empty,
    output wire [$clog2(DEPTH):0] count
);
    localparam integer AW = $clog2(DEPTH);
    reg [AW-1:0] write_pointer, read_pointer;
    reg [AW:0] stored;      // entries in the RAM, not counting the head
    reg head_valid;
    wire take = !head_valid || pop;
    wire fetch = take && stored != 0;
    pc386_ram #(.WIDTH(WIDTH), .DEPTH(DEPTH)) storage (
        .clk(clk), .write(push), .waddress(write_pointer), .wdata(din), .wbyteenable(1'b1),
        .read(fetch), .raddress(read_pointer), .rdata(dout)
    );
    assign empty = !head_valid;
    assign count = stored + {{AW{1'b0}}, head_valid};
    assign full = count == (AW + 1)'(DEPTH);
    always @(posedge clk) begin
        if (reset) begin
            write_pointer <= 0; read_pointer <= 0; stored <= 0; head_valid <= 0;
        end else begin
            if (push) write_pointer <= write_pointer + 1'b1;
            if (fetch) read_pointer <= read_pointer + 1'b1;
            stored <= stored + {{AW{1'b0}}, push} - {{AW{1'b0}}, fetch};
            if (take) head_valid <= fetch;
        end
    end
endmodule

// Show-ahead FIFO between two clocks: Gray-coded pointers cross through two
// flip-flops each way, the storage is a dual-clock RAM.
module pc386_async_fifo #(
    parameter integer WIDTH = 64,
    parameter integer DEPTH = 16
) (
    input wire wclk, wreset,
    input wire push,
    input wire [WIDTH-1:0] din,
    output wire full,
    input wire rclk, rreset,
    input wire pop,
    output wire [WIDTH-1:0] dout,
    output wire empty
);
    localparam integer AW = $clog2(DEPTH);
    function automatic [AW:0] gray(input [AW:0] value);
        gray = value ^ (value >> 1);
    endfunction
    function automatic [AW:0] binary(input [AW:0] value);
        integer bit_index;
        begin
            binary[AW] = value[AW];
            for (bit_index = AW - 1; bit_index >= 0; bit_index = bit_index - 1)
                binary[bit_index] = binary[bit_index + 1] ^ value[bit_index];
        end
    endfunction
    reg [AW:0] write_pointer, write_gray, read_pointer, read_gray;
    (* async_reg = "true" *) reg [AW:0] read_gray_sync [0:1];
    (* async_reg = "true" *) reg [AW:0] write_gray_sync [0:1];
    wire [AW:0] read_seen = binary(read_gray_sync[1]);
    wire [AW:0] write_seen = binary(write_gray_sync[1]);
    assign full = write_pointer - read_seen == (AW + 1)'(DEPTH);
    reg head_valid;
    wire take = !head_valid || pop;
    wire fetch = take && read_pointer != write_seen;
    pc386_dcram #(.WIDTH(WIDTH), .DEPTH(DEPTH)) storage (
        .wclk(wclk), .write(push), .waddress(write_pointer[AW-1:0]), .wdata(din),
        .rclk(rclk), .read(fetch), .raddress(read_pointer[AW-1:0]), .rdata(dout)
    );
    assign empty = !head_valid;
    always @(posedge wclk) begin
        if (wreset) begin
            write_pointer <= 0; write_gray <= 0; read_gray_sync[0] <= 0; read_gray_sync[1] <= 0;
        end else begin
            if (push) begin
                write_pointer <= write_pointer + 1'b1;
                write_gray <= gray(write_pointer + 1'b1);
            end
            read_gray_sync[0] <= read_gray;
            read_gray_sync[1] <= read_gray_sync[0];
        end
    end
    always @(posedge rclk) begin
        if (rreset) begin
            read_pointer <= 0; read_gray <= 0; head_valid <= 0;
            write_gray_sync[0] <= 0; write_gray_sync[1] <= 0;
        end else begin
            if (fetch) begin
                read_pointer <= read_pointer + 1'b1;
                read_gray <= gray(read_pointer + 1'b1);
            end
            if (take) head_valid <= fetch;
            write_gray_sync[0] <= write_gray;
            write_gray_sync[1] <= write_gray_sync[0];
        end
    end
endmodule
