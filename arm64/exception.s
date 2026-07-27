// ARM64 processor support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

#include "textflag.h"

TEXT ·handleException(SB),NOSPLIT|NOFRAME,$0
	// debug tripwire: raw 'E' to UART0 THR before anything that could
	// fault again (no stack or memory use)
	MOVD	$0x05000000, R1
	MOVD	$0x45, R0
	MOVW	R0, (R1)

	MRS	ELR_EL1, R0
	MOVD	R0, 8(RSP)	// arg
	JMP	·systemException(SB)

// func set_vbar(addr uint64)
TEXT ·set_vbar(SB),NOSPLIT,$0
	MOVD	addr+0(FP), R0
	MSR	R0, VBAR_EL1
	RET

// func read_el() uint64
TEXT ·read_el(SB),$0-8
	MRS	CurrentEL, R0
	MOVD	R0, ret+0(FP)
	RET

// func read_esr() uint64
TEXT ·read_esr(SB),$0-8
	MRS	ESR_EL1, R0
	MOVD	R0, ret+0(FP)
	RET

// func read_far() uint64
TEXT ·read_far(SB),$0-8
	MRS	FAR_EL1, R0
	MOVD	R0, ret+0(FP)
	RET

// func sync_icache(start, end uint64)
//
// Points of unification: clean D-cache lines to PoU, invalidate I-cache
// lines, so that opcodes written as data become visible to instruction
// fetch (B2.4.4 ARM ARM ARMv8).
TEXT ·sync_icache(SB),NOSPLIT,$0-16
	MOVD	start+0(FP), R0
	MOVD	end+8(FP), R1

	MOVD	R0, R2
dc_loop:
	WORD	$0xd50b7b22	// dc cvau, x2
	ADD	$64, R2
	CMP	R1, R2
	BLT	dc_loop

	WORD	$0xd5033b9f	// dsb ish

	MOVD	R0, R2
ic_loop:
	WORD	$0xd50b7522	// ic ivau, x2
	ADD	$64, R2
	CMP	R1, R2
	BLT	ic_loop

	WORD	$0xd5033b9f	// dsb ish
	WORD	$0xd5033fdf	// isb
	RET

// EL2 catcher debug reporter: entered from the runtime-synthesized EL2
// vector stubs (init.s) with the slot index in R9. Prints slot and
// SPSR/ESR/ELR/FAR_EL2 over UART0 (16550, base 0x05000000), then parks.
// Runs at EL2 with MMU off and no usable stack; BL nesting is one deep.

// char in R0, UART base in R1, clobbers R7
TEXT ·el2Putc(SB),NOSPLIT|NOFRAME,$0
putcwait:
	MOVWU	0x14(R1), R7	// LSR
	AND	$0x20, R7, R7	// THRE
	CBZ	R7, putcwait
	MOVW	R0, (R1)
	RET

// value in R2, UART base in R1, clobbers R0, R7, R11
TEXT ·el2PutHex(SB),NOSPLIT|NOFRAME,$0
	MOVD	$60, R11
hexloop:
	LSR	R11, R2, R0
	AND	$0xf, R0, R0
	CMP	$10, R0
	BLT	hexdigit
	ADD	$87, R0		// 'a' - 10
	B	hexemit
hexdigit:
	ADD	$48, R0		// '0'
hexemit:
	MOVWU	0x14(R1), R7
	AND	$0x20, R7, R7
	CBZ	R7, hexemit
	MOVW	R0, (R1)
	SUB	$4, R11, R11
	CMP	$0, R11
	BGE	hexloop
	RET

TEXT ·el2Catch(SB),NOSPLIT|NOFRAME,$0
	MOVD	$0x05000000, R1

	MOVD	$0x0d, R0	// \r
	BL	·el2Putc(SB)
	MOVD	$0x0a, R0	// \n
	BL	·el2Putc(SB)
	MOVD	$0x32, R0	// '2'
	BL	·el2Putc(SB)
	MOVD	$0x21, R0	// '!'
	BL	·el2Putc(SB)

	ADD	$0x30, R9, R0	// slot: '0'..'9',':'..'?' for 10..15
	BL	·el2Putc(SB)

	MOVD	$0x20, R0
	BL	·el2Putc(SB)
	WORD	$0xd53c4005	// mrs x5, spsr_el2
	MOVD	R5, R2
	BL	·el2PutHex(SB)

	MOVD	$0x20, R0
	BL	·el2Putc(SB)
	WORD	$0xd53c5202	// mrs x2, esr_el2
	BL	·el2PutHex(SB)

	MOVD	$0x20, R0
	BL	·el2Putc(SB)
	WORD	$0xd53c4023	// mrs x3, elr_el2
	MOVD	R3, R2
	BL	·el2PutHex(SB)

	MOVD	$0x20, R0
	BL	·el2Putc(SB)
	WORD	$0xd53c6004	// mrs x4, far_el2
	MOVD	R4, R2
	BL	·el2PutHex(SB)

el2park:
	B	el2park
