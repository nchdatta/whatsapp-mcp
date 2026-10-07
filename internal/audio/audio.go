// Package audio prepares voice notes: converting to Ogg Opus and reading the
// duration and a waveform preview out of the Ogg container.
package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IsOpus reports whether the file looks like Ogg Opus by extension.
func IsOpus(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ogg", ".opus", ".oga":
		return true
	}
	return false
}

// ErrNoFFmpeg means ffmpeg is needed but not installed.
var ErrNoFFmpeg = errors.New("ffmpeg is not installed (needed to convert audio to a voice note); send the file as a regular attachment instead")

// ToOpus converts any audio (or video's audio track) to mono Ogg Opus in a temp
// file. Call cleanup when done with it.
func ToOpus(ctx context.Context, input string) (path string, cleanup func(), err error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", nil, ErrNoFFmpeg
	}
	out, err := os.CreateTemp("", "voice-*.ogg")
	if err != nil {
		return "", nil, err
	}
	out.Close()
	cleanup = func() { os.Remove(out.Name()) }

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-loglevel", "error", "-y",
		"-i", input, "-vn", "-ac", "1", "-ar", "48000",
		"-c:a", "libopus", "-b:a", "24k", "-application", "voip",
		"-f", "ogg", out.Name())
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("ffmpeg: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.Name(), cleanup, nil
}

// Info is what WhatsApp wants to know about a voice note.
type Info struct {
	Duration time.Duration
	Waveform []byte // 64 samples, 0-100
}

const (
	opusRate     = 48000 // Opus granule positions always count 48 kHz samples
	waveformBars = 64
)

type oggPage struct {
	granule int64
	body    []byte
}

// readPages splits an Ogg stream into pages. It stops at the first malformed page.
func readPages(data []byte) []oggPage {
	var pages []oggPage
	for len(data) >= 27 && string(data[:4]) == "OggS" {
		segments := int(data[26])
		if len(data) < 27+segments {
			break
		}
		bodyLen := 0
		for _, l := range data[27 : 27+segments] {
			bodyLen += int(l)
		}
		end := 27 + segments + bodyLen
		if len(data) < end {
			break
		}
		pages = append(pages, oggPage{
			granule: int64(binary.LittleEndian.Uint64(data[6:14])),
			body:    data[27+segments : end],
		})
		data = data[end:]
	}
	return pages
}

// Inspect reads duration and a waveform from Ogg Opus data. The waveform is
// derived from how many encoded bytes each slice of time uses: with variable
// bitrate Opus, louder passages take more bytes, so this tracks loudness well
// enough for the preview bars without decoding audio.
func Inspect(data []byte) (Info, error) {
	pages := readPages(data)
	if len(pages) == 0 || !bytes.HasPrefix(pages[0].body, []byte("OpusHead")) {
		return Info{}, errors.New("not an Ogg Opus file")
	}
	head := pages[0].body
	var preSkip int64
	if len(head) >= 12 {
		preSkip = int64(binary.LittleEndian.Uint16(head[10:12]))
	}

	var last int64
	for _, p := range pages {
		if p.granule > last {
			last = p.granule
		}
	}
	samples := last - preSkip
	if samples <= 0 {
		return Info{}, errors.New("Ogg Opus file has no audio")
	}

	// Spread each audio page's bytes over its time span
	bins := make([]float64, waveformBars)
	prev := int64(0)
	for _, p := range pages[1:] {
		if p.granule <= 0 || p.granule < prev {
			continue // header/comment pages carry no audio
		}
		mid := (prev + p.granule) / 2
		bin := int(mid * waveformBars / (samples + preSkip))
		if bin >= waveformBars {
			bin = waveformBars - 1
		}
		span := float64(p.granule-prev) / opusRate
		if span > 0 {
			bins[bin] += float64(len(p.body)) / span
		}
		prev = p.granule
	}

	return Info{
		Duration: time.Duration(samples) * time.Second / opusRate,
		Waveform: normalize(bins),
	}, nil
}

// normalize scales values to 0-100, filling empty bins from their neighbours
// (short notes have fewer pages than bars).
func normalize(bins []float64) []byte {
	lastSeen := 0.0
	for i, v := range bins {
		if v == 0 {
			bins[i] = lastSeen
		} else {
			lastSeen = v
		}
	}
	lo, hi := bins[0], bins[0]
	for _, v := range bins {
		lo, hi = min(lo, v), max(hi, v)
	}
	out := make([]byte, len(bins))
	for i, v := range bins {
		if hi > lo {
			out[i] = byte(10 + 90*(v-lo)/(hi-lo))
		} else {
			out[i] = 50
		}
	}
	return out
}
