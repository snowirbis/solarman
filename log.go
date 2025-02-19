package solarman

import (
	"fmt"
)

func Err(inv *InverterLogger, point string, op string, err error) error {
	if err == nil {
		return fmt.Errorf("ERROR::%s [%d] %s", point, inv.LoggerSerialN, op)
	}
	return fmt.Errorf("ERROR::%s [%d] %s: %w", point, inv.LoggerSerialN, op, err)
}

func Debug(inv *InverterLogger, point string, op string, frame []byte) {
	if inv.DebugEnable == true {
		fmt.Printf("DEBUG::%s [%d] %s: ", point, inv.LoggerSerialN, op)
		for _, b := range frame {
			fmt.Printf("%02x ", b)
		}
		fmt.Println(" ")
	}
}
