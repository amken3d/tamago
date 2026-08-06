// ARM processor support
// https://github.com/usbarmory/tamago
//
// Copyright (c) The TamaGo Authors. All Rights Reserved.
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package arm

import (
	"runtime/goos"
	"unsafe"

	"github.com/usbarmory/tamago/internal/reg"
)

// ARM exception vector offsets
// (Table 11-1, ARM® Cortex™ -A Series Programmer’s Guide).
const (
	RESET          = 0x00
	UNDEFINED      = 0x04
	SUPERVISOR     = 0x08
	PREFETCH_ABORT = 0x0c
	DATA_ABORT     = 0x10
	IRQ            = 0x18
	FIQ            = 0x1c
)

// linknamed by application as needed
var vecTableStart uint32

// SetVectorTableStart overrides the location of the 64 kB reserved area
// (vector table, L1/L2 page tables, exception stack), which otherwise
// defaults to RamStart. It must be called before Init/InitEarly and the
// address must be 16 kB aligned (L1 page table alignment: the L1 table is
// placed at this address + 0x4000).
func SetVectorTableStart(addr uint32) {
	vecTableStart = addr
}

// excStack is the exception stack address set by initVectorTable,
// read by exception_v5.s which cannot use the ARMv6+ banked SP MRS.
var excStack uint32

const (
	vecTableJump   = 0xe59ff018 // ldr pc, [pc, #24]
	excStackOffset = 0x8000     // 32 kB
	excStackSize   = 0x4000     // 16 kB
)

// defined in exception.s or exception_v5.s (see build constraints)
func set_exc_stack(addr uint32)
func set_exc_stack_ns(addr uint32)
func set_vbar(addr uint32)
func set_mvbar(addr uint32)
func resetHandler()
func undefinedHandler()
func supervisorHandler()
func prefetchAbortHandler()
func dataAbortHandler()
func irqHandler()
func fiqHandler()
func nullHandler()

type ExceptionHandler func()

func vector(fn ExceptionHandler) uint32 {
	return **((**uint32)(unsafe.Pointer(&fn)))
}

type VectorTable struct {
	Reset         ExceptionHandler
	Undefined     ExceptionHandler
	Supervisor    ExceptionHandler
	PrefetchAbort ExceptionHandler
	DataAbort     ExceptionHandler
	IRQ           ExceptionHandler
	FIQ           ExceptionHandler
}

// DefaultExceptionHandler handles an exception by printing its vector and
// processor mode before panicking.
//
// For aborts it also reports the fault status and faulting address (DFSR/DFAR
// for a data abort, IFSR/IFAR for a prefetch abort). Without those, an abort
// report says only that memory access failed somewhere -- with them it names the
// address, which is the difference between diagnosing a stray write and guessing
// at it. The status register's encoding is in the ARM ARM (B3.13.3): the low
// bits give the fault type (0b00101 translation, 0b01101 permission, ...) and
// bit 11 (WnR) distinguishes a write from a read.
func DefaultExceptionHandler(off int) {
	print("exception: vector ", off, " mode ", int(read_cpsr()&0x1f), "\n")

	// Printed in decimal on purpose: the runtime's print formats integers
	// without allocating, and allocating inside a fault handler -- on the system
	// stack, with a heap that may be exactly what is corrupted -- risks faulting
	// again and losing the report entirely.
	switch off {
	case DATA_ABORT:
		dfsr := read_dfsr()
		print("data abort: DFAR(dec) ", int64(read_dfar()),
			" DFSR ", int64(dfsr), " status ", int64(dfsr&0x40f), " ", wnr(dfsr), "\n")
	case PREFETCH_ABORT:
		print("prefetch abort: IFAR(dec) ", int64(read_ifar()), " IFSR ", int64(read_ifsr()), "\n")
	}

	panic("unhandled exception")
}

// wnr decodes DFSR bit 11: whether the aborted access was a write or a read.
func wnr(dfsr uint32) string {
	if dfsr&(1<<11) != 0 {
		return "write"
	}

	return "read"
}

// SystemExceptionHandler allows to override the default exception handler
// executed at any exception by the table returned by SystemVectorTable(),
// which is used by default when initializing the CPU instance (e.g.
// CPU.Init()).
var SystemExceptionHandler = DefaultExceptionHandler

// Per-core exception breadcrumbs, written BEFORE any handler runs: an
// exception on a secondary core can spiral or wedge before its report
// reaches the console (printing takes locks and a buffered console may
// never flush), so the bare facts -- count, vector, fault address, mode --
// go to fixed scratch words a diagnostic on another core can read.
// 4 words per core at excScratch + 16*core: count, vector, addr, cpsr.
const excScratch = 0xb180

func read_mpidr() uint32

func systemException(off int) {
	core := uintptr(read_mpidr() & 3)
	b := excScratch + 16*core
	*(*uint32)(unsafe.Pointer(b)) += 1
	*(*uint32)(unsafe.Pointer(b + 4)) = uint32(off)

	var addr uint32
	switch off {
	case DATA_ABORT:
		addr = read_dfar()
	case PREFETCH_ABORT:
		addr = read_ifar()
	}
	*(*uint32)(unsafe.Pointer(b + 8)) = addr
	*(*uint32)(unsafe.Pointer(b + 12)) = read_cpsr()

	SystemExceptionHandler(off)
}

// SystemVectorTable returns a vector table that, for all exceptions, switches
// to system mode and calls the SystemExceptionHandler on the Go runtime stack
// within goroutine g0.
func SystemVectorTable() VectorTable {
	return VectorTable{
		Reset:         resetHandler,
		Undefined:     undefinedHandler,
		Supervisor:    supervisorHandler,
		PrefetchAbort: prefetchAbortHandler,
		DataAbort:     dataAbortHandler,
		IRQ:           irqHandler,
		FIQ:           fiqHandler,
	}
}

// VectorName returns the exception vector offset name.
func VectorName(off int) string {
	switch off {
	case RESET:
		return "RESET"
	case UNDEFINED:
		return "UNDEFINED"
	case SUPERVISOR:
		return "SUPERVISOR"
	case PREFETCH_ABORT:
		return "PREFETCH_ABORT"
	case DATA_ABORT:
		return "DATA_ABORT"
	case IRQ:
		return "IRQ"
	case FIQ:
		return "FIQ"
	}

	return "Unknown"
}

// SetVectorTable updates the CPU exception handling vector table with the
// addresses of the functions defined in the passed structure.
func (cpu *CPU) SetVectorTable(t VectorTable) {
	vecTable := cpu.vbar + 8*4

	// set handler pointers
	// Table 11-1 ARM® Cortex™ -A Series Programmer’s Guide

	reg.Write(vecTable+RESET, vector(t.Reset))
	reg.Write(vecTable+UNDEFINED, vector(t.Undefined))
	reg.Write(vecTable+SUPERVISOR, vector(t.Supervisor))
	reg.Write(vecTable+PREFETCH_ABORT, vector(t.PrefetchAbort))
	reg.Write(vecTable+DATA_ABORT, vector(t.DataAbort))
	reg.Write(vecTable+IRQ, vector(t.IRQ))
	reg.Write(vecTable+FIQ, vector(t.FIQ))
}

//go:nosplit
func (cpu *CPU) initVectorTable() {
	// 32-bytes alignment is required
	cpu.vbar = uint32(goos.RamStart)

	// the application is allowed to override the reserved area
	if vecTableStart != 0 {
		cpu.vbar = vecTableStart
	}

	// initialize jump table
	// Table 11-1 ARM® Cortex™ -A Series Programmer’s Guide
	for i := uint32(0); i < 8; i++ {
		reg.Write(cpu.vbar+4*i, vecTableJump)
	}

	// set exception handlers
	cpu.SetVectorTable(SystemVectorTable())

	// Do not set VBAR on cores with fixed exception vectors (e.g. ARMv5)
	// which have no VBAR/MVBAR registers.
	if cpu.vbar != 0 {
		set_vbar(cpu.vbar)

		if cpu.Secure() {
			// set monitor vector base address register
			set_mvbar(cpu.vbar)
		}
	}

	// Set the stack pointer for exception modes to provide a stack when
	// summoned by exception vectors.
	excStack = cpu.vbar + excStackOffset + excStackSize

	if forceNonSecure {
		// Monitor mode cannot be entered from the non-secure world
		// (see set_exc_stack_ns).
		set_exc_stack_ns(excStack)
	} else {
		set_exc_stack(excStack)
	}
}
