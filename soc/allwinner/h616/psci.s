// Allwinner H616 PSCI support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the h616 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// func smc(fn, a0, a1, a2 uint64) uint64
TEXT ·smc(SB),$0-40
	MOVD	fn+0(FP), R0
	MOVD	a0+8(FP), R1
	MOVD	a1+16(FP), R2
	MOVD	a2+24(FP), R3

	// debug tripwire: raw 'A' to UART0 THR right before the SMC
	MOVD	$0x05000000, R4
	MOVD	$0x41, R5
	MOVW	R5, (R4)

	// SMC #0
	WORD	$0xd4000003

	// debug tripwire: raw 'B' right after the SMC returns
	MOVD	$0x05000000, R4
	MOVD	$0x42, R5
	MOVW	R5, (R4)

	MOVD	R0, ret+32(FP)

	RET
