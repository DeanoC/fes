//
// KF8255_Priority_Resolver
//
// Written by Kitune-san
//
`include "KF8259_Common_Package.svh"

module KF8259_Priority_Resolver (
    // Inputs from control logic
    input   logic   [2:0]   priority_rotate,
    input   logic   [7:0]   interrupt_mask,
    input   logic   [7:0]   interrupt_special_mask,
    input   logic           special_fully_nest_config,
    input   logic   [7:0]   highest_level_in_service,

    // Inputs
    input   logic   [7:0]   interrupt_request_register,
    input   logic   [7:0]   in_service_register,

    // Outputs
    output  logic   [7:0]   interrupt
);

    //
    // Masked flags
    //
    logic   [7:0]   masked_interrupt_request;
    assign masked_interrupt_request = interrupt_request_register & ~interrupt_mask;

    logic   [7:0]   masked_in_service;
    assign masked_in_service        = in_service_register & ~interrupt_special_mask;


    // Special fully nested mode moves the highest service bit one priority
    // lower. The lowest priority bit falls off instead of wrapping around.
    // This is the original rotated-domain expression in physical bit order.
    wire [7:0] effective_in_service;
    genvar i;
    generate for (i = 0; i < 8; i = i + 1) begin: level_priority
        localparam integer PREVIOUS = (i + 7) % 8;
        wire [7:0] higher = KF8259_Common_Package::higher_priority_mask(i, priority_rotate);
        assign effective_in_service[i] = special_fully_nest_config ?
            ((masked_in_service[i] & ~highest_level_in_service[i]) |
             (highest_level_in_service[PREVIOUS] && priority_rotate != 3'(PREVIOUS))) :
            masked_in_service[i];
        assign interrupt[i] = masked_interrupt_request[i] &&
            !(|(masked_interrupt_request & higher)) &&
            !(|(effective_in_service & (higher | (8'b1 << i))));
    end endgenerate

endmodule
