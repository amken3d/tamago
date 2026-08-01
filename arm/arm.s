// ARM processor support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// func read_cpsr() uint32
TEXT ·read_cpsr(SB),$0-4
	// ARM Architecture Reference Manual ARMv7-A and ARMv7-R edition
	// B1.3.3 Program Status Registers (PSRs)
	WORD	$0xe10f0000 // mrs r0, CPSR
	MOVW	R0, ret+0(FP)

	RET

// func exit(int32)
TEXT ·exit(SB),$0-4
	// wait forever in low-power state
	WORD	$0xe10f0000 // mrs r0, CPSR
	ORR	$1<<7, R0   // mask IRQs
	WORD	$0xe121f000 // msr CPSR_c, r0
	WORD	$0xe320f003 // wfi

// func read_dfsr() uint32
TEXT ·read_dfsr(SB),$0-4
	// ARM Architecture Reference Manual ARMv7-A/R, B4.1.51:
	// DFSR, Data Fault Status Register
	MRC	15, 0, R0, C5, C0, 0
	MOVW	R0, ret+0(FP)

	RET

// func read_dfar() uint32
TEXT ·read_dfar(SB),$0-4
	// B4.1.50: DFAR, Data Fault Address Register -- the address whose access
	// faulted. The single most useful datum when diagnosing a data abort.
	MRC	15, 0, R0, C6, C0, 0
	MOVW	R0, ret+0(FP)

	RET

// func read_ifsr() uint32
TEXT ·read_ifsr(SB),$0-4
	// B4.1.96: IFSR, Instruction Fault Status Register
	MRC	15, 0, R0, C5, C0, 1
	MOVW	R0, ret+0(FP)

	RET

// func read_ifar() uint32
TEXT ·read_ifar(SB),$0-4
	// B4.1.95: IFAR, Instruction Fault Address Register
	MRC	15, 0, R0, C6, C0, 2
	MOVW	R0, ret+0(FP)

	RET
