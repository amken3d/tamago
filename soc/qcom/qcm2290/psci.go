// Qualcomm QCM2290 PSCI support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the qcm2290 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package qcm2290

// PSCI (Power State Coordination Interface) function IDs, SMC64 calling
// convention (ARM DEN 0022, firmware advertises PSCIv1.0).
const (
	PSCI_VERSION      = 0x84000000
	PSCI_SYSTEM_OFF   = 0x84000008
	PSCI_SYSTEM_RESET = 0x84000009
	PSCI_CPU_ON       = 0xc4000003
)

// defined in psci.s
func smc(fn, a0, a1, a2 uint64) uint64

// PSCIVersion returns the firmware PSCI version (major<<16 | minor).
func PSCIVersion() uint64 {
	return smc(PSCI_VERSION, 0, 0, 0)
}

// Reset requests a warm system reset through the secure firmware; on
// success it does not return.
func Reset() {
	smc(PSCI_SYSTEM_RESET, 0, 0, 0)
}

// CPUOn releases a secondary core: mpidr selects the target (affinity
// value), entry is the physical address it starts executing at (EL1,
// MMU off), ctx is handed to the entry point in x0. Returns the PSCI
// status (0 = success).
func CPUOn(mpidr, entry, ctx uint64) int64 {
	return int64(smc(PSCI_CPU_ON, mpidr, entry, ctx))
}
