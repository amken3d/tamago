// BCM2835 ARM interrupt controller driver
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package bcm2835

import (
	"github.com/usbarmory/tamago/internal/reg"
)

// ARM interrupt controller registers (offsets from the peripheral base).
// See BCM2835-ARM-Peripherals.pdf section 7. On the BCM2836/BCM2837 this
// legacy controller aggregates the peripheral (GPU) interrupt sources; its
// output is a single line routed to a CPU core by the QA7 local controller.
//
// There is no acknowledge / end-of-interrupt register: an interrupt is
// cleared by servicing the originating peripheral. A handler that returns
// without quieting its peripheral will be re-entered immediately.
const (
	IRQ_BASIC_PENDING = 0xb200
	IRQ_PENDING_1     = 0xb204
	IRQ_PENDING_2     = 0xb208
	FIQ_CONTROL       = 0xb20c
	ENABLE_IRQ_1      = 0xb210
	ENABLE_IRQ_2      = 0xb214
	ENABLE_BASIC_IRQ  = 0xb218
	DISABLE_IRQ_1     = 0xb21c
	DISABLE_IRQ_2     = 0xb220
	DISABLE_BASIC_IRQ = 0xb224
)

// Peripheral interrupt source numbers (subset). Sources 0..31 live in the
// register-1 group, sources 32..63 in the register-2 group.
const (
	// IRQ_AUX is the shared AUX interrupt (mini-UART, SPI1, SPI2).
	IRQ_AUX = 29
)

// InterruptController is the BCM2835 ARM (legacy) interrupt controller. The
// zero value is ready to use; register addresses are resolved through
// PeripheralAddress so the controller works regardless of the SoC peripheral
// base configured at Init0.
type InterruptController struct{}

// IC is the ARM interrupt controller instance.
var IC = &InterruptController{}

// enable/disable are write-1-to-act: only the bits written as 1 take effect,
// so no read-modify-write is needed.

func groupReg(irq int, r1, r2 uint32) (addr uint32, bit uint32) {
	if irq < 32 {
		return PeripheralAddress(r1), 1 << uint(irq)
	}
	return PeripheralAddress(r2), 1 << uint(irq-32)
}

// Enable unmasks the given peripheral interrupt source (0..63).
func (hw *InterruptController) Enable(irq int) {
	if irq < 0 || irq > 63 {
		return
	}

	addr, bit := groupReg(irq, ENABLE_IRQ_1, ENABLE_IRQ_2)
	reg.Write(addr, bit)
}

// Disable masks the given peripheral interrupt source (0..63).
func (hw *InterruptController) Disable(irq int) {
	if irq < 0 || irq > 63 {
		return
	}

	addr, bit := groupReg(irq, DISABLE_IRQ_1, DISABLE_IRQ_2)
	reg.Write(addr, bit)
}

// Pending1 returns the pending bitmap for sources 0..31.
func (hw *InterruptController) Pending1() uint32 {
	return reg.Read(PeripheralAddress(IRQ_PENDING_1))
}

// Pending2 returns the pending bitmap for sources 32..63.
func (hw *InterruptController) Pending2() uint32 {
	return reg.Read(PeripheralAddress(IRQ_PENDING_2))
}

// BasicPending returns the ARM-basic pending bitmap (timer, mailbox and the
// two pending-register summary bits).
func (hw *InterruptController) BasicPending() uint32 {
	return reg.Read(PeripheralAddress(IRQ_BASIC_PENDING))
}

// Pending reports whether the given source (0..63) is currently asserting.
func (hw *InterruptController) Pending(irq int) bool {
	if irq < 0 || irq > 63 {
		return false
	}

	if irq < 32 {
		return hw.Pending1()&(1<<uint(irq)) != 0
	}
	return hw.Pending2()&(1<<uint(irq-32)) != 0
}
