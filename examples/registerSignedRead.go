package main

import (
	"fmt"
	"os"

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

	// solar strings current voltage for
	// tested with Deye SUN-6K-SG03LP1-EU
	// for basic map of registers see "examples/registers"
	startRegister := 0xBF
	registerCount := 1

	// Read 3 registers starting from 0x16
	// Returning addressed map [register]value
	data, err := deye.Read(startRegister, registerCount)
	if err != nil {
		fmt.Println("Error reading registers:", err)
		os.Exit(1)
	}

	// some registers contains signed values
	// and that's how to parse it

	batteryCurrent := deye.SignedToFloat(data[startRegister]) * 0.01

	fmt.Printf("Read registers from 0x%X: %v\n", startRegister, data)
	//Output: Read registers from 0x6D: map[191:961]

	// A positive value indicates the battery discharge current (A)
	// A negative value indicates the battery charge current (A)
	fmt.Printf("Battery current: %.2f A\n", batteryCurrent)

	//Output: Battery current: 9.61 A]
}
