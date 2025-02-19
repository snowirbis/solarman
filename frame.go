package solarman

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// -----------------------------------------------------------------------------
// Frame for data exchange with the device
// -----------------------------------------------------------------------------

type Frame struct {
	PayloadLength  uint16
	ReqControlCode uint16
	ResControlCode uint16
	SerialNumber   uint16
	DeviceSN       uint32
	Payload        []byte
}

type FrameMeta struct {
	StartMarker    byte   // SolarMan V5 payload starting marker
	EndMarker      byte   // SolarMan V5 payload ending marker
	ReqControlCode uint16 // SolarMan V5 request control code
	ResControlCode uint16 // SolarMan V5 response control code
}

var DefaultMeta = FrameMeta{
	StartMarker:    0xA5,
	EndMarker:      0x15,
	ReqControlCode: 0x4510,
	ResControlCode: 0x1510,
}

func (inv *InverterLogger) NewFrame(deviceSN uint32, payload []byte) *Frame {
	return &Frame{
		PayloadLength:  uint16(len(payload)),
		ReqControlCode: inv.Meta.ReqControlCode,
		SerialNumber:   inv.GetNextSequenceNumber(),
		DeviceSN:       deviceSN,
		Payload:        payload,
	}
}

func (f *Frame) MarshalBinary(inv *InverterLogger) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteByte(inv.Meta.StartMarker)

	if err := binary.Write(&buf, binary.LittleEndian, f.PayloadLength); err != nil {
		return nil, fmt.Errorf("f.PayloadLength buf write failed - %w", err)
	}

	if err := binary.Write(&buf, binary.LittleEndian, inv.Meta.ReqControlCode); err != nil {
		return nil, fmt.Errorf("inv.Meta.ReqControlCode buf write failed - %w", err)
	}

	if err := binary.Write(&buf, binary.LittleEndian, f.SerialNumber); err != nil {
		return nil, fmt.Errorf("f.SerialNumber buf write failed - %w", err)
	}

	if err := binary.Write(&buf, binary.LittleEndian, f.DeviceSN); err != nil {
		return nil, fmt.Errorf("f.DeviceSN buf write failed - %w", err)
	}

	buf.Write(f.Payload)
	buf.WriteByte(calcCheckSum8(buf.Bytes()[1:])) // checksum (without start byte)
	buf.WriteByte(inv.Meta.EndMarker)

	return buf.Bytes(), nil
}

func (f *Frame) UnmarshalBinary(inv *InverterLogger, data []byte) error {
	buf := bytes.NewBuffer(data)

	startMarker := inv.Meta.StartMarker
	endMarker := inv.Meta.EndMarker
	resControlCode := inv.Meta.ResControlCode

	b, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read start marker - %w", err)
	} else if b != startMarker {
		return fmt.Errorf("expected 0x%X as start marker, got: 0x%X", startMarker, b)
	}

	if err := binary.Read(buf, binary.LittleEndian, &f.PayloadLength); err != nil {
		return fmt.Errorf("failed to read payload length - %w", err)
	}

	if err := binary.Read(buf, binary.LittleEndian, &f.ResControlCode); err != nil {
		return fmt.Errorf("failed to read control code - %w", err)
	} else if f.ResControlCode != resControlCode {
		return fmt.Errorf("expected 0x%X as control code, got: 0x%X", resControlCode, f.ResControlCode)
	}

	if err := binary.Read(buf, binary.BigEndian, &f.SerialNumber); err != nil {
		return fmt.Errorf("failed to read serial number - %w", err)
	}

	if err := binary.Read(buf, binary.LittleEndian, &f.DeviceSN); err != nil {
		return fmt.Errorf("failed to read device serial number - %w", err)
	}

	f.Payload = make([]byte, f.PayloadLength)
	n, err := buf.Read(f.Payload)
	if err != nil {
		return fmt.Errorf("failed to read payload - %w", err)
	} else if n != int(f.PayloadLength) {
		return fmt.Errorf("only read %d bytes instead of %d", n, f.PayloadLength)
	}

	// calculate expected checksum exclude startMarker & endMarker
	expectedChecksum := calcCheckSum8(data[1 : len(data)-2])

	// Read actual checksum
	actualChecksum, err := buf.ReadByte()
	if err != nil {
		return fmt.Errorf("failed to read checksum - %w", err)
	} else if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected 0x%X, got 0x%X", expectedChecksum, actualChecksum)
	}

	b, err = buf.ReadByte() // end marker
	if err != nil {
		return fmt.Errorf("failed to read end marker - %w", err)
	} else if b != endMarker {
		return fmt.Errorf("expected 0x%X as end marker, got: 0x%X", endMarker, b)
	}

	if buf.Len() != 0 {
		return fmt.Errorf("buffer not empty, %d bytes left", buf.Len())
	}

	return nil
}
