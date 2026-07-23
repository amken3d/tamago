// ARM processor support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build !linkcpuinit

#include "textflag.h"

TEXT cpuinit(SB),NOSPLIT|NOFRAME,$0
	// set stack pointer
	MOVW	runtime∕goos·RamStart(SB), R13
	MOVW	runtime∕goos·RamSize(SB), R1
	MOVW	runtime∕goos·RamStackOffset(SB), R2
	ADD	R1, R13
	SUB	R2, R13
	MOVW	R13, R3

	// detect HYP mode and switch to SVC if necessary
	WORD	$0xe10f0000	// mrs r0, CPSR
	AND	$0x1f, R0, R0	// get processor mode

	CMP	$0x10, R0	// USR mode
	BL.EQ	_rt0_tamago_start(SB)

	CMP	$0x1a, R0	// HYP mode
	B.NE	after_eret

	// Entered in HYP mode (e.g. Raspberry Pi firmware): configure the
	// hypervisor state we are about to leave behind, as its reset values
	// can otherwise trap PL1 operation back to HYP (whose vectors are not
	// set): allow PL1/0 access to cp10/cp11 (VFP), disable all HYP traps,
	// zero the virtual counter offset.
	MRC	15, 4, R1, C1, C1, 2	// HCPTR
	BIC	$(1<<10), R1		// clear TCP10 (VFP traps)
	BIC	$(1<<11), R1		// clear TCP11
	MCR	15, 4, R1, C1, C1, 2
	MOVW	$0, R1
	MCR	15, 4, R1, C1, C1, 0	// HCR = 0: no HYP traps
	MOVW	$0, R2
	WORD	$0xec421f4e		// mcrr p15, 4, r1, r2, c14 (CNTVOFF = 0)

	BIC	$0x1f, R0
	ORR	$0x1d3, R0	// AIF masked, SVC mode
	// add lr, pc, #8: PC reads as current instruction +8, so this yields
	// this instruction +16 = after_eret. (Upstream uses #12, which lands
	// one instruction past after_eret, skipping the SYS mode switch.)
	MOVW	$8(R15), R14
	WORD	$0xe16ff000	// msr SPSR_fsxc, r0
	WORD	$0xe12ef30e	// msr ELR_hyp, lr
	WORD	$0xe160006e	// eret

after_eret:
	// enter System Mode
	WORD	$0xe321f0df	// msr CPSR_c, 0xdf

	// sanitize SCTLR: use VBAR-based vectors (clear V) taken in ARM state
	// (clear TE), regardless of what the firmware left behind
	MRC	15, 0, R1, C1, C0, 0
	BIC	$(1<<13), R1	// V: low/VBAR vectors
	BIC	$(1<<30), R1	// TE: exceptions taken in ARM state
	MCR	15, 0, R1, C1, C0, 0

	MOVW	R3, R13
	B	_rt0_tamago_start(SB)

