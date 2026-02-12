// Raspberry Pi Zero 2W LED support
// https://github.com/usbarmory/tamago
//
// Copyright (c) the pizero2w package authors
//
// Use of this source code is governed by the license
// that can be found in the LICENSE file.

package pizero2w

import (
	"errors"

	"github.com/usbarmory/tamago/soc/bcm2835"
)

// LED GPIO lines
const (
	// Activity LED (active-low on Pi Zero 2W)
	ACTIVITY = 29
)

var activity *bcm2835.GPIO

func init() {
	var err error

	activity, err = bcm2835.NewGPIO(ACTIVITY)

	if err != nil {
		panic(err)
	}

	activity.Out()
}

// LED turns on/off an LED by name.
func (b *board) LED(name string, on bool) (err error) {
	switch name {
	case "activity", "Activity", "ACTIVITY":
		// Activity LED is active-low on the Pi Zero 2W
		if on {
			activity.Low()
		} else {
			activity.High()
		}
	default:
		return errors.New("invalid LED")
	}

	return
}
