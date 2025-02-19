package solarman

import (
	"sync/atomic"
)

func (inv *InverterLogger) GetNextSequenceNumber() uint16 {
	// Atomic increment for ModBus frame identification
	newSeq := atomic.AddUint32(&inv.SequenceNumber, 1)
	if newSeq == 0 {
		// If the new number is 0 (may be due to overflow),
		// force it to 1
		newSeq = 1
		atomic.StoreUint32(&inv.SequenceNumber, 1)
	}
	return uint16(newSeq)
}
