// SPDX-License-Identifier: MIT
// fes_i386_cpu for machine_tb --cpu rtl: the CPU's ports unchanged, plus the
// byte count of the load M1 sends to the data port. The port's eight bytes
// carry no size, and a VGA read has side effects (it loads the latches), so
// the bench reads only the bytes the load uses, with byte enables, as the
// L1D's uncached path will.
module pc386_rtl_cpu (
    input  logic             clk,
    input  logic             reset,
    input  logic [7:0][31:0] init_gpr,
    input  logic [31:0]      init_eflags,
    input  logic [31:0]      init_eip,
    input  logic [5:0][15:0] init_sel,
    input  logic [5:0][31:0] init_base,
    input  logic [5:0][31:0] init_limit,
    input  logic [5:0][15:0] init_ar,
    output logic [27:0]      mem_block,
    output logic             mem_load,
    input  logic [15:0][7:0] mem_data,
    input  logic             mem_valid,
    output logic [31:0]      dmem_addr,
    output logic             dmem_read,
    output logic [3:0]       dmem_bytes,  // the load's bytes: 1, 2, 4, 6 or 8
    input  logic [7:0][7:0]  dmem_rdata,
    input  logic             dmem_valid,
    output logic             dmem_write,
    output logic [31:0]      dmem_waddr,
    output logic [7:0][7:0]  dmem_wdata,
    output logic [7:0]       dmem_wmask,
    output logic             io_req,
    output logic             io_write,
    output logic [15:0]      io_port,
    output logic [1:0]       io_size,
    output logic [31:0]      io_wdata,
    input  logic             io_ack,
    input  logic [31:0]      io_rdata,
    input  logic             intr,
    input  logic             nmi,
    output logic             inta_req,
    input  logic             inta_ack,
    input  logic [7:0]       inta_vector,
    output logic             trace_retire,
    output logic             trace_exception,
    output logic [7:0]       trace_vector,
    output logic             trace_halt,
    output logic             trace_unimplemented,
    output logic             trace_issue,
    output logic             trace_md_wait,
    output logic [7:0][31:0] arch_gpr,
    output logic [31:0]      arch_eflags,
    output logic [31:0]      arch_eip,
    output logic [5:0][15:0] arch_sel,
    output logic [5:0][31:0] arch_base,
    output logic [5:0][31:0] arch_limit,
    output logic [5:0][15:0] arch_ar
);
    fes_i386_cpu cpu (.*);
    assign dmem_bytes = cpu.m1_s.uop.mem.bytes;
endmodule
