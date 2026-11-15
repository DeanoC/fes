// SPDX-License-Identifier: MIT
// DDR port 2 arbiter (docs/pc386-machine-design.md, "HPS DDR window and
// ports"): the display plane's reads (through pc386_plane_bridge), the
// presenter's line reads, the VGA capture writes and the EMS board's single
// beats (pc386_ems), in that priority, then the storage clients in turn
// (docs/design/pc386-removable-media.md, "Port 2"): the shell's aux master
// (pc386_disk, the ATA image and its CRC pass, behind the status ring) and the
// floppy controller (pc386_fdc), each with reads of 1 to 128 beats and
// single-beat writes. When both storage clients ask, the one granted less
// recently goes first, so each issues one command between the other's. A
// master is granted for one command; a write burst keeps the grant until its
// last beat. Read data returns in command order to the master that asked.
module pc386_port2 (
    input wire clk, reset,
    // Plane bridge: reads
    input wire [28:0] plane_address,
    input wire [7:0] plane_burstcount,
    input wire plane_read,
    output wire plane_waitrequest,
    output wire plane_readdatavalid,
    // Presenter: reads
    input wire [28:0] present_address,
    input wire [7:0] present_burstcount,
    input wire present_read,
    output wire present_waitrequest,
    output wire present_readdatavalid,
    // Capture: writes
    input wire [28:0] capture_address,
    input wire [7:0] capture_burstcount,
    input wire capture_write,
    input wire [63:0] capture_writedata,
    output wire capture_waitrequest,
    // EMS board: single-beat reads and writes
    input wire [28:0] ems_address,
    input wire ems_read, ems_write,
    input wire [63:0] ems_writedata,
    input wire [7:0] ems_byteenable,
    output wire ems_waitrequest,
    output wire ems_readdatavalid,
    // Shell: reads of 1 to 128 beats, single-beat writes
    input wire [28:0] aux_address,
    input wire [7:0] aux_burstcount,
    input wire aux_read, aux_write,
    input wire [63:0] aux_writedata,
    input wire [7:0] aux_byteenable,
    output wire aux_waitrequest,
    output wire aux_readdatavalid,
    // Floppy controller: reads of 1 to 128 beats, single-beat writes
    input wire [28:0] fdc_address,
    input wire [7:0] fdc_burstcount,
    input wire fdc_read, fdc_write,
    input wire [63:0] fdc_writedata,
    input wire [7:0] fdc_byteenable,
    output wire fdc_waitrequest,
    output wire fdc_readdatavalid,
    // Port 2
    output wire [28:0] address,
    output wire [7:0] burstcount,
    output wire read, write,
    output wire [63:0] writedata,
    output wire [7:0] byteenable,
    input wire waitrequest,
    input wire readdatavalid
);
    localparam [2:0] PLANE = 3'd0, PRESENT = 3'd1, CAPTURE = 3'd2, EMS = 3'd3, AUX = 3'd4, FDC = 3'd5;
    reg granted;
    reg [2:0] owner;
    reg [7:0] write_left;
    reg fdc_last;                     // the floppy had the last storage grant
    wire aux_asks = aux_read || aux_write;
    wire fdc_asks = fdc_read || fdc_write;
    wire [2:0] choice = plane_read ? PLANE : present_read ? PRESENT : capture_write ? CAPTURE :
                        ems_read || ems_write ? EMS : fdc_asks && (!aux_asks || !fdc_last) ? FDC : AUX;
    wire any = plane_read || present_read || capture_write || ems_read || ems_write || aux_asks || fdc_asks;
    assign address = owner == PLANE ? plane_address : owner == PRESENT ? present_address :
                      owner == CAPTURE ? capture_address : owner == EMS ? ems_address :
                      owner == FDC ? fdc_address : aux_address;
    assign burstcount = owner == PLANE ? plane_burstcount : owner == PRESENT ? present_burstcount :
                        owner == CAPTURE ? capture_burstcount : owner == EMS ? 8'd1 :
                        owner == FDC ? (fdc_write ? 8'd1 : fdc_burstcount) : aux_write ? 8'd1 : aux_burstcount;
    assign read = granted && (owner == PLANE ? plane_read : owner == PRESENT ? present_read :
                              owner == EMS ? ems_read : owner == FDC ? fdc_read : owner == AUX && aux_read);
    assign write = granted && (owner == CAPTURE ? capture_write : owner == EMS ? ems_write :
                               owner == FDC ? fdc_write : owner == AUX && aux_write);
    assign writedata = owner == AUX ? aux_writedata : owner == FDC ? fdc_writedata :
                       owner == EMS ? ems_writedata : capture_writedata;
    assign byteenable = owner == AUX ? aux_byteenable : owner == FDC ? fdc_byteenable :
                        owner == EMS ? ems_byteenable : 8'hff;
    assign plane_waitrequest = !(granted && owner == PLANE) || waitrequest;
    assign present_waitrequest = !(granted && owner == PRESENT) || waitrequest;
    assign capture_waitrequest = !(granted && owner == CAPTURE) || waitrequest;
    assign ems_waitrequest = !(granted && owner == EMS) || waitrequest;
    assign aux_waitrequest = !(granted && owner == AUX) || waitrequest;
    assign fdc_waitrequest = !(granted && owner == FDC) || waitrequest;
    wire read_taken = read && !waitrequest;
    wire write_taken = write && !waitrequest;

    // Owners of outstanding reads: {owner, beats}.
    wire [10:0] head;
    wire owners_empty, owners_full;
    wire [4:0] owners_count;
    reg [7:0] returned;
    wire last_beat = readdatavalid && returned + 8'd1 == head[7:0];
    pc386_fifo #(.WIDTH(11), .DEPTH(16)) owners (
        .clk(clk), .reset(reset), .push(read_taken), .din({owner, burstcount}), .full(owners_full),
        .pop(last_beat), .dout(head), .empty(owners_empty), .count(owners_count)
    );
    assign plane_readdatavalid = readdatavalid && head[10:8] == PLANE;
    assign present_readdatavalid = readdatavalid && head[10:8] == PRESENT;
    assign ems_readdatavalid = readdatavalid && head[10:8] == EMS;
    assign aux_readdatavalid = readdatavalid && head[10:8] == AUX;
    assign fdc_readdatavalid = readdatavalid && head[10:8] == FDC;

    always @(posedge clk) begin
        if (reset) begin
            granted <= 0; owner <= PLANE; write_left <= 0; returned <= 0; fdc_last <= 0;
        end else begin
            if (readdatavalid) returned <= last_beat ? 8'd0 : returned + 8'd1;
            if (!granted) begin
                // A new grant waits while the read queue is full.
                if (any && !owners_full) begin
                    granted <= 1; owner <= choice;
                    if (choice == FDC) fdc_last <= 1;
                    else if (choice == AUX) fdc_last <= 0;
                end
            end else if (read_taken) granted <= 0;
            else if (write_taken) begin
                // The first beat sets the length; the grant ends with the last.
                if (write_left == 0) begin
                    if (burstcount == 8'd1) granted <= 0;
                    else write_left <= burstcount - 8'd1;
                end else begin
                    write_left <= write_left - 8'd1;
                    if (write_left == 8'd1) granted <= 0;
                end
            end else if (!read && !write && write_left == 0) granted <= 0;
        end
    end
    wire unused = ^{owners_empty, owners_count};
endmodule
