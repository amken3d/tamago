// Qualcomm QCM2290 PSCI support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

// func smc(fn, a0, a1, a2 uint64) uint64
TEXT ·smc(SB),$0-40
	MOVD	fn+0(FP), R0
	MOVD	a0+8(FP), R1
	MOVD	a1+16(FP), R2
	MOVD	a2+24(FP), R3

	// SMC #0
	WORD	$0xd4000003

	MOVD	R0, ret+32(FP)

	RET
