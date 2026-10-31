// SPDX-License-Identifier: MIT
`include "fes_application.vh"
`include "fes_computer.vh"

// The ATA image in the DDR window on port 2's aux master
// (docs/pc386-machine-design.md, "Disk"): pc386_ata's image port, and the
// CRC pass of fes.media.ata-disk's Commit.
//
// The image is at window offset 0x08000000 (FesComputerAtaDiskWindowOffset).
// The drive's requests, a 64-bit word at a time, pass straight through as
// single beats: disk_ready is the clock the port takes the command and
// disk_rvalid the read's data.
//
// While verify is high (the unit is verifying, fes_computer_storage) the pass
// reads sectors x 64 words from the image in bursts of one sector and runs
// CRC-32/IEEE over them, a word per beat, first byte lowest. It then pulses
// verify_done, with verify_match when the CRC is crc, and waits for verify to
// fall. The drive is absent while the unit verifies, so it asks nothing; the
// pass keeps the port until every beat it asked for has returned, also when
// verify falls early. reset is port 2's.
module pc386_disk (
    input wire clk, reset,
    input wire verify,
    input wire [31:0] sectors,
    input wire [31:0] crc,
    output reg verify_done, verify_match,
    // pc386_ata's image port
    input wire disk_valid, disk_write,
    input wire [23:0] disk_address,
    input wire [63:0] disk_wdata,
    output wire disk_ready, disk_rvalid,
    output wire [63:0] disk_rdata,
    // Port 2's aux master (read data on readdata)
    output wire [28:0] aux_address,
    output wire [7:0] aux_burstcount,
    output wire aux_read, aux_write,
    output wire [63:0] aux_writedata,
    output wire [7:0] aux_byteenable,
    input wire aux_waitrequest, aux_readdatavalid,
    input wire [63:0] readdata
);
    localparam [31:0] IMAGE = `FES_APPLICATION_HPS_DDR_WINDOW_BASE + `FES_COMPUTER_ATA_DISK_WINDOW_OFFSET;
    localparam [28:0] IMAGE_WORD = IMAGE[31:3];
    localparam [7:0] BURST = 8'd64;  // one sector
    localparam [31:0] POLY = `FES_COMPUTER_MEDIA_CRC32_POLYNOMIAL;

    function automatic [31:0] crc32_word(input [31:0] value, input [63:0] data);
        integer i;
        reg [31:0] c;
        begin
            c = value;
            for (i = 0; i < 64; i = i + 1)
                c = (c[0] ^ data[i]) ? ((c >> 1) ^ POLY) : (c >> 1);
            crc32_word = c;
        end
    endfunction

    reg passing, finished;            // the pass runs; it has answered
    reg [18:0] issued;                // sectors asked for
    reg [24:0] beats;                 // words returned in all
    reg [31:0] state;
    wire [24:0] total = {sectors[18:0], 6'd0};
    wire owed = beats != {issued, 6'd0};
    wire pass_read = passing && verify && issued != sectors[18:0];
    wire pass_port = passing || owed;  // the pass owns the port
    wire taken = aux_read && !aux_waitrequest;

    assign aux_address = IMAGE_WORD + (pass_port ? {4'd0, issued, 6'd0} : {5'd0, disk_address});
    assign aux_burstcount = pass_port ? BURST : 8'd1;
    assign aux_read = pass_port ? pass_read : disk_valid && !disk_write;
    assign aux_write = !pass_port && disk_valid && disk_write;
    assign aux_writedata = disk_wdata;
    assign aux_byteenable = 8'hff;
    assign disk_ready = !pass_port && disk_valid && !aux_waitrequest;
    assign disk_rvalid = !pass_port && aux_readdatavalid;
    assign disk_rdata = readdata;

    always @(posedge clk) begin
        verify_done <= 0;
        if (reset) begin
            passing <= 0; finished <= 0; issued <= 0; beats <= 0; state <= 0; verify_match <= 0;
        end else begin
            if (!verify) finished <= 0;
            if (verify && !passing && !finished && !owed) begin
                passing <= 1; issued <= 0; beats <= 0; state <= 32'hffffffff;
            end
            if (taken && pass_port) issued <= issued + 1'b1;
            if (aux_readdatavalid && pass_port) begin
                beats <= beats + 1'b1;
                state <= crc32_word(state, readdata);
            end
            if (passing && !verify) passing <= 0;   // abandoned: the owed beats drain
            else if (passing && beats == total && !owed) begin
                passing <= 0; finished <= 1;
                verify_done <= 1;
                verify_match <= ~state == crc;
            end
        end
    end
    wire unused = ^sectors[31:19];
endmodule
