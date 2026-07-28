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
	// debug tripwire: 'S' = image entered
	MOVD	$0x05000000, R7
	MOVD	$0x53, R8
	MOVW	R8, (R7)

	MRS	CurrentEL, R0
	LSR	$2, R0, R0
	AND	$0b11, R0, R0

	// already at EL1
	CMP	$1, R0
	BEQ	init

	// entered at EL2 (e.g. chain loaded from TF-A + U-Boot booti)
	CMP	$2, R0
	BEQ	el2

	// While tamago has been tested in Secure EL3, we drop to Non-secure
	// EL1 to ease chain loading from TF-A or bootloaders, as on AArch64
	// the OS is expected to run at this level.
	//
	// Future tamago unikernels for TF-A replacement or Secure monitors can
	// branch here to remain in EL3 provided that _EL1 register access is
	// replaced with _EL3.

	// ARM Architecture Reference Manual ARMv8, for ARMv8-A architecture
	// profile.

	// D12.2.99 SCR_EL3, Secure Configuration Register
	MOVD	$0, R0
	ORR	$1<<10, R0	// set lower levels as AArch64
	ORR	$1<<5, R0	// set reserved bit
	ORR	$1<<4, R0	// set reserved bit
	ORR	$1<<0, R0	// set Non-secure state
	WORD	$0xd51e1100	// msr scr_el3, x0

	// D12.2.44 HCR_EL2, Hypervisor Configuration Register
	MOVD	$1<<31, R0	// set EL1 level as AArch64
	WORD	$0xd51c1100	// msr HCR_EL2, x0

	// C5.2.19 SPSR_EL3, Saved Program Status Register (EL3)
	MOVD	$0, R0
	ORR	$0b1111<<6, R0	// mask exceptions/interrupts
	ORR	$0b0101<<0, R0	// set EL1h
	WORD	$0xd51e4000	// msr SPSR_EL3, x0

	// drop to EL1
	MOVD	$·cpuinit_el1(SB), R0
	WORD	$0xd51e4020	// msr ELR_EL3, x0
	ISB	SY
	ERET

el2:
	// debug tripwire: build a catcher vector table for EL2 at 0x40201000
	// (2 KB aligned DRAM above the BL31-resident first 2 MB, caches
	// still off). Each of the 16 vector slots
	// is a synthesized 4-instruction stub:
	//   movz x9, #slot ; movz x10, #lo16 ; movk x10, #hi16, lsl #16 ;
	//   br x10
	// jumping to the assembled el2Catch reporter, which dumps slot and
	// SPSR/ESR/ELR/FAR_EL2 on UART0 and parks.
	MOVD	$·el2Catch(SB), R8
	AND	$0xffff, R8, R9		// lo16
	LSR	$16, R8, R10
	AND	$0xffff, R10, R10	// hi16
	MOVD	$0xd280000a, R11	// movz x10, #lo16
	ORR	R9<<5, R11, R11
	MOVD	$0xf2a0000a, R12	// movk x10, #hi16, lsl #16
	ORR	R10<<5, R12, R12

	MOVD	$0x40201000, R4
	MOVD	R4, R6
	MOVD	$0, R5			// slot index
el2_vec_loop:
	MOVD	$0xd2800009, R7		// movz x9, #slot
	ORR	R5<<5, R7, R7
	MOVW	R7, (R6)
	MOVW	R11, 4(R6)
	MOVW	R12, 8(R6)
	MOVD	$0xd61f0140, R7		// br x10
	MOVW	R7, 12(R6)
	ADD	$0x80, R6
	ADD	$1, R5
	CMP	$16, R5
	BLT	el2_vec_loop
	WORD	$0xd51cc004	// msr VBAR_EL2, x4
	ISB	SY

	// D12.2.44 HCR_EL2, Hypervisor Configuration Register
	MOVD	$1<<31, R0	// set EL1 level as AArch64
	WORD	$0xd51c1100	// msr HCR_EL2, x0

	// D12.2.24 CNTHCTL_EL2, Counter-timer Hypervisor Control Register
	MOVD	$0b11, R0	// EL1PCEN | EL1PCTEN: EL1 timer/counter access
	WORD	$0xd51ce100	// msr CNTHCTL_EL2, x0

	// D12.2.34 CNTVOFF_EL2, Counter-timer Virtual Offset Register
	MOVD	$0, R0		// zero virtual counter offset
	WORD	$0xd51ce060	// msr CNTVOFF_EL2, x0

	// D12.2.31 CPTR_EL2, Architectural Feature Trap Register (EL2)
	MOVD	$0x33ff, R0	// RES1 bits, no FP/SIMD traps
	WORD	$0xd51c1140	// msr CPTR_EL2, x0

	// C5.2.18 SPSR_EL2, Saved Program Status Register (EL2)
	MOVD	$0, R0
	ORR	$0b1111<<6, R0	// mask exceptions/interrupts
	ORR	$0b0101<<0, R0	// set EL1h
	WORD	$0xd51c4000	// msr SPSR_EL2, x0

	// drop to EL1
	MOVD	$·cpuinit_el1(SB), R0
	WORD	$0xd51c4020	// msr ELR_EL2, x0
	ISB	SY
	ERET

init:
	B	·cpuinit_el1(SB)

TEXT ·cpuinit_el1(SB),NOSPLIT|NOFRAME,$0
	// debug tripwire: '1' = arrived at EL1
	MOVD	$0x05000000, R7
	MOVD	$0x31, R8
	MOVW	R8, (R7)

	// D12.2.100 SCTLR_EL1, System Control Register (EL1)
	MRS	SCTLR_EL1, R0
	BIC	$1<<1, R0	// clear A bit
	BIC	$1<<0, R0	// clear M bit
	MSR	R0, SCTLR_EL1
	ISB	SY

	// set stack pointer
	MOVD	runtime∕goos·RamStart(SB), R1
	MOVD	R1, RSP
	MOVD	runtime∕goos·RamSize(SB), R1
	MOVD	runtime∕goos·RamStackOffset(SB), R2
	ADD	R1, RSP
	SUB	R2, RSP

	// debug tripwire: 'R' = handing off to the Go runtime
	MOVD	$0x05000000, R7
	MOVD	$0x52, R8
	MOVW	R8, (R7)

	B	_rt0_tamago_start(SB)
