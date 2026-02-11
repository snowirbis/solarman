package solarman

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
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
	conn           net.Conn
	connID         uint64
	connNext       uint64
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

/*
	Modified to persistent connection - InverterLogger.conn
*/

func (inv *InverterLogger) connect() error {
	if inv.conn != nil {
		return nil
	}

	conn, err := net.DialTimeout("tcp", inv.LoggerAddress, inv.Timeout)
	if err != nil {
		return inv.error("net.DialTimeout", "conn failed", err)
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(30 * time.Second)
		_ = tc.SetNoDelay(true)
	}

	inv.conn = conn
	inv.connNext++
	inv.connID = inv.connNext

	inv.debugConn("OPEN", "")

	return nil
}

func (inv *InverterLogger) do(requestFrame []byte) ([]byte, error) {
	if err := inv.connect(); err != nil {
		return nil, err
	}

	_ = inv.conn.SetWriteDeadline(time.Now().Add(inv.Timeout))
	_ = inv.conn.SetReadDeadline(time.Now().Add(inv.Timeout))

	inv.debug("net.requestFrame", "SENT", requestFrame)

	if _, err := inv.conn.Write(requestFrame); err != nil {
		inv.closeConn(inv.closeReason("write", err))
		return nil, inv.error("conn.Write", "write failed", err)
	}

	reply := make([]byte, 512)
	n, err := inv.conn.Read(reply)
	if err != nil {
		inv.closeConn(inv.closeReason("read", err))
		return nil, inv.error("conn.Read", "read failed", err)
	}

	reply = reply[:n]
	inv.debug("net.reply", "RECD", reply)

	return reply, nil
}

func (inv *InverterLogger) debugConn(event string, extra string) {
	if !inv.DebugEnable {
		return
	}

	if inv.conn == nil {
		inv.debug("net.conn", event, []byte("conn=nil "+extra), 1)
		return
	}

	laddr := inv.conn.LocalAddr().String()
	raddr := inv.conn.RemoteAddr().String()

	msg := []byte(
		fmt.Sprintf("id=%d %s -> %s %s",
			inv.connID, laddr, raddr, extra,
		),
	)

	inv.debug("net.conn", event, msg, 1)
}

func (inv *InverterLogger) closeConn(reason string) {
	if inv.conn == nil {
		return
	}

	inv.debugConn("CLOSE", "reason="+reason)

	_ = inv.conn.Close()
	inv.conn = nil
	inv.connID = 0
}

func (inv *InverterLogger) closeReason(prefix string, err error) string {
	if err == nil {
		return prefix
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return prefix + "_timeout"
	}
	if errors.Is(err, io.EOF) {
		return prefix + "_eof"
	}
	return prefix + "_error"
}

/*

Public methods using InverterLogger.conn

*/

func (inv *InverterLogger) Read(startReg, regCnt int) (map[int]uint16, error) {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	requestPayload, _ := inv.NewReadRequestPayload(uint16(startReg), uint16(regCnt)).MarshalBinary(inv)
	requestFrame, _ := inv.NewFrame(inv.LoggerSerialN, requestPayload).MarshalBinary(inv)

	reply, err := inv.do(requestFrame)
	if err != nil {
		return nil, inv.error("Read.do", "request failed", err)
	}

	var responseFrame Frame
	if err := responseFrame.UnmarshalBinary(inv, reply); err != nil {
		return nil, inv.error("Read.responseFrame.UnmarshalBinary", "frame unmarshal failed", err)
	}

	var responsePayload ResponsePayload
	if err := responsePayload.UnmarshalBinary(inv, responseFrame.Payload); err != nil {
		return nil, inv.error("Read.responsePayload.UnmarshalBinary", "payload unmarshal failed", err)
	}

	inv.debug("Read.responsePayload.Value", "RECD", responsePayload.Value)

	buf := bytes.NewBuffer(responsePayload.Value)

	res := make(map[int]uint16)
	for i := 0; i < regCnt; i++ {
		var val uint16
		if err := binary.Read(buf, binary.BigEndian, &val); err != nil {
			return nil, inv.error("Read.responsePayload.binary.Read", "read payload to buf failed", err)
		}
		res[startReg+i] = val
	}

	return res, nil
}

func (inv *InverterLogger) Write(startRegister int, values []int) (int, int, error) {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	numRegisters := len(values)
	registerValues := make([]uint16, numRegisters)
	for offset, value := range values {
		registerValues[offset] = uint16(value)
	}

	writePayload, err := inv.NewWriteRequestPayload(uint16(startRegister), registerValues).MarshalBinary()
	if err != nil {
		return 0, 0, inv.error("Write.writePayload", "payload marshal failed", err)
	}

	writeFrame, err := inv.NewFrame(inv.LoggerSerialN, writePayload).MarshalBinary(inv)
	if err != nil {
		return 0, 0, inv.error("Write.writeFrame", "frame marshal failed", err)
	}

	reply, err := inv.do(writeFrame)
	if err != nil {
		return 0, 0, inv.error("Write.do", "request failed", err)
	}

	var responseFrame Frame
	if err := responseFrame.UnmarshalBinary(inv, reply); err != nil {
		return 0, 0, inv.error("Write.responseFrame.UnmarshalBinary", "frame unmarshal failed", err)
	}

	count, start, err := inv.parseWriteResponse(responseFrame.Payload, values)
	if err != nil {
		return 0, 0, inv.error("Write.parseWriteResponse", "payload unmarshal failed", err)
	}

	return count, start, nil
}

func (inv *InverterLogger) Close() error {
	inv.mu.Lock()
	defer inv.mu.Unlock()

	if inv.conn == nil {
		// idempotent close
		if inv.DebugEnable {
			inv.debug("net.conn", "CLOSE", []byte("id=0 conn=nil reason=manual"))
		}
		return nil
	}

	inv.closeConn("manual")
	return nil
}
