// ARM64 processor support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build !linkcpuinit

#include "arm64.h"
#include "textflag.h"

TEXT cpuinit(SB),NOSPLIT|NOFRAME,$0
	// get current exception level
	MRS	CurrentEL, R0
	LSR	$2, R0, R0
	AND	$0b11, R0, R0

	CMP	$3, R0
	BEQ	el3_setup

	CMP	$2, R0
	BEQ	el2_setup

	B	exit

el3_setup:
	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture
	// profile.

	// D12.2.102 SCTLR_EL3, System Control Register (EL3)
	WORD	$0xd53e1000	// mrs x0, sctlr_el3
	BIC	$1<<1, R0	// clear A bit
	BIC	$1<<0, R0	// clear M bit
	WORD	$0xd51e1000	// msr sctlr_el3, x0
	ISB	SY

	// D12.2.99 SCR_EL3, Secure Configuration Register
	WORD	$0xd53e1100	// mrs x0, scr_el3
	ORR	$1<<3, R0	// set EA bit
	ORR	$1<<2, R0	// set FIQ bit
	ORR	$1<<1, R0	// set IRQ bit
	WORD	$0xd51e1100	// msr scr_el3, x0
	ISB	SY

	B	stack_setup

el2_setup:
	// D12.2.100 SCTLR_EL2, System Control Register (EL2)
	WORD	$0xd53c1000	// mrs x0, sctlr_el2
	BIC	$1<<1, R0	// clear A bit
	BIC	$1<<0, R0	// clear M bit
	WORD	$0xd51c1000	// msr sctlr_el2, x0
	ISB	SY

	// D12.2.47 HCR_EL2, Hypervisor Configuration Register
	WORD	$0xd53c1100	// mrs x0, hcr_el2
	ORR	$1<<3, R0	// set AMO - SError routed to EL2
	ORR	$1<<4, R0	// set IMO - IRQ routed to EL2
	ORR	$1<<5, R0	// set FMO - FIQ routed to EL2
	WORD	$0xd51c1100	// msr hcr_el2, x0
	ISB	SY

	// D12.8.11 CNTHCTL_EL2, Counter-timer Hypervisor Control register
	WORD	$0xd53ce100	// mrs x0, cnthctl_el2
	ORR	$0x3, R0	// set EL1PCTEN and EL1PCEN
	WORD	$0xd51ce100	// msr cnthctl_el2, x0
	ISB	SY

	B	stack_setup

stack_setup:
	// set stack pointer
	MOVD	runtime∕goos·RamStart(SB), R1
	MOVD	R1, RSP
	MOVD	runtime∕goos·RamSize(SB), R1
	MOVD	runtime∕goos·RamStackOffset(SB), R2
	ADD	R1, RSP
	SUB	R2, RSP

	B	_rt0_tamago_start(SB)

exit:
	JMP	·exit(SB)
