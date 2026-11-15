// SPDX-License-Identifier: GPL-3.0-or-later
// Relative, private menu CSR prototype. No board mapping or admission grant.
// STATUS latches a coherent result; each command is generation bound.
module fes_embedded_menu_control #(
    parameter [31:0] WINDOW_BASE = 32'h95000000,
    parameter [31:0] WINDOW_BYTES = 32'd8388608
)(
    input wire clk, reset_n, valid, write, maintenance_held,
    input wire [7:0] address,
    input wire [31:0] wdata,
    input wire [3:0] wstrb,
    output reg [31:0] rdata,
    output reg error,
    output wire start_ready, submit_ready, running, stopped, fault,
    output wire [31:0] displayed_sequence, underflows,
    output wire [23:0] rgb,
    output wire de, hs, vs,
    output wire reader_valid,
    output wire [31:0] reader_address,
    output wire [7:0] reader_length,
    input wire reader_ready, reader_abort,
    input wire [63:0] reader_generation,
    input wire reader_rvalid,
    input wire [127:0] reader_rdata,
    output wire reader_rready
);
    localparam [7:0] COMMAND=8'h80, EXPECT_LO=8'h84, EXPECT_HI=8'h88,
        SEQUENCE=8'h8c, SLOT=8'h90, STATUS=8'h94, RESULT_LO=8'h98,
        RESULT_HI=8'h9c, ACCEPTED=8'ha0, DISPLAYED=8'ha4, UNDERFLOWS=8'ha8,
        RESULT_SLOT=8'hac, BASE=8'hb0, SPAN=8'hb4, STRIDE=8'hb8,
        WIDTH=8'hbc, HEIGHT=8'hc0, MAGIC=8'hc4, VERSION=8'hc8, MAINTENANCE_HELD=8'hcc;
    reg [63:0] expected_generation, active_generation, snapshot_generation;
    reg [31:0] staged_sequence, accepted_sequence, snapshot_accepted,
        snapshot_displayed, snapshot_underflows;
    reg staged_slot, accepted_slot, snapshot_slot;
    // One descriptor accepted by the CSR is held until the video side is ready.
    reg submit_pending, pending_slot;
    reg [31:0] pending_sequence;
    wire scanout_submit_ready;
    assign submit_ready = same_owner && active_generation == reader_generation &&
        running && !stopped && !fault && !submit_pending &&
        displayed_sequence == accepted_sequence;
    wire same_owner = expected_generation != 0 && expected_generation == reader_generation && !reader_abort;
    wire start_allowed = same_owner && start_ready && staged_sequence > accepted_sequence;
    wire submit_allowed = same_owner && active_generation == reader_generation && submit_ready &&
        staged_sequence > accepted_sequence && staged_slot != accepted_slot;
    wire stop_allowed = same_owner && active_generation == reader_generation && running && !fault;
    wire command = valid && write && address == COMMAND && wstrb == 4'hf;
    wire start = command && wdata == 1 && start_allowed;
    wire accept_submit = command && wdata == 2 && submit_allowed;
    wire submit = submit_pending && same_owner && active_generation == reader_generation &&
        scanout_submit_ready && !stop;
    wire stop = command && wdata == 3 && stop_allowed;
    wire [31:0] flags = {26'd0,reader_abort,fault,stopped,submit_ready,running,start_ready};

    fes_embedded_menu_scanout #(.WINDOW_BASE(WINDOW_BASE), .WINDOW_BYTES(WINDOW_BYTES)) scanout (
        .clk(clk), .reset_n(reset_n), .start(start), .initial_slot(staged_slot),
        .initial_sequence(staged_sequence), .start_ready(start_ready),
        .submit_valid(submit), .submit_slot(pending_slot), .submit_sequence(pending_sequence),
        .submit_ready(scanout_submit_ready), .quiesce(stop), .running(running), .stopped(stopped), .fault(fault),
        .displayed_sequence(displayed_sequence), .underflows(underflows), .rgb(rgb), .de(de), .hs(hs), .vs(vs),
        .reader_valid(reader_valid), .reader_address(reader_address), .reader_length(reader_length),
        .reader_ready(reader_ready), .reader_abort(reader_abort), .reader_generation(reader_generation),
        .reader_rvalid(reader_rvalid), .reader_rdata(reader_rdata), .reader_rready(reader_rready)
    );
    always @* begin
        rdata=0; error=0;
        if (valid) begin
            if (address[1:0]!=0 || wstrb!=4'hf) error=1;
            else if (write) case (address)
                EXPECT_LO,EXPECT_HI,SEQUENCE: ;
                SLOT: if (wdata>1) error=1;
                COMMAND: if (!((wdata==1 && start_allowed) || (wdata==2 && submit_allowed) ||
                              (wdata==3 && stop_allowed))) error=1;
                default: error=1;
            endcase
            else case (address)
                COMMAND: rdata=0;
                EXPECT_LO: rdata=expected_generation[31:0];
                EXPECT_HI: rdata=expected_generation[63:32];
                SEQUENCE: rdata=staged_sequence;
                SLOT: rdata={31'd0,staged_slot};
                STATUS: rdata=flags;
                RESULT_LO: rdata=snapshot_generation[31:0];
                RESULT_HI: rdata=snapshot_generation[63:32];
                ACCEPTED: rdata=snapshot_accepted;
                DISPLAYED: rdata=snapshot_displayed;
                UNDERFLOWS: rdata=snapshot_underflows;
                RESULT_SLOT: rdata={31'd0,snapshot_slot};
                BASE: rdata=WINDOW_BASE;
                SPAN: rdata=WINDOW_BYTES;
                STRIDE: rdata=5120;
                WIDTH: rdata=1280;
                HEIGHT: rdata=720;
                MAGIC: rdata=32'h31534d46; // FMS1
                VERSION: rdata=1;
                MAINTENANCE_HELD: rdata={31'd0,maintenance_held};
                default: error=1;
            endcase
        end
    end
    always @(posedge clk) begin
        if (!reset_n) begin
            expected_generation<=0; active_generation<=0; snapshot_generation<=0;
            staged_sequence<=0; accepted_sequence<=0; snapshot_accepted<=0;
            snapshot_displayed<=0; snapshot_underflows<=0;
            staged_slot<=0; accepted_slot<=0; snapshot_slot<=0;
            submit_pending<=0; pending_slot<=0; pending_sequence<=0;
        end else begin
            if (start) active_generation<=reader_generation;
            if (start || stop || reader_abort || reader_generation != active_generation || submit)
                submit_pending<=0;
            if (accept_submit) begin
                submit_pending<=1;
                pending_slot<=staged_slot;
                pending_sequence<=staged_sequence;
            end
            if (start || accept_submit) begin
                accepted_sequence<=staged_sequence;
                accepted_slot<=staged_slot;
            end
            if (valid && !error && write) case (address)
                EXPECT_LO: expected_generation[31:0]<=wdata;
                EXPECT_HI: expected_generation[63:32]<=wdata;
                SEQUENCE: staged_sequence<=wdata;
                SLOT: staged_slot<=wdata[0];
                default: ;
            endcase
            if (valid && !error && !write && address==STATUS) begin
                snapshot_generation<=active_generation;
                snapshot_accepted<=accepted_sequence;
                snapshot_displayed<=displayed_sequence;
                snapshot_underflows<=underflows;
                snapshot_slot<=accepted_slot;
            end
        end
    end
endmodule
