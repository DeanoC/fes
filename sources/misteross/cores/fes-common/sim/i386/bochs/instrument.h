// SPDX-License-Identifier: MIT
// Instrumentation hooks compiled into the pinned Bochs CPU for the FES i386
// oracle adapter (bochs_oracle.cpp). Bochs includes this header from cpu/cpu.h
// when configured with --enable-instrumentation=<this directory>. Only the
// hooks the adapter needs call out; the rest compile to nothing.
#ifndef FES_I386_BOCHS_INSTRUMENT_H
#define FES_I386_BOCHS_INSTRUMENT_H

class bxInstruction_c;

void fes_bx_before_execution(bxInstruction_c *i);
void fes_bx_after_execution(bxInstruction_c *i);
void fes_bx_repeat_iteration(bxInstruction_c *i);
void fes_bx_exception(unsigned vector, unsigned error_code);
void fes_bx_interrupt(unsigned vector);
void fes_bx_hwinterrupt(unsigned vector);
void fes_bx_lin_access(bx_address lin, bx_address phy, unsigned len, unsigned rw);

#define BX_INSTR_INIT_ENV()
#define BX_INSTR_EXIT_ENV()
#define BX_INSTR_INITIALIZE(cpu_id)
#define BX_INSTR_EXIT(cpu_id)
#define BX_INSTR_RESET(cpu_id, type)
#define BX_INSTR_HLT(cpu_id)
#define BX_INSTR_MWAIT(cpu_id, addr, len, flags)
#define BX_INSTR_DEBUG_PROMPT()
#define BX_INSTR_DEBUG_CMD(cmd)
#define BX_INSTR_NEAR_BRANCH_TAKEN(cpu_id, what, branch_eip, new_eip)
#define BX_INSTR_NEAR_BRANCH_NOT_TAKEN(cpu_id, branch_eip)
#define BX_INSTR_FAR_BRANCH(cpu_id, what, prev_cs, prev_eip, new_cs, new_eip)
#define BX_INSTR_OPCODE(cpu_id, i, opcode, len, is32, is64)
#define BX_INSTR_EXCEPTION(cpu_id, vector, error_code) fes_bx_exception(vector, error_code)
#define BX_INSTR_INTERRUPT(cpu_id, vector) fes_bx_interrupt(vector)
#define BX_INSTR_HWINTERRUPT(cpu_id, vector, cs, eip) fes_bx_hwinterrupt(vector)
#define BX_INSTR_CLFLUSH(cpu_id, laddr, paddr)
#define BX_INSTR_CACHE_CNTRL(cpu_id, what)
#define BX_INSTR_TLB_CNTRL(cpu_id, what, new_cr3)
#define BX_INSTR_PREFETCH_HINT(cpu_id, what, seg, offset)
#define BX_INSTR_BEFORE_EXECUTION(cpu_id, i) fes_bx_before_execution(i)
#define BX_INSTR_AFTER_EXECUTION(cpu_id, i) fes_bx_after_execution(i)
#define BX_INSTR_REPEAT_ITERATION(cpu_id, i) fes_bx_repeat_iteration(i)
#define BX_INSTR_LIN_ACCESS(cpu_id, lin, phy, len, memtype, rw) fes_bx_lin_access(lin, phy, len, rw)
#define BX_INSTR_PHY_ACCESS(cpu_id, phy, len, memtype, rw)
#define BX_INSTR_INP(addr, len)
#define BX_INSTR_INP2(addr, len, val)
#define BX_INSTR_OUTP(addr, len, val)
#define BX_INSTR_CPUID(cpu_id)
#define BX_INSTR_WRMSR(cpu_id, addr, value)
#define BX_INSTR_VMEXIT(cpu_id, reason, qualification)

#endif
