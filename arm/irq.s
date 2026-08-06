// ARM processor support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build arm.6

#include "go_asm.h"
#include "textflag.h"

// func irq_enable(spsr bool)
TEXT ·irq_enable(SB),$0-1
	MOVB	spsr+0(FP), R0
	CMP	$1, R0
	B.EQ	spsr

	WORD	$0xf1080080 // cpsie i
	RET
spsr:
	WORD	$0xe14f0000 // mrs r0, SPSR
	BIC	$1<<7, R0   // unmask IRQs
	WORD	$0xe169f000 // msr SPSR, r0
	RET

// func irq_disable(spsr bool)
TEXT ·irq_disable(SB),$0-1
	MOVB	spsr+0(FP), R0
	CMP	$1, R0
	B.EQ	spsr

	WORD	$0xf10c0080 // cpsid i
	RET
spsr:
	WORD	$0xe14f0000 // mrs r0, SPSR
	ORR	$1<<7, R0   // mask IRQs
	WORD	$0xe169f000 // msr SPSR, r0
	RET

// func fiq_enable(spsr bool)
TEXT ·fiq_enable(SB),$0-1
	MOVB	spsr+0(FP), R0
	CMP	$1, R0
	B.EQ	spsr

	WORD	$0xf1080040 // cpsie f
	RET
spsr:
	WORD	$0xe14f0000 // mrs r0, SPSR
	BIC	$1<<6, R0   // unmask FIQs
	WORD	$0xe169f000 // msr SPSR, r0
	RET

// func fiq_disable(spsr bool)
TEXT ·fiq_disable(SB),$0-1
	MOVB	spsr+0(FP), R0
	CMP	$1, R0
	B.EQ	spsr

	WORD	$0xf10c0040 // cpsid f
	RET
spsr:
	WORD	$0xe14f0000 // mrs r0, SPSR
	ORR	$1<<6, R0   // mask FIQs
	WORD	$0xe169f000 // msr SPSR, r0
	RET

// func wfi()
TEXT ·wfi(SB),$0
	// wait until an interrupt is received in low-power state
	WORD	$0xe320f003 // wfi
	RET

// The IRQ handler serves two different worlds, split by MPIDR (which makes
// this bcm2836/v7-MP specific):
//
// Core 0 keeps the original model: relay to the service goroutine, return
// with IRQs re-masked in SPSR, and let that goroutine unmask when every
// source is quiet. Preemption of core 0 is the cooperative poison
// (runtime.tamagoPreempt), driven by the platform's periodic tick.
//
// A secondary core has no service goroutine, and returning masked would
// make the first interrupt also the last. Its only two sources are acked
// inline -- the CNTP alarm (disarmed; SetAlarm re-arms) and its QA7
// mailbox-0 doorbell, which doubles as the preemption IPI (see
// runtime.preemptM) -- and it returns with the interrupted SPSR intact.
// Besides the poison, a secondary builds a full trap frame
// [sp, lr, spsr, r0-r12, pc] and offers it to the runtime: if a preemption
// request is pending and the interrupted PC is an async-safe point,
// runtime.tamagoSigPreempt rewrites sp/lr/pc so the return below enters
// runtime.asyncPreempt -- preemption that reaches even call-free loops.
TEXT ·irqHandler(SB),NOSPLIT|NOFRAME,$0
	// remove exception specific LR offset
	SUB	$4, R14, R14

	// save caller registers (the pushed r14 doubles as the frame's pc)
	MOVM.DB.W	[R0-R12, R14], (R13)	// push {r0-r12, r14}

	MRC	15, 0, R0, C0, C0, 5	// MPIDR
	AND	$3, R0, R0
	CMP	$0, R0
	B.EQ	core0

	// --- secondary core: ack the only enabled sources inline ---
	// QA7: pending at 0x40000060+4*core, mailbox 0 clear at 0x400000c0+0x10*core
	MOVW	$0x40000060, R1
	ADD	R0<<2, R1, R1
	MOVW	(R1), R2

	TST	$0x2, R2		// CNTPNSIRQ: the core's alarm
	B.EQ	mbox
	MOVW	$0, R3
	MCR	15, 0, R3, C14, C2, 1	// CNTP_CTL = 0: disarm

mbox:
	TST	$0x10, R2		// mailbox 0: the wake/preempt doorbell
	B.EQ	poison
	MOVW	$0x400000c0, R3
	ADD	R0<<4, R3, R3
	MVN	$0, R4
	MOVW	R4, (R3)

poison:
	// per-core IRQ-entry counter at 0xb1c0+4*core: from another core this
	// distinguishes an interrupt storm (huge) from a core that stopped
	// taking interrupts at all (frozen) -- the two silent failure modes
	// bring-up keeps meeting. R0 still holds the core number here.
	MOVW	$0xb1c0, R1
	ADD	R0<<2, R1, R1
	MOVW	(R1), R3
	ADD	$1, R3, R3
	MOVW	R3, (R1)

	// cooperative tier: folds into the next stack-growth check
	CALL	runtime·tamagoPreempt(SB)

	// async tier: ask first, build after. The check is cheap and almost
	// always answers "leave the frame alone" (no pending request, tier
	// not armed, wrong moment) -- the ordinary interrupt pays no frame
	// capture at all, and consuming an unservable request there keeps
	// preemptM's 0->1 delivery edge live.
	SUB	$12, R13, R13
	CALL	runtime·tamagoPreemptCheck(SB)
	MOVW	4(R13), R1		// signal g (0: plain return)
	MOVW	8(R13), R2		// its stack top
	ADD	$12, R13, R13
	CMP	$0, R1
	B.EQ	secpop

	// accepted: complete the trap frame -- banked SP/LR and SPSR below
	// the pushed registers -- and run tamagoSigPreempt(frame, gp) on the
	// signal stack, the same move a Unix signal makes. Nothing survives
	// a Go call in registers, so the IRQ stack pointer and the
	// interrupted g ride in the callee frame.
	WORD	$0xe14f0000		// mrs r0, SPSR
	MOVW.W	R0, -4(R13)		// push spsr
	SUB	$8, R13, R13
	WORD	$0xe8cd6000		// stm sp, {sp, lr}^ (banked SP/LR)

	MOVW	R13, R4			// frame
	MOVW	g, R5			// interrupted g
	MOVW	R13, R6			// IRQ stack
	MOVW	R1, g			// g = gsignal
	BIC	$7, R2
	MOVW	R2, R13
	SUB	$20, R13, R13
	MOVW	R4, 4(R13)		// arg: frame
	MOVW	R5, 8(R13)		// arg: gp
	MOVW	R6, 12(R13)		// saved IRQ stack
	MOVW	R5, 16(R13)		// saved g
	CALL	runtime·tamagoSigPreempt(SB)
	MOVW	16(R13), g
	MOVW	12(R13), R13

	// honor the (possibly rewritten) frame: banked SP/LR, the original
	// SPSR -- no re-masking, the next IPI must land -- then fall through
	// to the register pop, the frame's pc arriving in R14
	WORD	$0xe8dd6000		// ldm sp, {sp, lr}^ (banked SP/LR)
	ADD	$8, R13, R13
	MOVW.P	4(R13), R0		// pop spsr
	WORD	$0xe169f000		// msr SPSR, r0

secpop:
	MOVM.IA.W	(R13), [R0-R12, R14]	// pop {r0-r12, r14}
	MOVW.S	R14, R15

core0:
	// request cooperative preemption of the interrupted goroutine (g register
	// R10 still holds it here); folds into its next stack-growth check.
	CALL	runtime·tamagoPreempt(SB)

	// consume any pending preemptM request: this path never runs the
	// async tier, and a flag left set would silence future doorbells
	CALL	runtime·tamagoPreemptAck(SB)

	// let the platform ack self-contained sources (tick, doorbell) in
	// place: if nothing is left pending, return with the mask untouched.
	// The relay-and-mask model deadlocks a stopped world -- the service
	// goroutine cannot run to unmask, the tick dies, and the time-slice
	// fan-out the stop needs dies with it.
	SUB	$8, R13, R13
	CALL	runtime·tamagoIRQAck(SB)
	MOVBU	4(R13), R0		// bool: ONE byte -- a word load reads
					// stack garbage above it (and did: the
					// first boot tick "handled" itself into
					// an interrupt storm)
	ADD	$8, R13, R13
	CMP	$0, R0
	B.NE	c0pop

	SUB	$8, R13, R13
	MOVW	$(const_IRQ_SIGNAL), R0
	MOVW	R0, 4(R13)
	CALL	os∕signal·Relay(SB)
	ADD	$8, R13, R13

	// the IRQ handling goroutine is expected to unmask IRQs
	WORD	$0xe14f0000			// mrs r0, SPSR
	ORR	$1<<7, R0			// mask IRQs
	WORD	$0xe169f000			// msr SPSR, r0

c0pop:
	// restore caller registers
	MOVM.IA.W	(R13), [R0-R12, R14]	// pop {r0-r12, r14}

	// restore PC from LR and mode
	MOVW.S	R14, R15

// func read_mpidr() uint32
TEXT ·read_mpidr(SB),$0-4
	MRC	15, 0, R0, C0, C0, 5	// MPIDR
	MOVW	R0, ret+0(FP)

	RET
