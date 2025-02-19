package main

import (
	"fmt"
	"os"
	"time"

	"github.com/snowirbis/solarman"
)

var (
	loggerAddress     = "192.168.1.18:8899"
	loggerSN          = uint32(2900000000)
	connectionTimeout = 5
)

// Before running, make sure you don't have
// any other connections to the logger on port 8899

func main() {

	deye := solarman.Init(loggerAddress, loggerSN, connectionTimeout)
	deye.SetDebug(true)

	// defaults defined in frame.go
	// type FrameMeta struct {
	//    	StartMarker    byte   // SolarMan V5 payload starting marker
	//    	EndMarker      byte   // SolarMan V5 payload ending marker
	//    	ReqControlCode uint16 // SolarMan V5 request control code
	//    	ResControlCode uint16 // SolarMan V5 response control code
	// }
	//
	// var DefaultMeta = FrameMeta{
	// 	StartMarker:    0xA5,
	// 	EndMarker:      0x15,
	// 	ReqControlCode: 0x4510,
	// 	ResControlCode: 0x1510,
	// }

	// If meta for your datalogger differs from default, set it here
	deye.SetMeta(0xA5, 0x15, 0x4510, 0x1510)

	// Inverter time for Solarman loggers with serial number starting with 29********
	// tested with Deye SUN-6K-SG03LP1-EU
	// for basic map of registers see "examples/registers"
	startRegister := 0x16

	// Read time from inverter
	cnt, start, timeSet, err := deye.SetDateTime(startRegister, time.Now())
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Printf("Inverter time set: %v, written %d bytes at start address %d\n", timeSet, cnt, start)

}
