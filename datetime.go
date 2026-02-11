package solarman

import (
	"fmt"
	"time"
)

// conversions between a []byte slice and localtime
// for SolarMan V5:
// - register 22: high byte – year (offset from 2000), low byte – month;
// - register 23: high byte – day, low byte – hour;
// - register 24: high byte – minutes, low byte – seconds.

/* private methods */

func (inv *InverterLogger) localTimeToBytes(timeDate time.Time) []int {

	year := timeDate.Year()
	month := int(timeDate.Month())
	day := timeDate.Day()
	hour := timeDate.Hour()
	minute := timeDate.Minute()
	second := timeDate.Second()

	yearOffset := year - 2000
	yearMonth := (yearOffset << 8) | month
	dayHour := (day << 8) | hour
	minuteSecond := (minute << 8) | second

	return []int{yearMonth, dayHour, minuteSecond}
}

func (inv *InverterLogger) bytesToLocalTime(regs []int) (time.Time, error) {
	if len(regs) != 3 {
		return time.Time{}, inv.error("BytesToLocalTime", fmt.Sprintf("expected slice of 3 elements, got %d", len(regs)), nil)
	}

	yearOffset := regs[0] >> 8
	month := time.Month(regs[0] & 0xFF)

	day := int(regs[1] >> 8)
	hour := int(regs[1] & 0xFF)

	minute := int(regs[2] >> 8)
	second := int(regs[2] & 0xFF)

	year := 2000 + int(yearOffset)

	// Fill struct time.Time with our values
	t := time.Date(year, month, day, hour, minute, second, 0, time.Local)

	return t, nil
}

/* Public methods */

func (inv *InverterLogger) GetDateTime(startRegister int) (time.Time, error) {

	registers, err := inv.Read(startRegister, 3)

	if len(registers) < 3 {
		return time.Time{}, fmt.Errorf("GetDateTime: expected 3 registers, got %d", len(registers))
	}

	if err != nil {
		return time.Time{}, err
	}

	yymm := registers[startRegister]
	hhmm := registers[startRegister+1]
	mmss := registers[startRegister+2]

	invDateTime, err := inv.bytesToLocalTime([]int{int(yymm), int(hhmm), int(mmss)})

	if err != nil {
		return time.Time{}, err
	}

	return invDateTime, nil

}

func (inv *InverterLogger) SetDateTime(startRegister int, setTime time.Time) (int, int, time.Time, error) {

	inverterTime := inv.localTimeToBytes(setTime)

	cnt, start, err := inv.Write(startRegister, inverterTime)

	if err != nil {
		return 0, 0, time.Time{}, err
	}

	// each register contains 2 bytes
	if cnt != len(inverterTime)*2 {
		return 0, 0, time.Time{}, fmt.Errorf("SetDateTime: expected to write %d registers, wrote %d", len(inverterTime)*2, cnt)
	}

	timeSet, err := inv.bytesToLocalTime(inverterTime)
	if err != nil {
		return 0, 0, time.Time{}, err
	}

	return cnt, start, timeSet, nil
}
