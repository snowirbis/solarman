package solarman

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// makeFrame builds a raw Solarman V5 frame with an arbitrary control code and
// sequence so tests can synthesize responses, heartbeats and stale frames.
func makeFrame(inv *InverterLogger, controlCode, seq uint16, payload []byte) []byte {
	var b bytes.Buffer
	b.WriteByte(inv.Meta.StartMarker)
	_ = binary.Write(&b, binary.LittleEndian, uint16(len(payload)))
	_ = binary.Write(&b, binary.LittleEndian, controlCode)
	_ = binary.Write(&b, binary.LittleEndian, seq)
	_ = binary.Write(&b, binary.LittleEndian, inv.LoggerSerialN)
	b.Write(payload)
	b.WriteByte(calcCheckSum8(b.Bytes()[1:])) // checksum excludes start marker
	b.WriteByte(inv.Meta.EndMarker)
	return b.Bytes()
}

// request builds a request frame with a known sequence low byte so the test can
// craft the matching response.
func request(inv *InverterLogger, seq uint16) []byte {
	return makeFrame(inv, inv.Meta.ReqControlCode, seq, []byte{0x01, 0x03, 0x02, 0x4a, 0x00, 0x69})
}

// TestDoMatchesSequenceAndSkipsNoise verifies that do() returns only the frame
// matching our request's sequence, skipping heartbeats (wrong control code),
// data reports, and stale/lagged responses (right control code, wrong sequence).
func TestDoMatchesSequenceAndSkipsNoise(t *testing.T) {
	inv := Init("test", 0x12345678, 2)

	client, server := net.Pipe()
	inv.conn = client
	inv.connID = 1
	defer inv.closeConn("test")

	const reqSeq = 0x42
	req := request(inv, reqSeq)

	heartbeat := makeFrame(inv, 0x4710, 0x00, []byte{0x00})                        // wrong control code
	staleResp := makeFrame(inv, inv.Meta.ResControlCode, 0x41, []byte{0xAA, 0xBB}) // lagged response
	dataReport := makeFrame(inv, 0x4210, 0x00, []byte{0x01, 0x02})                 // wrong control code
	response := makeFrame(inv, inv.Meta.ResControlCode, reqSeq, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	var skipped []uint16
	inv.OnUnsolicited = func(cc uint16, _ []byte) { skipped = append(skipped, cc) }

	go func() {
		buf := make([]byte, 256)
		_, _ = server.Read(buf) // consume the request
		_, _ = server.Write(heartbeat)
		_, _ = server.Write(staleResp)
		_, _ = server.Write(dataReport)
		_, _ = server.Write(response)
	}()

	reply, err := inv.do(req)
	if err != nil {
		t.Fatalf("do() returned error: %v", err)
	}
	if !bytes.Equal(reply, response) {
		t.Fatalf("do() returned the wrong frame; seq matching failed")
	}
	// Only the control-code-mismatch frames go through the hook (not the stale one).
	want := []uint16{0x4710, 0x4210}
	if len(skipped) != len(want) || skipped[0] != want[0] || skipped[1] != want[1] {
		t.Fatalf("OnUnsolicited control codes = %#x, want %#x", skipped, want)
	}
}

// TestDoReturnsMatchingResponse confirms the clean happy path.
func TestDoReturnsMatchingResponse(t *testing.T) {
	inv := Init("test", 0x12345678, 2)

	client, server := net.Pipe()
	inv.conn = client
	inv.connID = 1
	defer inv.closeConn("test")

	const reqSeq = 0x07
	req := request(inv, reqSeq)
	response := makeFrame(inv, inv.Meta.ResControlCode, reqSeq, []byte{0x01, 0x02, 0x03})

	go func() {
		buf := make([]byte, 256)
		_, _ = server.Read(buf)
		_, _ = server.Write(response)
	}()

	reply, err := inv.do(req)
	if err != nil {
		t.Fatalf("do() returned error: %v", err)
	}
	if !bytes.Equal(reply, response) {
		t.Fatalf("returned frame does not match the response frame")
	}
}

// TestDoTimesOutOnOnlyNoise ensures the read deadline bounds the wait when only
// non-matching frames keep arriving.
func TestDoTimesOutOnOnlyNoise(t *testing.T) {
	inv := Init("test", 0x12345678, 1)

	client, server := net.Pipe()
	inv.conn = client
	inv.connID = 1
	defer inv.closeConn("test")

	const reqSeq = 0x10
	req := request(inv, reqSeq)
	heartbeat := makeFrame(inv, 0x4710, 0x00, []byte{0x00})

	go func() {
		buf := make([]byte, 256)
		_, _ = server.Read(buf)
		for {
			if _, err := server.Write(heartbeat); err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	start := time.Now()
	if _, err := inv.do(req); err == nil {
		t.Fatal("expected a timeout error when no matching response arrives")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("do() took %v, expected it to give up near the 1s timeout", elapsed)
	}
}
