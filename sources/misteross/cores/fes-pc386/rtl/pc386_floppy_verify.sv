// SPDX-License-Identifier: MIT
`include "fes_application.vh"
`include "fes_computer.vh"

// The CRC pass of fes.media.pc-floppy's Commit (mister-packages
// docs/computer-io.md, "PC floppy"), on port 2's floppy master between the
// controller (pc386_fdc) and the port.
//
// While verify[k] is high (drive k's unit is verifying, pc386_floppy_media)
// the pass reads sectors[k] sectors of drive k's image, one 64-beat burst a
// sector, and runs CRC-32/IEEE over them, a word per beat, first byte lowest.
// It then pulses done[k], with match when the CRC is crc[k], and waits for
// verify[k] to fall. A floppy commits while the machine runs, so the pass
// shares the port with the controller: it starts a burst only while the
// controller presents nothing and is owed no beat, and the controller waits
// (waitrequest) while a burst of the pass is asked or owed. The controller
// never reads a verifying drive, which is empty until it is ready.
module pc386_floppy_verify (
    input wire clk, reset,
    input wire [1:0] verify,
    input wire [1:0][12:0] sectors,
    input wire [1:0][31:0] crc,
    output reg [1:0] done,
    output reg match,
    // The controller's image port
    input wire [28:0] fdc_address,
    input wire [7:0] fdc_burstcount,
    input wire fdc_read, fdc_write,
    input wire [63:0] fdc_writedata,
    input wire [7:0] fdc_byteenable,
    output wire fdc_waitrequest, fdc_readdatavalid,
    // Port 2's floppy master (read data on its shared readdata)
    output wire [28:0] port_address,
    output wire [7:0] port_burstcount,
    output wire port_read, port_write,
    output wire [63:0] port_writedata,
    output wire [7:0] port_byteenable,
    input wire port_waitrequest, port_readdatavalid,
    input wire [63:0] readdata
);
    localparam [31:0] IMAGE0 = `FES_APPLICATION_HPS_DDR_WINDOW_BASE + `FES_COMPUTER_PC_FLOPPY_WINDOW_OFFSET;
    localparam [28:0] IMAGE0_WORD = IMAGE0[31:3];
    localparam [28:0] DRIVE_WORDS = 29'(`FES_COMPUTER_PC_FLOPPY_WINDOW_STRIDE / 8);
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

    reg passing, drive;                // a pass runs, for drive
    reg [1:0] finished;                // answered; wait for verify to fall
    reg [12:0] issued;                 // sectors asked for
    reg [6:0] mine;                    // the pass's beats still owed
    reg [7:0] theirs;                  // the controller's beats still owed
    reg [31:0] state;
    reg [12:0] returned;               // sectors whose 64 beats have come back
    reg [5:0] beat;
    // The pass asks for a sector only while the controller is quiet.
    wire controller_quiet = !fdc_read && !fdc_write && theirs == 8'd0;
    wire pass_read = passing && verify[drive] && issued != sectors[drive] && mine == 7'd0 && controller_quiet;
    wire pass_port = pass_read || mine != 7'd0;

    assign port_address = pass_port ? IMAGE0_WORD + (drive ? DRIVE_WORDS : 29'd0) + {10'd0, issued, 6'd0}
                                    : fdc_address;
    assign port_burstcount = pass_port ? 8'd64 : fdc_burstcount;
    assign port_read = pass_port ? pass_read : fdc_read;
    assign port_write = !pass_port && fdc_write;
    assign port_writedata = fdc_writedata;
    assign port_byteenable = pass_port ? 8'hff : fdc_byteenable;
    assign fdc_waitrequest = pass_port || port_waitrequest;
    assign fdc_readdatavalid = port_readdatavalid && mine == 7'd0;

    wire pass_taken = pass_read && !port_waitrequest;
    wire fdc_taken = !pass_port && fdc_read && !port_waitrequest;
    wire pass_beat = port_readdatavalid && mine != 7'd0;

    always @(posedge clk) begin
        done <= 2'b00;
        if (reset) begin
            passing <= 0; drive <= 0; finished <= 0; issued <= 0; mine <= 0; theirs <= 0;
            state <= 32'hffffffff; returned <= 0; beat <= 0; match <= 0;
        end else begin
            // The controller's outstanding read beats.
            theirs <= theirs + (fdc_taken ? fdc_burstcount : 8'd0)
                      - {7'd0, port_readdatavalid && mine == 7'd0 && theirs != 8'd0};
            for (int k = 0; k < 2; k++) if (!verify[k]) finished[k] <= 0;
            if (!passing) begin
                if (verify[0] && !finished[0]) begin
                    passing <= 1; drive <= 0;
                end else if (verify[1] && !finished[1]) begin
                    passing <= 1; drive <= 1;
                end
                issued <= 0; returned <= 0; beat <= 0; state <= 32'hffffffff;
            end else begin
                // A burst is asked only once the last one is back, so a
                // taken burst and a returning beat never share a clock.
                if (pass_taken) begin
                    issued <= issued + 13'd1;
                    mine <= 7'd64;
                end
                if (pass_beat) begin
                    state <= crc32_word(state, readdata);
                    beat <= beat + 6'd1;
                    mine <= mine - 7'd1;
                    if (beat == 6'd63) returned <= returned + 13'd1;
                end
                // Every sector back, or the drive no longer verifying: answer
                // (or drop) once no beat of the pass is owed.
                if (mine == 7'd0 && !pass_taken && (returned == sectors[drive] || !verify[drive])) begin
                    if (verify[drive]) begin
                        done[drive] <= 1;
                        match <= ~state == crc[drive];
                        finished[drive] <= 1;
                    end
                    passing <= 0;
                end
            end
        end
    end
endmodule
