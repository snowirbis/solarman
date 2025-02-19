package solarman

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// -----------------------------------------------------------------------------
// Write Multiple Registers Request (Modbus Function 0x10)
// -----------------------------------------------------------------------------

type WriteRequestPayload struct {
	FrameType    uint8  // Example: 0x02
	SensorType   uint16 // Example: 0x0000
	DeliveryTime uint32 // Timestamp of delivery (if needed)
	PowerOnTime  uint32 // Device uptime (if needed)
	OffsetTime   uint32 // Time offset (if needed)

	DeviceAddress   uint8    // Device address, usually 0x01
	FunctionCode    uint8    // Function code for writing multiple registers = 0x10
	RegisterAddress uint16   // Starting register address
	RegisterValues  []uint16 // List of values to write (2 bytes per register)
}

func (inv *InverterLogger) NewWriteRequestPayload(registerAddress uint16, values []uint16) *WriteRequestPayload {
	return &WriteRequestPayload{
		FrameType:       0x02,
		SensorType:      0x0000,
		DeliveryTime:    0x00000000,
		PowerOnTime:     0x00000000,
		OffsetTime:      0x00000000,
		DeviceAddress:   0x01,
		FunctionCode:    0x10, // Write Multiple Registers
		RegisterAddress: registerAddress,
		RegisterValues:  values,
	}
}

func (w *WriteRequestPayload) marshalBusinessData() ([]byte, error) {
	var buf bytes.Buffer

	// According to Modbus specification, function 0x10:
	// 1 byte: DeviceAddress
	// 1 byte: FunctionCode (0x10)
	// 2 bytes: Starting address of register (Big Endian)
	// 2 bytes: Number of registers (Big Endian)
	// 1 byte: Byte Count = number of registers * 2
	// N*2 bytes: Data (each register — 2 bytes, Big Endian)
	// 2 bytes: CRC16 (Little Endian)	buf.WriteByte(w.DeviceAddress)

	buf.WriteByte(w.DeviceAddress)
	buf.WriteByte(w.FunctionCode)

	if err := binary.Write(&buf, binary.BigEndian, w.RegisterAddress); err != nil {
		return nil, fmt.Errorf("w.RegisterAddress binary Write failed: %w", err)
	}

	quantity := uint16(len(w.RegisterValues))

	if err := binary.Write(&buf, binary.BigEndian, quantity); err != nil {
		return nil, fmt.Errorf("quantity binary Write failed: %w", err)
	}

	byteCount := uint8(quantity * 2)
	buf.WriteByte(byteCount)

	for _, val := range w.RegisterValues {
		if err := binary.Write(&buf, binary.BigEndian, val); err != nil {
			return nil, fmt.Errorf("register value binary Write failed: %w", err)
		}
	}

	crc := calcCRC16Modbus(buf.Bytes())

	if err := binary.Write(&buf, binary.LittleEndian, crc); err != nil {
		return nil, fmt.Errorf("CRC16 binary Write failed: %w", err)
	}

	return buf.Bytes(), nil
}

func (w *WriteRequestPayload) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer

	// Write payload header
	if err := binary.Write(&buf, binary.LittleEndian, w.FrameType); err != nil {
		return nil, fmt.Errorf("w.FrameType binary Write failed: %w", err)
	}
	if err := binary.Write(&buf, binary.LittleEndian, w.SensorType); err != nil {
		return nil, fmt.Errorf("SensorType binary Write failed: %w", err)
	}
	if err := binary.Write(&buf, binary.LittleEndian, w.DeliveryTime); err != nil {
		return nil, fmt.Errorf("w.DeliveryTime binary Write failed: %w", err)
	}
	if err := binary.Write(&buf, binary.LittleEndian, w.PowerOnTime); err != nil {
		return nil, fmt.Errorf("w.PowerOnTime binary Write failed: %w", err)
	}
	if err := binary.Write(&buf, binary.LittleEndian, w.OffsetTime); err != nil {
		return nil, fmt.Errorf("w.OffsetTime binary Write failed: %w", err)
	}

	// Add business data to record registers

	businessData, err := w.marshalBusinessData()
	if err != nil {
		return nil, fmt.Errorf("MarshalBinary w.marshalBusinessData failed: %w", err)
	}

	buf.Write(businessData)

	return buf.Bytes(), nil
}

// parseWriteResponse processes the server response in V5 format.

func (inv *InverterLogger) parseWriteResponse(responsePayload []byte, values []int) (int, int, error) {
	// Look for the start of the Modbus response (should contain 01 10)
	startIndex := -1
	for i := 0; i < len(responsePayload)-2; i++ {
		if responsePayload[i] == 0x01 && responsePayload[i+1] == 0x10 {
			startIndex = i
			break
		}
	}

	if startIndex == -1 {
		return 0, 0, fmt.Errorf("Modbus response not found in payload")
	}

	// Make sure the response contains at least 8 bytes after `01 10`
	if len(responsePayload[startIndex:]) < 8 {
		return 0, 0, fmt.Errorf("unexpected response length: %d bytes, expected at least 8", len(responsePayload[startIndex:]))
	}

	// Fetch only Modbus-part (8 bytes after 01 10)
	modbusResponse := responsePayload[startIndex : startIndex+8]
	buf := bytes.NewBuffer(modbusResponse)

	var deviceAddress, functionCode uint8
	var respStartAddress, respQuantity uint16

	if err := binary.Read(buf, binary.BigEndian, &deviceAddress); err != nil {
		return 0, 0, fmt.Errorf("failed to read deviceAddress: %w", err)
	}
	if err := binary.Read(buf, binary.BigEndian, &functionCode); err != nil {
		return 0, 0, fmt.Errorf("failed to read functionCode: %w", err)
	}
	if err := binary.Read(buf, binary.BigEndian, &respStartAddress); err != nil {
		return 0, 0, fmt.Errorf("failed to read starting address: %w", err)
	}
	if err := binary.Read(buf, binary.BigEndian, &respQuantity); err != nil {
		return 0, 0, fmt.Errorf("failed to read quantity: %w", err)
	}

	// Determine the extra bytes after the Modbus response
	// remainingBytes := len(responsePayload) - (startIndex + 8)
	// if remainingBytes > 0 {
	// I don't know what we should do if some bytes left
	// }

	// Check the correctness of the answer
	if deviceAddress != 0x01 || functionCode != 0x10 {
		return 0, 0, fmt.Errorf("unexpected response: deviceAddress %d, functionCode %d", deviceAddress, functionCode)
	}
	if respQuantity != uint16(len(values)) {
		return 0, 0, fmt.Errorf("unexpected quantity: expected %d, got %d", len(values), respQuantity)
	}

	// Return number of bytes written (each register is 2 bytes) and the starting register
	writtenBytes := int(respQuantity) * 2
	startRegister := int(respStartAddress)

	return writtenBytes, startRegister, nil
}
