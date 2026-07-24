// Qualcomm QCM2290 interrupt routing
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package qcm2290

import (
	"github.com/usbarmory/tamago/internal/reg"
)

// ARM generic timer private peripheral interrupts, GIC INTID = 16 + the
// device tree PPI index (agatti.dtsi timer node, in interrupts order:
// secure-physical 1, non-secure-physical 2, virtual 3, hypervisor 0).
//
// The virtual timer is the one delivered to non-secure EL1 under the
// Qualcomm hypervisor (the physical timer PPI never signals; the stock
// kernel selects the virtual timer for the same reason).
const (
	TIMER_EL1_PHYS_IRQ = 16 + 2
	TIMER_EL1_VIRT_IRQ = 16 + 3
)

// defined in gic.s
func write_icc_igrpen1_el1(val uint64)
func write_icc_sre_el1(val uint64)
func write_icc_pmr_el1(val uint64)

// GICv3 register offsets used by the minimal non-secure init
const (
	gicdCTLR      = 0x000
	gicrWAKER     = 0x014
	gicrIGROUPR0  = 0x10000 + 0x080 // SGI_BASE frame, PPI/SGI groups
	gicrISENABLER = 0x10000 + 0x100 // SGI_BASE frame, PPI/SGI enables
	gicrISPENDR0  = 0x10000 + 0x200 // SGI_BASE frame, PPI/SGI pending
)

// InitGIC initializes the GICv3 for wake-up capable timer interrupts.
//
// The OS runs at non-secure EL1 under the Qualcomm hypervisor with
// GICD_CTLR.DS=0: non-secure interrupts are Group 1 and Group 0 state
// is not writable in the non-secure register views. This is a minimal
// Group 1 configuration so a pending timer PPI reaches the core as a
// WFI wake-up event (IRQs stay masked at the PE; no exception is
// taken). The generic gic package Init() is not used: its full
// distributor sweep and Group 0 CPU-interface writes reset the platform
// from non-secure EL1 (hypervisor/XPU intervention).
func InitGIC() {
	// Mark the CPU as online (reads back awake on this platform)
	reg.Clear(GICR_BASE+gicrWAKER, 1)

	// System register interface, unmasked priorities, Group 1 signaling
	write_icc_sre_el1(1)
	write_icc_pmr_el1(0xff)
	write_icc_igrpen1_el1(1)

	// Affinity routing for the non-secure state: without it the
	// redistributor SGI/PPI frame is not in effect (bit 4 = ARE_NS in
	// the non-secure GICD_CTLR view; groups must still be disabled at
	// this point)
	reg.Set(GICD_BASE+gicdCTLR, 4)

	// Distributor Group 1 forwarding (non-secure view of GICD_CTLR:
	// bit 0 EnableGrp1, bit 1 EnableGrp1A under affinity routing)
	reg.Set(GICD_BASE+gicdCTLR, 0)
	reg.Set(GICD_BASE+gicdCTLR, 1)

	// Timer PPIs: the virtual timer is the deliverable one; the
	// physical PPI is enabled for completeness
	reg.SetTo(GICR_BASE+gicrISENABLER, TIMER_EL1_PHYS_IRQ, true)
	reg.SetTo(GICR_BASE+gicrISENABLER, TIMER_EL1_VIRT_IRQ, true)
}

// GICState returns the non-secure views of GICD_CTLR and the
// redistributor PPI group/enable/pending registers, for bring-up
// diagnostics.
func GICState() (ctlr, igroupr0, isenabler0, ispendr0 uint32) {
	return reg.Read(GICD_BASE + gicdCTLR),
		reg.Read(GICR_BASE + gicrIGROUPR0),
		reg.Read(GICR_BASE + gicrISENABLER),
		reg.Read(GICR_BASE + gicrISPENDR0)
}
