# Solarman
Solarman is a Go package for reading and writing Modbus registers over TCP for SolarMan-based inverters.

## Features
- Read group of registers
- Write to group of registers
- Easy Get/Set inverter internal clock using pre-defined functions
- Convert retrieved signed-values to float
- Extended bytestream debug
- Meta control to work with various invertors (set StartMarker EndMarker ReqControlCode ResControlCode)

## Basic usage

```go
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
    startRegister := 0x6d
    registerCount := 3

    // Read 3 registers starting from 0x6d
    data, err := deye.Read(startRegister, registerCount)
    if err != nil {
	fmt.Println("Error reading registers:", err)
	os.Exit(1)
    }

    // Returning addressed map [register]value
    fmt.Printf("Read registers from 0x%X: %v\n", startRegister, data)

    //Output: Read registers from 0x6D: map[109:385 110:0 111:392]

}
```

## Extended usage
See "examples"

## Acknowledgements  
This project was inspired by [xThaid/inverterlogger](https://github.com/xThaid/inverterlogger), which implemented Modbus register reading.  
This package extends functionality by adding write support, improved concurrency, debugging options and simple inverter clock management.

# Final Warning!

This code has the ability to write to the inverter's registers.  
**Please use the write methods with caution**, as improper use may lead to equipment failure!  

## Disclaimer

The author **is not responsible** for any damage caused by the use of this code, including but not limited to:  

- **Harm to your equipment** or third-party devices  
- **Damage to private or public property**  
- **Physical or psychological injuries** sustained by:  
  - You  
  - Your relatives & loved ones  
  - Pets, wild or domestic animals  
  - Politicians, corporate executives, employees  
  - Any other individuals  

**Use at your own risk!**
