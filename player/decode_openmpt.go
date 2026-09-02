package player

import (
	"errors"
	"fmt"
	"io"

	"github.com/gopxl/beep/v2"

	"github.com/bjarneo/cliamp/player/openmpt"
)

// decodeOpenmpt decodes a tracker module file (.mod, .s3m, .xm, .it, ...)
// via libopenmpt. libopenmpt renders directly at the requested sample
// rate, so no separate resampling stage is needed.
func decodeOpenmpt(rc io.ReadCloser, sr beep.SampleRate) (beep.StreamSeekCloser, beep.Format, error) {
	defer rc.Close()

	if !openmpt.Available() {
		return nil, beep.Format{}, fmt.Errorf("libopenmpt is required to play tracker module files (.mod/.s3m/.xm/.it) — install it with your package manager (e.g. apt install libopenmpt0, brew install libopenmpt, pacman -S libopenmpt)")
	}

	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, beep.Format{}, fmt.Errorf("read module: %w", err)
	}

	mod, err := openmpt.Open(data)
	if err != nil {
		return nil, beep.Format{}, err
	}

	format := beep.Format{SampleRate: sr, NumChannels: 2, Precision: 2}
	length := int(mod.DurationSeconds() * float64(sr))
	return &openmptStreamer{mod: mod, sampleRate: int(sr), length: length}, format, nil
}

// openmptStreamer adapts an *openmpt.Module to beep.StreamSeekCloser.
type openmptStreamer struct {
	mod        *openmpt.Module
	sampleRate int
	buf        []float32
	pos        int
	length     int // total frames; 0 if libopenmpt couldn't estimate a duration
	drained    bool
}

func (s *openmptStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	if s.drained {
		return 0, false
	}

	need := len(samples) * 2
	if cap(s.buf) < need {
		s.buf = make([]float32, need)
	}
	buf := s.buf[:need]

	got := s.mod.ReadInterleavedFloatStereo(s.sampleRate, buf)
	if got == 0 {
		s.drained = true
		return 0, false
	}

	for i := 0; i < got; i++ {
		samples[i][0] = float64(buf[i*2])
		samples[i][1] = float64(buf[i*2+1])
	}
	s.pos += got
	return got, true
}

func (s *openmptStreamer) Err() error { return nil }

func (s *openmptStreamer) Len() int { return s.length }

func (s *openmptStreamer) Position() int { return s.pos }

func (s *openmptStreamer) Seek(p int) error {
	if p < 0 {
		return errors.New("openmpt: negative seek position")
	}
	actual := s.mod.SetPositionSeconds(float64(p) / float64(s.sampleRate))
	s.pos = int(actual * float64(s.sampleRate))
	s.drained = false
	return nil
}

func (s *openmptStreamer) Close() error {
	s.mod.Close()
	return nil
}
