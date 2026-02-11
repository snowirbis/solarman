package solarman

import (
	"fmt"
)

func (inv *InverterLogger) error(point string, op string, err error) error {
	if err == nil {
		return fmt.Errorf("ERROR::%s [%d] %s", point, inv.LoggerSerialN, op)
	}
	return fmt.Errorf("ERROR::%s [%d] %s: %w", point, inv.LoggerSerialN, op, err)
}

func (inv *InverterLogger) debug(point string, op string, frame []byte, format ...int) {
	if !inv.DebugEnable {
		return
	}

	fmt.Printf("DEBUG::%s [%d] %s: ", point, inv.LoggerSerialN, op)

	asString := false
	if len(format) > 0 && format[0] == 1 {
		asString = true
	}

	if asString {
		fmt.Printf("%s\n", string(frame))
		return
	}

	for _, b := range frame {
		fmt.Printf("%02x ", b)
	}
	fmt.Println(" ")
}
