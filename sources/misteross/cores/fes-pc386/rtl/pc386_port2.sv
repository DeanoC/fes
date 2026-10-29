// SPDX-License-Identifier: MIT
// DDR port 2 arbiter (docs/pc386-machine-design.md, "HPS DDR window and
// ports"): the display plane's reads (through pc386_plane_bridge), the
// presenter's line reads, the VGA capture writes and the shell's single-beat
// reads and writes (aux: the ATA image and pc386_host_link), in that
// priority. A master is granted for one command; a write burst keeps the
// grant until its last beat. Read data returns in command order to the
// master that asked.
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
    // Shell: single-beat reads and writes
    input wire [28:0] aux_address,
    input wire aux_read, aux_write,
    input wire [63:0] aux_writedata,
    input wire [7:0] aux_byteenable,
    output wire aux_waitrequest,
    output wire aux_readdatavalid,
    // Port 2
    output wire [28:0] address,
    output wire [7:0] burstcount,
    output wire read, write,
    output wire [63:0] writedata,
    output wire [7:0] byteenable,
    input wire waitrequest,
    input wire readdatavalid
);
    localparam [1:0] PLANE = 2'd0, PRESENT = 2'd1, CAPTURE = 2'd2, AUX = 2'd3;
    reg granted;
    reg [1:0] owner;
    reg [7:0] write_left;
    wire [1:0] choice = plane_read ? PLANE : present_read ? PRESENT : capture_write ? CAPTURE : AUX;
    wire any = plane_read || present_read || capture_write || aux_read || aux_write;
    assign address = owner == PLANE ? plane_address : owner == PRESENT ? present_address :
                     owner == CAPTURE ? capture_address : aux_address;
    assign burstcount = owner == PLANE ? plane_burstcount : owner == PRESENT ? present_burstcount :
                        owner == CAPTURE ? capture_burstcount : 8'd1;
    assign read = granted && (owner == PLANE ? plane_read : owner == PRESENT ? present_read : owner == AUX && aux_read);
    assign write = granted && (owner == CAPTURE ? capture_write : owner == AUX && aux_write);
    assign writedata = owner == AUX ? aux_writedata : capture_writedata;
    assign byteenable = owner == AUX ? aux_byteenable : 8'hff;
    assign plane_waitrequest = !(granted && owner == PLANE) || waitrequest;
    assign present_waitrequest = !(granted && owner == PRESENT) || waitrequest;
    assign capture_waitrequest = !(granted && owner == CAPTURE) || waitrequest;
    assign aux_waitrequest = !(granted && owner == AUX) || waitrequest;
    wire read_taken = read && !waitrequest;
    wire write_taken = write && !waitrequest;

    // Owners of outstanding reads: {owner, beats}.
    wire [9:0] head;
    wire owners_empty, owners_full;
    wire [4:0] owners_count;
    reg [7:0] returned;
    wire last_beat = readdatavalid && returned + 8'd1 == head[7:0];
    pc386_fifo #(.WIDTH(10), .DEPTH(16)) owners (
        .clk(clk), .reset(reset), .push(read_taken), .din({owner, burstcount}), .full(owners_full),
        .pop(last_beat), .dout(head), .empty(owners_empty), .count(owners_count)
    );
    assign plane_readdatavalid = readdatavalid && head[9:8] == PLANE;
    assign present_readdatavalid = readdatavalid && head[9:8] == PRESENT;
    assign aux_readdatavalid = readdatavalid && head[9:8] == AUX;

    always @(posedge clk) begin
        if (reset) begin
            granted <= 0; owner <= PLANE; write_left <= 0; returned <= 0;
        end else begin
            if (readdatavalid) returned <= last_beat ? 8'd0 : returned + 8'd1;
            if (!granted) begin
                // A new grant waits while the read queue is full.
                if (any && !owners_full) begin granted <= 1; owner <= choice; end
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
