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

	// OnUnsolicited, if set, is invoked for every frame received from the logger
	// whose control code is not the expected response control code — e.g. the
	// logger's periodic heartbeat (0x4710) or data-report frames. Such frames are
	// skipped while waiting for the matching response. The callback is dispatched
	// after the internal lock has been released, so it is free to call back into
	// the logger (e.g. issue another read).
	OnUnsolicited func(controlCode uint16, frame []byte)
}

// unsolicitedFrame is a frame received while waiting for a response whose control
// code is not the expected response code (heartbeat / data report). They are
// collected during the locked exchange and dispatched to OnUnsolicited afterwards.
type unsolicitedFrame struct {
	controlCode uint16
	frame       []byte
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

// exchange sends a request and returns the matching response under the I/O lock,
// together with any unsolicited frames seen while waiting. The caller dispatches
// those frames to OnUnsolicited after the lock is released (see fireUnsolicited).
func (inv *InverterLogger) exchange(requestFrame []byte) ([]byte, []unsolicitedFrame, error) {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	return inv.do(requestFrame)
}

// fireUnsolicited dispatches collected unsolicited frames to the hook. It must be
// called without the internal lock held so the callback may re-enter the logger.
func (inv *InverterLogger) fireUnsolicited(frames []unsolicitedFrame) {
	cb := inv.OnUnsolicited
	if cb == nil {
		return
	}
	for _, f := range frames {
		cb(f.controlCode, f.frame)
	}
}

func (inv *InverterLogger) do(requestFrame []byte) ([]byte, []unsolicitedFrame, error) {
	if err := inv.connect(); err != nil {
		return nil, nil, err
	}

	_ = inv.conn.SetWriteDeadline(time.Now().Add(inv.Timeout))
	_ = inv.conn.SetReadDeadline(time.Now().Add(inv.Timeout))

	inv.debug("net.requestFrame", "SENT", requestFrame)

	if _, err := inv.conn.Write(requestFrame); err != nil {
		inv.closeConn(inv.closeReason("write", err))
		return nil, nil, inv.error("conn.Write", "write failed", err)
	}

	// The logger multiplexes several kinds of frames onto the same stream:
	//   * the Modbus response we asked for,
	//   * unsolicited heartbeat (control code 0x4710) and data-report frames,
	//   * stale/duplicate responses that lag the request stream.
	// It echoes the request's first sequence byte in the matching response, so we
	// match on both the control code and that byte. This skips heartbeats and,
	// crucially, never returns a stale or wrong-length response (which otherwise
	// surfaces as a desynced read or a payload EOF). Reading continues until the
	// matching response arrives or the deadline expires.
	wantSeq := requestFrame[5] // first sequence byte, echoed back by the logger
	deadline := time.Now().Add(inv.Timeout)
	var unsolicited []unsolicitedFrame
	for {
		_ = inv.conn.SetReadDeadline(deadline)

		frame, err := inv.readFrame()
		if err != nil {
			inv.closeConn(inv.closeReason("read", err))
			return nil, unsolicited, inv.error("conn.Read", "read failed", err)
		}

		controlCode := binary.LittleEndian.Uint16(frame[3:5])
		if controlCode == inv.Meta.ResControlCode && frame[5] == wantSeq {
			inv.debug("net.reply", "RECD", frame)
			return frame, unsolicited, nil
		}

		// Not our response. Collect frames whose control code is not the expected
		// response (heartbeat / data report) for later dispatch; stale or duplicate
		// same-control-code frames are dropped silently.
		if controlCode != inv.Meta.ResControlCode {
			inv.debug("net.reply", "SKIP-UNSOLICITED", frame)
			unsolicited = append(unsolicited, unsolicitedFrame{controlCode, frame})
		} else {
			inv.debug("net.reply", "SKIP-STALE", frame)
		}

		if !time.Now().Before(deadline) {
			err := fmt.Errorf("no matching response for sequence 0x%02X before deadline", wantSeq)
			inv.closeConn("read_timeout")
			return nil, unsolicited, inv.error("do.waitResponse", "response timed out", err)
		}
	}
}

// readFrame reads exactly one complete Solarman V5 frame from the connection,
// using the length field to frame the read. The frame layout is:
//
//	start(1) length(2,LE) control(2,LE) sequence(2) deviceSN(4) payload(length) checksum(1) end(1)
func (inv *InverterLogger) readFrame() ([]byte, error) {
	const (
		headerLen = 11 // start + length + control + sequence + deviceSN
		// maxPayloadLen bounds the network-controlled length field so a malformed
		// or hostile frame cannot drive a large allocation. Real V5 frames are a
		// few hundred bytes; this leaves generous headroom.
		maxPayloadLen = 4096
	)

	header := make([]byte, headerLen)
	if _, err := io.ReadFull(inv.conn, header); err != nil {
		return nil, err
	}
	if header[0] != inv.Meta.StartMarker {
		return nil, fmt.Errorf("expected 0x%X as start marker, got: 0x%X", inv.Meta.StartMarker, header[0])
	}

	payloadLen := binary.LittleEndian.Uint16(header[1:3])
	if int(payloadLen) > maxPayloadLen {
		return nil, fmt.Errorf("payload length %d exceeds maximum %d", payloadLen, maxPayloadLen)
	}

	rest := make([]byte, int(payloadLen)+2) // payload + checksum + end marker
	if _, err := io.ReadFull(inv.conn, rest); err != nil {
		return nil, err
	}

	frame := append(header, rest...)
	if last := frame[len(frame)-1]; last != inv.Meta.EndMarker {
		return nil, fmt.Errorf("expected 0x%X as end marker, got: 0x%X", inv.Meta.EndMarker, last)
	}

	// Validate the V5 checksum (sum of every byte between the start marker and the
	// checksum/end-marker bytes) so a corrupted frame is rejected here rather than
	// accidentally satisfying the control-code/sequence match later on.
	computed := calcCheckSum8(frame[1 : len(frame)-2])
	if got := frame[len(frame)-2]; got != computed {
		return nil, fmt.Errorf("frame checksum mismatch: computed 0x%02X, frame carries 0x%02X", computed, got)
	}

	return frame, nil
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
	requestPayload, _ := inv.NewReadRequestPayload(uint16(startReg), uint16(regCnt)).MarshalBinary(inv)
	requestFrame, _ := inv.NewFrame(inv.LoggerSerialN, requestPayload).MarshalBinary(inv)

	reply, unsolicited, err := inv.exchange(requestFrame)
	inv.fireUnsolicited(unsolicited)
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

	reply, unsolicited, err := inv.exchange(writeFrame)
	inv.fireUnsolicited(unsolicited)
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
