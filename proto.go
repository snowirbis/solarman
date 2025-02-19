package solarman

import (
	"bytes"
	"encoding/binary"
	"net"
	"sync"
	"time"
)

const (
// version = "1.0.2"
)

// -----------------------------------------------------------------------------
// Basic structure and functions
// -----------------------------------------------------------------------------

type InverterLogger struct {
	LoggerAddress  string
	LoggerSerialN  uint32
	DebugEnable    bool
	SequenceNumber uint32
	Timeout        time.Duration
	Meta           FrameMeta
	mu             sync.Mutex
}

func Init(address string, sn uint32, timeout int) *InverterLogger {
	return &InverterLogger{
		DebugEnable:    false,
		SequenceNumber: 0,
		LoggerAddress:  address,
		LoggerSerialN:  sn,
		Meta:           DefaultMeta,
		Timeout:        time.Duration(timeout) * time.Second,
	}
}

/*

assign own meta
defaults defined in frame.go

type FrameMeta struct {
	StartMarker    byte   // SolarMan V5 payload starting marker
	EndMarker      byte   // SolarMan V5 payload ending marker
	ReqControlCode uint16 // SolarMan V5 request control code
	ResControlCode uint16 // SolarMan V5 response control code
}
*/

func (inv *InverterLogger) SetMeta(StartMarker byte, EndMarker byte, ReqControlCode uint16, ResControlCode uint16) {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	inv.Meta.StartMarker = StartMarker
	inv.Meta.EndMarker = EndMarker
	inv.Meta.ReqControlCode = ReqControlCode
	inv.Meta.ResControlCode = ResControlCode
}

func (inv *InverterLogger) SetDebug(enable bool) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.DebugEnable = enable
}

func (inv *InverterLogger) Read(startReg, regCnt int) (map[int]uint16, error) {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	conn, err := net.DialTimeout("tcp", inv.LoggerAddress, inv.Timeout)
	if err != nil {
		return nil, Err(inv, "Read.net.DialTimeout", "conn failed", err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(inv.Timeout)); err != nil {
		return nil, Err(inv, "Read.net.SetReadDeadline", "failed to set read timeout", err)
	}

	if err := conn.SetWriteDeadline(time.Now().Add(inv.Timeout)); err != nil {
		return nil, Err(inv, "Read.net.SetWriteDeadline", "failed to set write timeout", err)
	}

	requestPayload, _ := inv.NewReadPayload(uint16(startReg), uint16(regCnt)).MarshalBinary(inv)
	requestFrame, _ := inv.NewFrame(inv.LoggerSerialN, requestPayload).MarshalBinary(inv)

	Debug(inv, "Read.requestFrame", "SENT", requestFrame)

	_, err = conn.Write(requestFrame)
	if err != nil {
		return nil, Err(inv, "Read.conn.Write", "write failed", err)
	}

	reply := make([]byte, 512)
	replyLen, err := conn.Read(reply)
	if err != nil {
		return nil, Err(inv, "Read.conn.Read", "read failed", err)
	}

	reply = reply[:replyLen]

	Debug(inv, "Read.reply", "RECD", reply)

	var responseFrame Frame
	err = responseFrame.UnmarshalBinary(inv, reply)
	if err != nil {
		return nil, Err(inv, "Read.responseFrame.UnmarshalBinary", "frame unmarshal failed", err)
	}

	var responsePayload ResponsePayload
	err = responsePayload.UnmarshalBinary(inv, responseFrame.Payload)
	if err != nil {
		return nil, Err(inv, "Read.responsePayload.UnmarshalBinary", "payload unmarshal failed", err)
	}

	Debug(inv, "Read.responsePayload.Value", "RECD", responsePayload.Value)

	buf := bytes.NewBuffer(responsePayload.Value)

	res := make(map[int]uint16)
	for i := 0; i < regCnt; i++ {
		var val uint16
		if err := binary.Read(buf, binary.BigEndian, &val); err != nil {
			return nil, Err(inv, "Read.responsePayload.binary.Read", "read payload to buf failed", err)
		}
		res[startReg+i] = val
	}

	return res, nil
}

func (inv *InverterLogger) Write(startRegister int, values []int) (int, int, error) {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	conn, err := net.DialTimeout("tcp", inv.LoggerAddress, inv.Timeout)
	if err != nil {
		return 0, 0, Err(inv, "Write.net.DialTimeout", "conn failed", err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(inv.Timeout)); err != nil {
		return 0, 0, Err(inv, "Read.net.SetReadDeadline", "failed to set read timeout", err)
	}

	if err := conn.SetWriteDeadline(time.Now().Add(inv.Timeout)); err != nil {
		return 0, 0, Err(inv, "Read.net.SetWriteDeadline", "failed to set write timeout", err)
	}

	numRegisters := len(values)
	registerValues := make([]uint16, numRegisters)

	for offset, value := range values {
		registerValues[offset] = uint16(value)
	}

	writePayload, err := inv.NewWriteRequestPayload(uint16(startRegister), registerValues).MarshalBinary()
	if err != nil {
		return 0, 0, Err(inv, "Write.writePayload", "payload marshal failed", err)
	}

	writeFrame, err := inv.NewFrame(inv.LoggerSerialN, writePayload).MarshalBinary(inv)
	if err != nil {
		return 0, 0, Err(inv, "Write.writeFrame", "frame marshal failed", err)
	}

	Debug(inv, "sendWriteRequest.writeFrame", "SENT", writeFrame)

	_, err = conn.Write(writeFrame)
	if err != nil {
		return 0, 0, Err(inv, "Write.conn.Write", "write failed", err)
	}

	reply := make([]byte, 512)
	replyLen, err := conn.Read(reply)
	if err != nil {
		return 0, 0, Err(inv, "Write.conn.Read", "read failed", err)
	}

	reply = reply[:replyLen]

	Debug(inv, "sendWriteRequest.reply", "RECD", reply)

	var responseFrame Frame
	if err = responseFrame.UnmarshalBinary(inv, reply); err != nil {
		return 0, 0, Err(inv, "Write.responseFrame.UnmarshalBinary", "frame unmarshal failed", err)
	}

	count, start, err := inv.parseWriteResponse(responseFrame.Payload, values)
	if err != nil {
		return 0, 0, Err(inv, "Write.parseWriteResponse.responseFrame.Payload", "payload unmarshal failed", err)
	}

	return count, start, nil
}
