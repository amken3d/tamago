// ARM64 processor support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

#include "arm64.h"

// func flush_tlb()
TEXT ·flush_tlb(SB),$0
	// invalidate TLBs
	DSB	SY
	ISB	SY

	MRS	CurrentEL, R0
	LSR	$2, R0, R0
	AND	$0b11, R0, R0
	CMP	$3, R0
	BEQ	el3_tlb

	// EL2: invalidate all EL2 TLBs
	WORD	$0xd50c871f	// tlbi alle2
	B	tlb_done

el3_tlb:
	// EL3: invalidate all EL3 TLBs
	WORD	$0xd50e871f	// tlbi alle3

tlb_done:
	DSB	SY
	ISB	SY

	RET

// func write_mair_el3(val uint64)
TEXT ·write_mair_el3(SB),$0-8
	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
	// D12.2.65 MAIR_EL3, Memory Attribute Indirection Register (EL3)
	MOVD	val+0(FP), R0
	WORD	$0xd51ea200	// msr mair_el3, x0
	ISB	SY

	RET

// func write_tcr_el3(val uint64)
TEXT ·write_tcr_el3(SB),$0-8
	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
	// D12.2.105 TCR_EL3, Translation Control Register (EL3)
	MOVD	val+0(FP), R0
	WORD	$0xd51e2040	// msr tcr_el3, x0
	ISB	SY

	RET

// func set_ttbr0_el3(addr uint64)
TEXT ·set_ttbr0_el3(SB),$0-8
	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
	// D12.2.113 TTBR0_EL3, Translation Table Base Register 0 (EL3)
	MOVD	addr+0(FP), R0
	WORD	$0xd51e2000	// msr ttbr0_el3, x0
	ISB	SY

	CALL	·flush_tlb(SB)

	WORD	$0xd53e1000	// mrs x0, sctlr_el3
	BIC	$1<<19, R0	// clear WXN bit
	ORR	$1<<12, R0	// enable I-cache
	ORR	$1<<2, R0	// enable D-cache
	ORR	$1<<0, R0	// enable MMU
	WORD	$0xd51e1000	// msr sctlr_el3, x0
	ISB	SY

	RET

// func write_mair_el2(val uint64)
TEXT ·write_mair_el2(SB),$0-8
	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
	// D12.2.64 MAIR_EL2, Memory Attribute Indirection Register (EL2)
	MOVD	val+0(FP), R0
	WORD	$0xd51ca200	// msr mair_el2, x0
	ISB	SY

	RET

// func write_tcr_el2(val uint64)
TEXT ·write_tcr_el2(SB),$0-8
	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
	// D12.2.104 TCR_EL2, Translation Control Register (EL2)
	MOVD	val+0(FP), R0
	WORD	$0xd51c2040	// msr tcr_el2, x0
	ISB	SY

	RET

// func set_ttbr0_el2(addr uint64)
TEXT ·set_ttbr0_el2(SB),$0-8
	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture profile
	// D12.2.112 TTBR0_EL2, Translation Table Base Register 0 (EL2)
	MOVD	addr+0(FP), R0
	WORD	$0xd51c2000	// msr ttbr0_el2, x0
	ISB	SY

	CALL	·flush_tlb(SB)

	WORD	$0xd53c1000	// mrs x0, sctlr_el2
	BIC	$1<<19, R0	// clear WXN bit
	ORR	$1<<12, R0	// enable I-cache
	ORR	$1<<2, R0	// enable D-cache
	ORR	$1<<0, R0	// enable MMU
	WORD	$0xd51c1000	// msr sctlr_el2, x0
	ISB	SY

	RET
