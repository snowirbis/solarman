package solarman

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// makeFrame builds a raw Solarman V5 frame with an arbitrary control code and
// sequence so tests can synthesize responses, heartbeats and stale frames. It
// fails the test on any write error rather than producing a malformed frame.
func makeFrame(t *testing.T, inv *InverterLogger, controlCode, seq uint16, payload []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("makeFrame write failed: %v", err)
		}
	}
	must(b.WriteByte(inv.Meta.StartMarker))
	must(binary.Write(&b, binary.LittleEndian, uint16(len(payload))))
	must(binary.Write(&b, binary.LittleEndian, controlCode))
	must(binary.Write(&b, binary.LittleEndian, seq))
	must(binary.Write(&b, binary.LittleEndian, inv.LoggerSerialN))
	_, err := b.Write(payload)
	must(err)
	must(b.WriteByte(calcCheckSum8(b.Bytes()[1:]))) // checksum excludes start marker
	must(b.WriteByte(inv.Meta.EndMarker))
	return b.Bytes()
}

// request builds a request frame with a known sequence low byte so the test can
// craft the matching response.
func request(t *testing.T, inv *InverterLogger, seq uint16) []byte {
	return makeFrame(t, inv, inv.Meta.ReqControlCode, seq, []byte{0x01, 0x03, 0x02, 0x4a, 0x00, 0x69})
}

func controlCodes(frames []unsolicitedFrame) []uint16 {
	out := make([]uint16, len(frames))
	for i, f := range frames {
		out[i] = f.controlCode
	}
	return out
}

// TestDoMatchesSequenceAndSkipsNoise verifies that do() returns only the frame
// matching our request's sequence, collecting heartbeats (wrong control code)
// and data reports as unsolicited while skipping stale/lagged responses (right
// control code, wrong sequence).
func TestDoMatchesSequenceAndSkipsNoise(t *testing.T) {
	inv := Init("test", 0x12345678, 2)

	client, server := net.Pipe()
	inv.conn = client
	inv.connID = 1
	defer inv.closeConn("test")

	const reqSeq = 0x42
	req := request(t, inv, reqSeq)

	heartbeat := makeFrame(t, inv, 0x4710, 0x00, []byte{0x00})                        // wrong control code
	staleResp := makeFrame(t, inv, inv.Meta.ResControlCode, 0x41, []byte{0xAA, 0xBB}) // lagged response
	dataReport := makeFrame(t, inv, 0x4210, 0x00, []byte{0x01, 0x02})                 // wrong control code
	response := makeFrame(t, inv, inv.Meta.ResControlCode, reqSeq, []byte{0xDE, 0xAD, 0xBE, 0xEF})

	go func() {
		buf := make([]byte, 256)
		_, _ = server.Read(buf) // consume the request
		_, _ = server.Write(heartbeat)
		_, _ = server.Write(staleResp)
		_, _ = server.Write(dataReport)
		_, _ = server.Write(response)
	}()

	reply, unsolicited, err := inv.do(req)
	if err != nil {
		t.Fatalf("do() returned error: %v", err)
	}
	if !bytes.Equal(reply, response) {
		t.Fatalf("do() returned the wrong frame; sequence matching failed")
	}
	// Only the control-code-mismatch frames are reported as unsolicited (not the
	// stale same-control-code response, which is dropped silently).
	got := controlCodes(unsolicited)
	want := []uint16{0x4710, 0x4210}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("unsolicited control codes = %#x, want %#x", got, want)
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
	req := request(t, inv, reqSeq)
	response := makeFrame(t, inv, inv.Meta.ResControlCode, reqSeq, []byte{0x01, 0x02, 0x03})

	go func() {
		buf := make([]byte, 256)
		_, _ = server.Read(buf)
		_, _ = server.Write(response)
	}()

	reply, unsolicited, err := inv.do(req)
	if err != nil {
		t.Fatalf("do() returned error: %v", err)
	}
	if !bytes.Equal(reply, response) {
		t.Fatalf("returned frame does not match the response frame")
	}
	if len(unsolicited) != 0 {
		t.Fatalf("expected no unsolicited frames, got %d", len(unsolicited))
	}
}

// TestDoRejectsCorruptedFrame verifies the checksum guard in readFrame rejects a
// frame whose checksum byte has been tampered with.
func TestDoRejectsCorruptedFrame(t *testing.T) {
	inv := Init("test", 0x12345678, 1)

	client, server := net.Pipe()
	inv.conn = client
	inv.connID = 1
	defer inv.closeConn("test")

	const reqSeq = 0x09
	req := request(t, inv, reqSeq)
	corrupt := makeFrame(t, inv, inv.Meta.ResControlCode, reqSeq, []byte{0x01, 0x02})
	corrupt[len(corrupt)-2] ^= 0xFF // flip the checksum byte

	go func() {
		buf := make([]byte, 256)
		_, _ = server.Read(buf)
		_, _ = server.Write(corrupt)
	}()

	if _, _, err := inv.do(req); err == nil {
		t.Fatal("expected a checksum error for a corrupted frame")
	}
}

// TestDoTimesOutOnOnlyNoise ensures the read deadline bounds the wait when only
// non-matching frames keep arriving, and the noise is still surfaced.
func TestDoTimesOutOnOnlyNoise(t *testing.T) {
	inv := Init("test", 0x12345678, 1)

	client, server := net.Pipe()
	inv.conn = client
	inv.connID = 1
	defer inv.closeConn("test")

	const reqSeq = 0x10
	req := request(t, inv, reqSeq)
	heartbeat := makeFrame(t, inv, 0x4710, 0x00, []byte{0x00})

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
	_, unsolicited, err := inv.do(req)
	if err == nil {
		t.Fatal("expected a timeout error when no matching response arrives")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("do() took %v, expected it to give up near the 1s timeout", elapsed)
	}
	if len(unsolicited) == 0 {
		t.Fatal("expected heartbeats to be surfaced even on timeout")
	}
}

// TestFireUnsolicited verifies the hook dispatch helper.
func TestFireUnsolicited(t *testing.T) {
	inv := Init("test", 1, 1)
	var got []uint16
	inv.OnUnsolicited = func(cc uint16, _ []byte) { got = append(got, cc) }
	inv.fireUnsolicited([]unsolicitedFrame{{0x4710, nil}, {0x4210, nil}})
	if len(got) != 2 || got[0] != 0x4710 || got[1] != 0x4210 {
		t.Fatalf("hook received %#x, want [0x4710 0x4210]", got)
	}

	// A nil hook must be a no-op.
	inv.OnUnsolicited = nil
	inv.fireUnsolicited([]unsolicitedFrame{{0x4710, nil}})
}
