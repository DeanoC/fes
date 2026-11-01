// Read-only allocated-buffer diagnostic. Disabled until an explicit valid SUBMIT.
// Registers, ownership and video/reader state all use the pixel clock. Platform
// Designer bridges own both memory/control crossings; no split-word CDC here.
`include "generated/fes_de25_scanout.vh"
module fes_de25_scanout #(
    parameter [127:0] BUILD_ID = 128'd0
) (
    input wire clk, reset_n,
    input wire clock_fault, display_ready,
    input wire [6:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output reg [31:0] readdata,
    output reg readdatavalid, writeresponsevalid,
    output wire [1:0] response,
    output wire waitrequest,
    output wire [31:0] avm_address,
    output wire [4:0] avm_burstcount,
    output wire avm_read,
    output wire [15:0] avm_byteenable,
    input wire avm_waitrequest,
    input wire [127:0] avm_readdata,
    input wire avm_readdatavalid,
    input wire [1:0] avm_response,
    output wire [23:0] video_rgb,
    output wire video_de, video_hs, video_vs
);
    reg [31:0] staged [0:6];
    reg configured, running, pending, queued_submit, pending_slot, displayed_slot;
    reg [31:0] grant_base, grant_span, accepted_sequence, displayed_sequence, errors;
    reg [63:0] generation;
    reg local_clear;
    // Mark the asynchronous ingress with Quartus-native CDC identification.
    (* preserve, dont_merge,
       altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED" *) reg clock_fault_meta;
    (* preserve, dont_merge *) reg clock_fault_sync;
    reg [31:0] snapshot_serial;
    reg [31:0] frozen [0:12];
    integer field, lane;

    wire [27:0] reader_address;
    wire [7:0] reader_burstcount;
    wire video_submit_ready, video_quiesced;
    wire [31:0] video_displayed_sequence, current_sequence, underflows;
    wire current_slot;
    wire [1:0] memory_errors;
    wire [8:0] outstanding_beats;
    wire [23:0] rgb;
    wire de, hs, vs;
    wire [31:0] byte_address = {25'd0, address};
    wire [31:0] enabled_command = writedata & {{8{byteenable[3]}}, {8{byteenable[2]}},
                                               {8{byteenable[1]}}, {8{byteenable[0]}}};
    wire command_write = write && |byteenable && byte_address == `FES_DE25_SCANOUT_COMMAND_OFFSET;
    wire submit_command = command_write && enabled_command == `FES_DE25_SCANOUT_COMMAND_SUBMIT;
    wire stop_command = command_write && enabled_command == `FES_DE25_SCANOUT_COMMAND_STOP;
    wire clear_command = command_write && enabled_command == `FES_DE25_SCANOUT_COMMAND_CLEAR;
    wire snapshot_command = command_write && enabled_command == `FES_DE25_SCANOUT_COMMAND_SNAPSHOT;
    // An unexpected response invalidates local accounting, including an empty
    // counter. Never turn that fault into a local release/reuse proof.
    wire drained = !running && video_quiesced && outstanding_beats == 9'd0 &&
        !memory_errors[1] && !(|(errors & `FES_DE25_SCANOUT_ERROR_UNEXPECTED_RESPONSE));
    wire legal_clear = clear_command && drained;
    // The board source is sticky too; retaining this bit independently makes
    // a fault irrevocable locally even if its external level later recovers.
    wire clock_failed = clock_fault_sync || |(errors & `FES_DE25_SCANOUT_ERROR_CLOCK);
    wire [32:0] staged_end = {1'b0, staged[0]} + {1'b0, staged[2]};
    wire descriptor_valid = staged[1] == 32'd0 && staged[0][4:0] == 5'd0 &&
        staged[2] == `FES_DE25_SCANOUT_ALLOCATION_BYTES && staged_end <= 33'h100000000 &&
        {staged[4], staged[3]} != 64'd0 && staged[5] != 32'd0 && staged[6][31:1] == 31'd0;
    wire same_grant = staged[0] == grant_base && staged[1] == 32'd0 && staged[2] == grant_span &&
                      {staged[4], staged[3]} == generation;
    reg [31:0] new_errors;
    always @* begin
        new_errors = 32'd0;
        if (clock_failed) new_errors = new_errors | `FES_DE25_SCANOUT_ERROR_CLOCK;
        if (running && !display_ready) new_errors = new_errors | `FES_DE25_SCANOUT_ERROR_DISPLAY;
        // A legal CLEAR resets the local reader/video on the following edge.
        // Suppress only their old sticky errors through that reset cycle.
        if (!legal_clear && !local_clear) begin
            if (memory_errors[0]) new_errors = new_errors | `FES_DE25_SCANOUT_ERROR_RESPONSE;
            if (memory_errors[1]) new_errors = new_errors | `FES_DE25_SCANOUT_ERROR_UNEXPECTED_RESPONSE;
            if (underflows != 32'd0) new_errors = new_errors | `FES_DE25_SCANOUT_ERROR_UNDERFLOW;
        end
        if (command_write && !submit_command && !stop_command && !clear_command && !snapshot_command)
            new_errors = new_errors | `FES_DE25_SCANOUT_ERROR_COMMAND;
        if (clear_command && !drained)
            new_errors = new_errors | `FES_DE25_SCANOUT_ERROR_COMMAND;
        if (submit_command) begin
            if (errors != 32'd0 || clock_failed || local_clear || pending || (!running && !drained))
                new_errors = new_errors | `FES_DE25_SCANOUT_ERROR_COMMAND;
            if (!descriptor_valid || (configured && !same_grant))
                new_errors = new_errors | `FES_DE25_SCANOUT_ERROR_DESCRIPTOR;
            if (staged[5] == 32'd0 || (configured && staged[5] <= accepted_sequence) ||
                staged[6][31:1] != 31'd0 || staged[6][0] == displayed_slot)
                new_errors = new_errors | `FES_DE25_SCANOUT_ERROR_SEQUENCE;
        end
    end
    wire [31:0] live_errors = errors | new_errors;
    wire [31:0] live_status = (configured ? `FES_DE25_SCANOUT_STATUS_CONFIGURED : 32'd0) |
        (running ? `FES_DE25_SCANOUT_STATUS_RUNNING : 32'd0) |
        (pending ? `FES_DE25_SCANOUT_STATUS_PENDING : 32'd0) |
        (drained ? `FES_DE25_SCANOUT_STATUS_DRAINED : 32'd0) |
        (live_errors != 32'd0 ? `FES_DE25_SCANOUT_STATUS_FAULT : 32'd0) |
        (clock_failed ? `FES_DE25_SCANOUT_STATUS_CLOCK_FAULT : 32'd0) |
        (display_ready ? `FES_DE25_SCANOUT_STATUS_DISPLAY_READY : 32'd0);
    reg stage_write;
    reg [2:0] stage_index;
    always @* begin
        stage_write = 1'b1;
        stage_index = 3'd0;
        case (byte_address)
            `FES_DE25_SCANOUT_GRANT_LO_OFFSET: stage_index = 3'd0;
            `FES_DE25_SCANOUT_GRANT_HI_OFFSET: stage_index = 3'd1;
            `FES_DE25_SCANOUT_GRANT_SPAN_OFFSET: stage_index = 3'd2;
            `FES_DE25_SCANOUT_GENERATION_LO_OFFSET: stage_index = 3'd3;
            `FES_DE25_SCANOUT_GENERATION_HI_OFFSET: stage_index = 3'd4;
            `FES_DE25_SCANOUT_SEQUENCE_OFFSET: stage_index = 3'd5;
            `FES_DE25_SCANOUT_SLOT_OFFSET: stage_index = 3'd6;
            default: stage_write = 1'b0;
        endcase
    end
    assign response = 2'b00;
    assign waitrequest = !reset_n;
    assign avm_address = {reader_address, 4'd0};
    assign avm_burstcount = reader_burstcount[4:0];
    assign avm_byteenable = 16'hffff;
    // Keep command validation independent of quiesce/clear combinationally.
    // Registered command faults stop on their acceptance edge; data/clock
    // faults also blank immediately without withdrawing any existing read.
    wire video_enable = configured && running && errors == 32'd0 &&
        !clock_failed && display_ready && memory_errors == 2'b00 && underflows == 32'd0 && !local_clear;
    assign video_rgb = video_enable ? rgb : 24'd0;
    assign video_de = video_enable && de;
    // HS/VS are board timing, not scanout visibility. Keep them continuous
    // while stopped, faulted or reset so the HDMI shell can finish each pulse.
    assign video_hs = hs;
    assign video_vs = vs;

    fes_menu_video #(.DYNAMIC_BASE(1'b1), .MAX_BURST_BEATS(8'd16),
        .CHECK_RESPONSE(1'b1), .READ_AHEAD(1'b1), .RESET_ABORTS_TRAFFIC(1'b1), .COMPLETE_FRAME_ACK(1'b1),
        .PRESERVE_RASTER_ON_RESET(1'b1)) video (
        .clk(clk), .rst(!reset_n || local_clear), .enable(video_enable), .quiesce(!running),
        .window_base(grant_base), .window_base_valid(configured),
        .raster_h(11'd0), .raster_v(10'd0),
        .submit_valid(queued_submit), .submit_slot(pending_slot), .submit_sequence(accepted_sequence),
        .submit_ready(video_submit_ready), .displayed_sequence(video_displayed_sequence),
        .current_sequence(current_sequence), .current_slot(current_slot),
        .memory_errors(memory_errors), .outstanding_beats(outstanding_beats),
        .underflows(underflows), .quiesced(video_quiesced), .rgb(rgb), .alpha(), .de(de), .hs(hs), .vs(vs),
        .address(reader_address), .burstcount(reader_burstcount), .read(avm_read),
        .waitrequest(avm_waitrequest), .readdata(avm_readdata),
        .readdatavalid(avm_readdatavalid), .response(avm_response));

    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) begin
            clock_fault_meta <= 1'b0;
            clock_fault_sync <= 1'b0;
        end else begin
            clock_fault_meta <= clock_fault;
            clock_fault_sync <= clock_fault_meta;
        end
    end
    always @(posedge clk or negedge reset_n) begin
        if (!reset_n) begin
            for (field = 0; field < 7; field = field + 1) staged[field] <= 32'd0;
            for (field = 0; field < 13; field = field + 1) frozen[field] <= 32'd0;
            readdata <= 32'd0; readdatavalid <= 1'b0; writeresponsevalid <= 1'b0;
            configured <= 1'b0; running <= 1'b0; pending <= 1'b0; queued_submit <= 1'b0;
            pending_slot <= 1'b0; displayed_slot <= 1'b0;
            grant_base <= 32'd0; grant_span <= 32'd0; generation <= 64'd0;
            accepted_sequence <= 32'd0; displayed_sequence <= 32'd0; errors <= 32'd0;
            local_clear <= 1'b0; snapshot_serial <= 32'd0;
        end else begin
            readdatavalid <= read;
            writeresponsevalid <= write;
            local_clear <= 1'b0;
            if (read) begin
                case (byte_address)
                    `FES_DE25_SCANOUT_ID_OFFSET: readdata <= `FES_DE25_SCANOUT_IDENTITY;
                    `FES_DE25_SCANOUT_VERSION_OFFSET: readdata <= `FES_DE25_SCANOUT_VERSION;
                    `FES_DE25_SCANOUT_STATUS_OFFSET: readdata <= live_status;
                    `FES_DE25_SCANOUT_GRANT_LO_OFFSET: readdata <= staged[0];
                    `FES_DE25_SCANOUT_GRANT_HI_OFFSET: readdata <= staged[1];
                    `FES_DE25_SCANOUT_GRANT_SPAN_OFFSET: readdata <= staged[2];
                    `FES_DE25_SCANOUT_GENERATION_LO_OFFSET: readdata <= staged[3];
                    `FES_DE25_SCANOUT_GENERATION_HI_OFFSET: readdata <= staged[4];
                    `FES_DE25_SCANOUT_SEQUENCE_OFFSET: readdata <= staged[5];
                    `FES_DE25_SCANOUT_SLOT_OFFSET: readdata <= staged[6];
                    `FES_DE25_SCANOUT_SNAPSHOT_SERIAL_OFFSET: readdata <= snapshot_serial;
                    `FES_DE25_SCANOUT_SNAPSHOT_STATUS_OFFSET: readdata <= frozen[0];
                    `FES_DE25_SCANOUT_SNAPSHOT_GENERATION_LO_OFFSET: readdata <= frozen[1];
                    `FES_DE25_SCANOUT_SNAPSHOT_GENERATION_HI_OFFSET: readdata <= frozen[2];
                    `FES_DE25_SCANOUT_SNAPSHOT_ACCEPTED_SEQUENCE_OFFSET: readdata <= frozen[3];
                    `FES_DE25_SCANOUT_SNAPSHOT_DISPLAYED_SEQUENCE_OFFSET: readdata <= frozen[4];
                    `FES_DE25_SCANOUT_SNAPSHOT_CURRENT_SEQUENCE_OFFSET: readdata <= frozen[5];
                    `FES_DE25_SCANOUT_SNAPSHOT_SLOTS_OFFSET: readdata <= frozen[6];
                    `FES_DE25_SCANOUT_SNAPSHOT_ERRORS_OFFSET: readdata <= frozen[7];
                    `FES_DE25_SCANOUT_SNAPSHOT_UNDERFLOWS_OFFSET: readdata <= frozen[8];
                    `FES_DE25_SCANOUT_SNAPSHOT_OUTSTANDING_OFFSET: readdata <= frozen[9];
                    `FES_DE25_SCANOUT_SNAPSHOT_GRANT_LO_OFFSET: readdata <= frozen[10];
                    `FES_DE25_SCANOUT_SNAPSHOT_GRANT_HI_OFFSET: readdata <= frozen[11];
                    `FES_DE25_SCANOUT_SNAPSHOT_GRANT_SPAN_OFFSET: readdata <= frozen[12];
                    `FES_DE25_SCANOUT_BUILD_ID0_OFFSET: readdata <= BUILD_ID[31:0];
                    `FES_DE25_SCANOUT_BUILD_ID1_OFFSET: readdata <= BUILD_ID[63:32];
                    `FES_DE25_SCANOUT_BUILD_ID2_OFFSET: readdata <= BUILD_ID[95:64];
                    `FES_DE25_SCANOUT_BUILD_ID3_OFFSET: readdata <= BUILD_ID[127:96];
                    default: readdata <= 32'd0;
                endcase
            end
            if (write && stage_write)
                for (lane = 0; lane < 4; lane = lane + 1)
                    if (byteenable[lane]) staged[stage_index][8*lane +: 8] <= writedata[8*lane +: 8];
            if (queued_submit && video_submit_ready) queued_submit <= 1'b0;
            if (pending && running && live_errors == 32'd0 && video_displayed_sequence == accepted_sequence) begin
                displayed_sequence <= video_displayed_sequence;
                displayed_slot <= pending_slot;
                pending <= 1'b0;
            end
            if (submit_command && new_errors == 32'd0) begin
                if (!configured) begin
                    grant_base <= staged[0]; grant_span <= staged[2]; generation <= {staged[4], staged[3]};
                    configured <= 1'b1;
                end
                running <= 1'b1; pending <= 1'b1; queued_submit <= 1'b1;
                accepted_sequence <= staged[5]; pending_slot <= staged[6][0];
            end
            if (stop_command || new_errors != 32'd0) begin
                running <= 1'b0; pending <= 1'b0; queued_submit <= 1'b0;
            end
            errors <= live_errors;
            if (legal_clear) begin
                configured <= 1'b0; running <= 1'b0; pending <= 1'b0; queued_submit <= 1'b0;
                grant_base <= 32'd0; grant_span <= 32'd0; generation <= 64'd0;
                accepted_sequence <= 32'd0; displayed_sequence <= 32'd0;
                pending_slot <= 1'b0; displayed_slot <= 1'b0;
                errors <= clock_failed ? `FES_DE25_SCANOUT_ERROR_CLOCK : 32'd0;
                local_clear <= 1'b1;
                for (field = 0; field < 7; field = field + 1) staged[field] <= 32'd0;
            end
            // Snapshot registers and their serial change only together here,
            // never on live counter changes, STOP or CLEAR.
            if (snapshot_command) begin
                snapshot_serial <= snapshot_serial + 32'd1;
                frozen[0] <= live_status; frozen[1] <= generation[31:0]; frozen[2] <= generation[63:32];
                frozen[3] <= accepted_sequence; frozen[4] <= displayed_sequence;
                frozen[5] <= configured ? current_sequence : 32'd0;
                frozen[6] <= {15'd0, pending_slot, 7'd0, displayed_slot, 7'd0, configured && current_slot};
                frozen[7] <= live_errors; frozen[8] <= configured ? underflows : 32'd0;
                frozen[9] <= {23'd0, outstanding_beats};
                frozen[10] <= grant_base; frozen[11] <= 32'd0; frozen[12] <= grant_span;
            end
        end
    end
endmodule
