// Diagnostic Avalon-MM register endpoint; addresses are byte offsets only.
// This module declares no Linux physical address or runtime programming ABI.
`include "generated/fes_de25_mailbox.vh"

module fes_de25_mailbox (
    input  wire                                     clk,
    input  wire                                     reset_n,
    input  wire [`FES_DE25_MAILBOX_ADDRESS_BITS-1:0] address,
    input  wire                                     read,
    input  wire                                     write,
    input  wire [3:0]                               byteenable,
    input  wire [31:0]                              writedata,
    output reg  [31:0]                              readdata,
    output reg                                      readdatavalid,
    output wire                                     waitrequest
);
    reg [31:0] scratch;
    reg [31:0] counter;
    integer lane;

    wire [31:0] byte_address = {{(32-`FES_DE25_MAILBOX_ADDRESS_BITS){1'b0}}, address};

    // No transaction is accepted during reset. Otherwise every asserted
    // read/write is accepted on its clock edge, including consecutive ones.
    assign waitrequest = !reset_n;

    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) begin
            scratch <= 32'd0;
            counter <= 32'd0;
            readdata <= 32'd0;
            readdatavalid <= 1'b0;
        end else begin
            counter <= counter + 32'd1;
            readdatavalid <= read;
            if (read) begin
                case (byte_address)
                    `FES_DE25_MAILBOX_ID_OFFSET:      readdata <= `FES_DE25_MAILBOX_IDENTITY;
                    `FES_DE25_MAILBOX_VERSION_OFFSET: readdata <= `FES_DE25_MAILBOX_VERSION;
                    `FES_DE25_MAILBOX_SCRATCH_OFFSET: readdata <= scratch;
                    `FES_DE25_MAILBOX_COUNTER_OFFSET: readdata <= counter;
                    `FES_DE25_MAILBOX_STATUS_OFFSET:  readdata <= `FES_DE25_MAILBOX_READY;
                    default:                         readdata <= 32'd0;
                endcase
            end
            if (write && byte_address == `FES_DE25_MAILBOX_SCRATCH_OFFSET) begin
                for (lane = 0; lane < 4; lane = lane + 1)
                    if (byteenable[lane])
                        scratch[8*lane +: 8] <= writedata[8*lane +: 8];
            end
        end
    end
endmodule
