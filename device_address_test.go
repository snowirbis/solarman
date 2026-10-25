package solarman

import "testing"

func TestDeviceAddress(t *testing.T) {
	inv := Init("test", 1, 1)
	if inv.DeviceAddress != 0x01 {
		t.Fatalf("default device address: got %d, want 1", inv.DeviceAddress)
	}

	inv.SetDeviceAddress(3)

	read, err := inv.NewReadRequestPayload(18, 1).marshalBusinessData()
	if err != nil {
		t.Fatal(err)
	}
	if read[0] != 3 {
		t.Fatalf("read request device address: got %d, want 3", read[0])
	}

	write, err := inv.NewWriteRequestPayload(18, []uint16{4140}).marshalBusinessData()
	if err != nil {
		t.Fatal(err)
	}
	if write[0] != 3 {
		t.Fatalf("write request device address: got %d, want 3", write[0])
	}

	response := []byte{0x03, 0x10, 0x00, 0x12, 0x00, 0x01}
	if _, _, err := inv.parseWriteResponse(append(response, 0, 0), []int{4140}); err != nil {
		t.Fatal(err)
	}

	inv.SetDeviceAddress(1)
	if _, _, err := inv.parseWriteResponse(append(response, 0, 0), []int{4140}); err == nil {
		t.Fatal("expected error for response from other device")
	}
}
