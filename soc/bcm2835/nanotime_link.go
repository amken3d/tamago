// BCM2835 SoC support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the bcm2835 package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

//go:build linknanotime

package bcm2835

// With the `linknanotime` tag the board provides runtime/goos.Nanotime and
// owns the ARM processor instance timer multiplier (see nanotime.go).
func setTimerMultiplier() {}
