// Package audio plays the game's WAV music and effects. Output is 44.1 kHz
// stereo; there is one music track at a time plus any number of effects.
package audio

import (
	"encoding/binary"
	"io"
	"math"
	"sync"

	"github.com/ebitengine/oto/v3"
)

const rate = 44100

type Loader func(name string) ([]byte, error)

type Engine struct {
	ctx  *oto.Context
	load Loader

	mu     sync.Mutex
	music  float64 // master volumes
	effect float64
	bgm    *track
	shots  []*track
}

type track struct {
	name   string
	music  bool
	player *oto.Player
	s      *stream
}

func New(load Loader) (*Engine, error) {
	ctx, ready, err := oto.NewContext(&oto.NewContextOptions{
		SampleRate:   rate,
		ChannelCount: 2,
		Format:       oto.FormatFloat32LE,
	})
	if err != nil {
		return nil, err
	}
	<-ready
	return &Engine{ctx: ctx, load: load, music: 1, effect: 1}, nil
}

// curve maps a 0..1 slider to gain; loudness is closer to the square.
func curve(v float64) float64 { return v * v }

func (e *Engine) SetMaster(music, effect float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.music, e.effect = music, effect
	if e.bgm != nil && !e.bgm.s.stopping() {
		e.bgm.s.setGain(curve(music), 0.2)
	}
}

func (e *Engine) start(name string, music bool) *track {
	data, err := e.load(name)
	if err != nil {
		return nil
	}
	src, err := decodeWAV(data)
	if err != nil {
		return nil
	}
	s := &stream{src: src, loop: music}
	g := curve(e.effect)
	if music {
		g = curve(e.music)
	}
	s.gain, s.target = g, g
	if music {
		s.gain = 0
		s.setGain(g, 1.0)
	}
	t := &track{name: name, music: music, s: s, player: e.ctx.NewPlayer(s)}
	t.player.SetBufferSize(rate * 2 * 4 / 10)
	t.player.Play()
	return t
}

// SetMusic makes name the looping music, fading out what was playing.
// The same name keeps playing; "" just fades out.
func (e *Engine) SetMusic(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.bgm != nil && e.bgm.name == name && !e.bgm.s.stopping() {
		return
	}
	if e.bgm != nil {
		e.bgm.s.fadeOut(1.5)
		e.bgm = nil
	}
	if name != "" {
		e.bgm = e.start(name, true)
	}
}

func (e *Engine) PlayEffects(names []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	live := e.shots[:0]
	for _, t := range e.shots {
		if t.player.IsPlaying() {
			live = append(live, t)
		}
	}
	e.shots = live
	for _, n := range names {
		if t := e.start(n, false); t != nil {
			e.shots = append(e.shots, t)
		}
	}
}

// StopEffects cuts one-shot effects, used when jumping around.
func (e *Engine) StopEffects() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, t := range e.shots {
		t.s.fadeOut(0.1)
	}
	e.shots = nil
}

// stream feeds a decoded sound to oto with a gain ramp.
type stream struct {
	mu     sync.Mutex
	src    pcm
	loop   bool
	buf    []float32
	gain   float64
	target float64
	step   float64 // gain change per frame
	stop   bool
	done   bool
}

func (s *stream) setGain(g, seconds float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.target = g
	if seconds <= 0 {
		s.gain, s.step = g, 0
		return
	}
	s.step = math.Abs(g-s.gain) / (seconds * rate)
}

func (s *stream) fadeOut(seconds float64) {
	s.setGain(0, seconds)
	s.mu.Lock()
	s.stop = true
	s.mu.Unlock()
}

func (s *stream) stopping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stop
}

func (s *stream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return 0, io.EOF
	}
	frames := len(p) / 8
	if frames == 0 {
		return 0, nil
	}
	if cap(s.buf) < frames*2 {
		s.buf = make([]float32, frames*2)
	}
	buf := s.buf[:frames*2]
	got := 0
	for got < frames {
		n := s.src.read(buf[got*2:])
		if n == 0 {
			if s.loop && got > 0 || s.loop && !s.stop {
				s.src.rewind()
				if n = s.src.read(buf[got*2:]); n == 0 {
					break
				}
			} else {
				break
			}
		}
		got += n
	}
	for f := 0; f < got; f++ {
		if s.gain != s.target {
			if s.step == 0 {
				s.gain = s.target
			} else if s.gain < s.target {
				s.gain = math.Min(s.target, s.gain+s.step)
			} else {
				s.gain = math.Max(s.target, s.gain-s.step)
			}
		}
		g := float32(s.gain)
		binary.LittleEndian.PutUint32(p[f*8:], math.Float32bits(buf[f*2]*g))
		binary.LittleEndian.PutUint32(p[f*8+4:], math.Float32bits(buf[f*2+1]*g))
	}
	if s.stop && s.gain <= 0 || got == 0 {
		s.done = true
	}
	if got == 0 {
		return 0, io.EOF
	}
	return got * 8, nil
}
