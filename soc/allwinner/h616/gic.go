// Allwinner H616 interrupt routing (GIC-400, GICv2)
// https://github.com/usbarmory/tamago
//
// Copyright (c) the h616 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package h616

import (
	"github.com/usbarmory/tamago/internal/reg"
)

// ARM generic timer private peripheral interrupts, GIC INTID = 16 + the
// device tree PPI index (sun50i-h616.dtsi timer node: secure-physical
// 13, non-secure-physical 14, virtual 11, hypervisor 10).
//
// The non-secure physical timer is the deliverable one at non-secure
// EL1: TF-A BL31 grants EL1 physical timer access and the EL2 entry
// path sets CNTHCTL_EL2.EL1PCEN/EL1PCTEN.
const (
	TIMER_EL1_PHYS_IRQ = 16 + 14 // INTID 30
	TIMER_EL1_VIRT_IRQ = 16 + 11 // INTID 27
)

// GICv2 register offsets used by the minimal non-secure init
// (ARM IHI 0048B.b).
const (
	gicdCTLR      = 0x000
	gicdTYPER     = 0x004
	gicdISENABLER = 0x100
	gicdISPENDR   = 0x200
	gicdISACTIVER = 0x300

	giccCTLR  = 0x000
	giccPMR   = 0x004
	giccRPR   = 0x014
	giccHPPIR = 0x018
)

// InitGIC initializes the GIC-400 (GICv2) for wake-up capable timer
// interrupts.
//
// The OS runs at non-secure EL1 under TF-A BL31 with the GIC security
// extensions in use: Group 0 state and grouping registers are owned by
// the secure world and are not writable from the non-secure register
// views. This is a minimal Group 1 configuration so a pending timer PPI
// reaches the core as a WFI wake-up event (IRQs stay masked at the PE;
// no exception is taken). The generic gic package Init() is not used:
// its full distributor sweep and Group 0 writes belong to the secure
// world (the same sweep resets the platform from non-secure EL1 on the
// QCM2290).
func InitGIC() {
	// Unmask all non-secure priorities at the CPU interface. The
	// non-secure view of GICC_PMR is shifted: this permits the lower
	// (non-secure) half of the priority range.
	reg.Write(GICC_BASE+giccPMR, 0xff)

	// CPU interface Group 1 forwarding (non-secure GICC_CTLR view:
	// bit 0 aliases EnableGrp1)
	reg.Set(GICC_BASE+giccCTLR, 0)

	// Distributor Group 1 forwarding (non-secure GICD_CTLR view:
	// bit 0 aliases EnableGrp1)
	reg.Set(GICD_BASE+gicdCTLR, 0)

	// Timer PPIs: the non-secure physical timer is the deliverable
	// one; the virtual timer is enabled for completeness. Non-secure
	// writes to enable bits of Group 0 interrupts are ignored.
	reg.SetTo(GICD_BASE+gicdISENABLER, TIMER_EL1_PHYS_IRQ, true)
	reg.SetTo(GICD_BASE+gicdISENABLER, TIMER_EL1_VIRT_IRQ, true)
}

// GICState returns the non-secure views of the distributor and CPU
// interface registers relevant to bring-up diagnostics: GICD_CTLR,
// GICD_ISENABLER0, GICD_ISPENDR0, GICD_ISACTIVER0, GICC_CTLR, GICC_PMR,
// GICC_RPR and GICC_HPPIR (highest priority pending, side-effect free).
func GICState() (dctlr, isenabler0, ispendr0, isactiver0, cctlr, pmr, rpr, hppir uint32) {
	dctlr = reg.Read(GICD_BASE + gicdCTLR)
	isenabler0 = reg.Read(GICD_BASE + gicdISENABLER)
	ispendr0 = reg.Read(GICD_BASE + gicdISPENDR)
	isactiver0 = reg.Read(GICD_BASE + gicdISACTIVER)
	cctlr = reg.Read(GICC_BASE + giccCTLR)
	pmr = reg.Read(GICC_BASE + giccPMR)
	rpr = reg.Read(GICC_BASE + giccRPR)
	hppir = reg.Read(GICC_BASE + giccHPPIR)
	return
}
