package solarman

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// -----------------------------------------------------------------------------
// Register read request (Modbus function 0x03)
// -----------------------------------------------------------------------------

type ReadRequestPayload struct {
	FrameType    uint8
	SensorType   uint16
	DeliveryTime uint32
	PowerOnTime  uint32
	OffsetTime   uint32

	DeviceAddress uint8  // device address, usually 0x01
	FunctionCode  uint8  // 0x03 – reading registers
	StartReg      uint16 // address of the first register
	RegCount      uint16 // number of registers to read
}

func (inv *InverterLogger) NewReadPayload(startReg, regCount uint16) *ReadRequestPayload {
	return &ReadRequestPayload{
		FrameType:     0x02,
		SensorType:    0x0000,
		DeliveryTime:  0x00000000,
		PowerOnTime:   0x00000000,
		OffsetTime:    0x00000000,
		DeviceAddress: 0x01,
		FunctionCode:  0x03,
		StartReg:      startReg,
		RegCount:      regCount,
	}
}

func (r *ReadRequestPayload) marshalBusinessData() []byte {
	var buf bytes.Buffer
	buf.WriteByte(r.DeviceAddress)
	buf.WriteByte(r.FunctionCode)
	binary.Write(&buf, binary.BigEndian, r.StartReg)
	binary.Write(&buf, binary.BigEndian, r.RegCount)
	// Modbus CRC16 proto requirement (2 bytes, little endian)
	binary.Write(&buf, binary.LittleEndian, calcCRC16Modbus(buf.Bytes()))
	return buf.Bytes()
}

func (r *ReadRequestPayload) MarshalBinary(inv *InverterLogger) ([]byte, error) {
	var buf bytes.Buffer

	// Form the payload header
	if err := binary.Write(&buf, binary.LittleEndian, r.FrameType); err != nil {
		return nil, fmt.Errorf("r.FrameType buf write failed - %w", err)
	}
	if err := binary.Write(&buf, binary.LittleEndian, r.SensorType); err != nil {
		return nil, fmt.Errorf("r.SensorType buf write failed - %w", err)
	}
	if err := binary.Write(&buf, binary.LittleEndian, r.DeliveryTime); err != nil {
		return nil, fmt.Errorf("r.DeliveryTime buf write failed - %w", err)
	}
	if err := binary.Write(&buf, binary.LittleEndian, r.PowerOnTime); err != nil {
		return nil, fmt.Errorf("r.PowerOnTime buf write failed - %w", err)
	}
	if err := binary.Write(&buf, binary.LittleEndian, r.OffsetTime); err != nil {
		return nil, fmt.Errorf("r.OffsetTime buf write failed - %w", err)
	}

	// Add business data (address, function, register, quantity and CRC)
	buf.Write(r.marshalBusinessData())

	return buf.Bytes(), nil
}

// -----------------------------------------------------------------------------
// Response to register read request
// -----------------------------------------------------------------------------

type ResponsePayload struct {
	FrameType    uint8
	StatusCode   uint8
	DeliveryTime uint32
	PowerOnTime  uint32
	OffsetTime   uint32

	DeviceAddress uint8
	FunctionCode  uint8
	ValueLength   uint8
	Value         []byte
}

// full frame unmarshaling
func (r *ResponsePayload) UnmarshalBinary(inv *InverterLogger, data []byte) error {
	buf := bytes.NewBuffer(data)

	if err := binary.Read(buf, binary.LittleEndian, &r.FrameType); err != nil {
		return fmt.Errorf("r.FrameType binary read failed - %w", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &r.StatusCode); err != nil {
		return fmt.Errorf("r.StatusCode binary read failed - %w", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &r.DeliveryTime); err != nil {
		return fmt.Errorf("r.DeliveryTime binary read failed - %w", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &r.PowerOnTime); err != nil {
		return fmt.Errorf("r.PowerOnTime binary read failed - %w", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &r.OffsetTime); err != nil {
		return fmt.Errorf("r.OffsetTime binary read failed - %w", err)
	}

	return r.unmarshalBusinessPayload(inv, buf.Bytes())
}

// payload unmarshaling
func (r *ResponsePayload) unmarshalBusinessPayload(inv *InverterLogger, data []byte) error {
	buf := bytes.NewBuffer(data)

	if err := binary.Read(buf, binary.LittleEndian, &r.DeviceAddress); err != nil {
		return fmt.Errorf("r.DeviceAddress binary read failed - %w", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &r.FunctionCode); err != nil {
		return fmt.Errorf("r.FunctionCode binary read failed - %w", err)
	}
	if err := binary.Read(buf, binary.LittleEndian, &r.ValueLength); err != nil {
		return fmt.Errorf("r.ValueLength binary read failed - %w", err)
	}

	r.Value = make([]byte, r.ValueLength)

	n, err := buf.Read(r.Value)

	if err != nil {
		return fmt.Errorf("r.Value buf read failed - %w", err)
	} else if n != int(r.ValueLength) {
		return fmt.Errorf("%d bytes read of expected %d bytes", n, r.ValueLength)
	}

	var crc uint16
	if err := binary.Read(buf, binary.LittleEndian, &crc); err != nil {
		return fmt.Errorf("CRC binary read failed - %w", err)
	}

	// Compute expected CRC (excluding last 2 bytes)
	expectedCRC := calcCRC16Modbus(data[:len(data)-2])

	// Compare CRC values
	if crc != expectedCRC {
		return fmt.Errorf("CRC mismatch: expected 0x%X, got 0x%X", expectedCRC, crc)
	}

	// Skip two bytes that can be null values
	buf.Next(2)

	if buf.Len() != 0 {
		return fmt.Errorf("%d bytes left in buffer", buf.Len())
	}

	return nil
}
