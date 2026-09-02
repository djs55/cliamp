package player

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"

	"github.com/gopxl/beep/v2"

	"github.com/bjarneo/cliamp/player/openmpt"
)

// testModuleBytes builds a minimal, valid ProTracker MOD file in memory —
// see player/openmpt/openmpt_test.go for the byte-layout comments.
func testModuleBytes(t *testing.T) []byte {
	t.Helper()

	buf := &bytes.Buffer{}
	title := make([]byte, 20)
	copy(title, "TESTMOD")
	buf.Write(title)

	const sampleWords = 200
	for i := 0; i < 31; i++ {
		ih := make([]byte, 30)
		if i == 0 {
			copy(ih[0:22], "smp1")
			binary.BigEndian.PutUint16(ih[22:24], uint16(sampleWords))
			ih[25] = 64
			binary.BigEndian.PutUint16(ih[28:30], 1)
		}
		buf.Write(ih)
	}

	buf.WriteByte(1)
	buf.WriteByte(127)
	buf.Write(make([]byte, 128))
	buf.WriteString("M.K.")

	pattern := make([]byte, 64*4*4)
	off := 0 // row 0, channel 0
	pattern[off+0] = byte(1&0xF0) | byte((428>>8)&0x0F)
	pattern[off+1] = byte(428 & 0xFF)
	pattern[off+2] = byte(1&0x0F) << 4
	off = (16*4 + 1) * 4 // row 16, channel 1
	pattern[off+0] = byte(1&0xF0) | byte((214>>8)&0x0F)
	pattern[off+1] = byte(214 & 0xFF)
	pattern[off+2] = byte(1&0x0F) << 4
	buf.Write(pattern)

	sample := make([]byte, sampleWords*2)
	for i := range sample {
		v := math.Sin(2 * math.Pi * float64(i) / 64.0)
		sample[i] = byte(int8(v * 100))
	}
	buf.Write(sample)

	return buf.Bytes()
}

type closeCountingReader struct {
	*bytes.Reader
	closed int
}

func (c *closeCountingReader) Close() error {
	c.closed++
	return nil
}

func TestSupportedExtsIncludesTrackerFormats(t *testing.T) {
	for _, ext := range []string{".mod", ".s3m", ".xm", ".it", ".mptm"} {
		if !SupportedExts[ext] {
			t.Errorf("SupportedExts[%q] = false, want true", ext)
		}
	}
}

func TestDecodeWithExtRoutesModuleFormats(t *testing.T) {
	if !openmpt.Available() {
		t.Skip("libopenmpt not installed in this environment")
	}

	data := testModuleBytes(t)
	for _, ext := range []string{".mod", ".s3m", ".xm", ".it"} {
		rc := &closeCountingReader{Reader: bytes.NewReader(data)}
		decoder, format, err := decodeWithExt(rc, ext, "test"+ext, 44100, 16)
		if err != nil {
			// Only .mod is actually a valid MOD file; the others are
			// expected to fail decoding (wrong format for the bytes) but
			// must still be *routed* to the openmpt decoder rather than
			// falling through to the mp3/wav/flac switch below it.
			if ext == ".mod" {
				t.Fatalf("decodeWithExt(%q): %v", ext, err)
			}
			continue
		}
		defer decoder.Close()
		if format.NumChannels != 2 {
			t.Errorf("decodeWithExt(%q): NumChannels = %d, want 2", ext, format.NumChannels)
		}
	}
}

func TestDecodeOpenmptStreamAndSeek(t *testing.T) {
	if !openmpt.Available() {
		t.Skip("libopenmpt not installed in this environment")
	}

	rc := &closeCountingReader{Reader: bytes.NewReader(testModuleBytes(t))}
	decoder, format, err := decodeOpenmpt(rc, beep.SampleRate(44100))
	if err != nil {
		t.Fatalf("decodeOpenmpt: %v", err)
	}
	defer decoder.Close()

	if rc.closed != 1 {
		t.Errorf("decodeOpenmpt closed the source reader %d times, want 1", rc.closed)
	}
	if format.SampleRate != 44100 || format.NumChannels != 2 {
		t.Fatalf("unexpected format: %+v", format)
	}
	if decoder.Len() <= 0 {
		t.Errorf("Len() = %d, want > 0", decoder.Len())
	}

	samples := make([][2]float64, 4096)
	total := 0
	sawNonZero := false
	for {
		n, ok := decoder.Stream(samples)
		for i := 0; i < n; i++ {
			if samples[i][0] != 0 || samples[i][1] != 0 {
				sawNonZero = true
			}
		}
		total += n
		if !ok {
			break
		}
	}
	if total == 0 {
		t.Fatal("Stream never produced any frames")
	}
	if !sawNonZero {
		t.Error("all streamed samples were silent")
	}
	if err := decoder.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}

	if err := decoder.Seek(0); err != nil {
		t.Fatalf("Seek(0): %v", err)
	}
	if decoder.Position() != 0 {
		t.Errorf("Position() after Seek(0) = %d, want 0", decoder.Position())
	}
	n, ok := decoder.Stream(samples)
	if n == 0 || !ok {
		t.Error("Stream after Seek(0) produced no frames")
	}

	if err := decoder.Seek(-1); err == nil {
		t.Error("Seek(-1): got nil error, want an error")
	}
}

func TestDecodeOpenmptRejectsCorruptFile(t *testing.T) {
	if !openmpt.Available() {
		t.Skip("libopenmpt not installed in this environment")
	}

	rc := &closeCountingReader{Reader: bytes.NewReader([]byte("not a module"))}
	if _, _, err := decodeOpenmpt(rc, beep.SampleRate(44100)); err == nil {
		t.Error("decodeOpenmpt on garbage bytes: got nil error, want an error")
	}
	if rc.closed != 1 {
		t.Errorf("decodeOpenmpt closed the source reader %d times, want 1", rc.closed)
	}
}

var _ io.ReadCloser = (*closeCountingReader)(nil)
