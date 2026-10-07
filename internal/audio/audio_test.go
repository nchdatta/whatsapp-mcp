package audio

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

// page builds a minimal Ogg page with a single segment table.
func page(granule int64, body []byte) []byte {
	var b bytes.Buffer
	b.WriteString("OggS")
	b.WriteByte(0) // version
	b.WriteByte(0) // header type
	binary.Write(&b, binary.LittleEndian, granule)
	b.Write(make([]byte, 12)) // serial, sequence, checksum
	var lacing []byte
	for n := len(body); ; n -= 255 {
		if n >= 255 {
			lacing = append(lacing, 255)
			continue
		}
		lacing = append(lacing, byte(n))
		break
	}
	b.WriteByte(byte(len(lacing)))
	b.Write(lacing)
	b.Write(body)
	return b.Bytes()
}

func opusHead(preSkip uint16) []byte {
	h := []byte("OpusHead")
	h = append(h, 1, 1) // version, channels
	h = binary.LittleEndian.AppendUint16(h, preSkip)
	h = binary.LittleEndian.AppendUint32(h, 48000)
	return append(h, 0, 0, 0)
}

func TestInspect(t *testing.T) {
	var data []byte
	data = append(data, page(0, opusHead(312))...)
	data = append(data, page(0, []byte("OpusTags"))...)
	// 3 seconds of audio: quiet, loud, quiet
	data = append(data, page(48000, make([]byte, 50))...)
	data = append(data, page(96000, make([]byte, 400))...)
	data = append(data, page(144000+312, make([]byte, 50))...)

	info, err := Inspect(data)
	if err != nil {
		t.Fatal(err)
	}
	if info.Duration != 3*time.Second {
		t.Errorf("duration = %v, want 3s", info.Duration)
	}
	if len(info.Waveform) != waveformBars {
		t.Fatalf("waveform has %d bars", len(info.Waveform))
	}
	mid, start := info.Waveform[waveformBars/2], info.Waveform[2]
	if mid <= start {
		t.Errorf("loud middle (%d) should be taller than quiet start (%d)", mid, start)
	}
	for i, v := range info.Waveform {
		if v > 100 {
			t.Errorf("bar %d = %d, out of range", i, v)
		}
	}
}

func TestInspectRejectsNonOpus(t *testing.T) {
	if _, err := Inspect([]byte("ID3 not ogg")); err == nil {
		t.Error("expected an error for non-Ogg data")
	}
	if _, err := Inspect(page(0, []byte("vorbis header"))); err == nil {
		t.Error("expected an error for Ogg without OpusHead")
	}
}
