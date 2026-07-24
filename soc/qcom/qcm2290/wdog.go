// Qualcomm APSS watchdog driver
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

// Watchdog registers, qcom,kpss-wdt layout
// (drivers/watchdog/qcom-wdt.c)
const (
	WDT_RST       = 0x04
	WDT_EN        = 0x08
	WDT_STS       = 0x0c
	WDT_BARK_TIME = 0x10
	WDT_BITE_TIME = 0x14
)

// Watchdog represents an APSS watchdog instance.
type Watchdog struct {
	Base uint32
}

// Disable stops the watchdog.
func (w *Watchdog) Disable() {
	reg.Write(w.Base+WDT_EN, 0)
	reg.Write(w.Base+WDT_RST, 1)
}

// Service pets the watchdog.
func (w *Watchdog) Service() {
	reg.Write(w.Base+WDT_RST, 1)
}
