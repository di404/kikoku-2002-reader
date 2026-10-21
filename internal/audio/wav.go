package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// pcm is a decoded sound read as stereo frames at the output rate.
type pcm interface {
	// read fills interleaved stereo samples and returns the frames written;
	// 0 means the end.
	read(out []float32) int
	rewind()
}

type wavFormat struct {
	tag, channels uint16
	rate          uint32
	blockAlign    uint16
	bits          uint16
	extra         []byte
}

// decodeWAV supports the two encodings the game uses: 16-bit PCM for
// effects and Microsoft ADPCM for music.
func decodeWAV(b []byte) (pcm, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	var f *wavFormat
	var data []byte
	for p := 12; p+8 <= len(b); {
		id := string(b[p : p+4])
		n := int(binary.LittleEndian.Uint32(b[p+4:]))
		body := b[p+8 : min(p+8+n, len(b))]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return nil, errors.New("short fmt chunk")
			}
			f = &wavFormat{
				tag:        binary.LittleEndian.Uint16(body),
				channels:   binary.LittleEndian.Uint16(body[2:]),
				rate:       binary.LittleEndian.Uint32(body[4:]),
				blockAlign: binary.LittleEndian.Uint16(body[12:]),
				bits:       binary.LittleEndian.Uint16(body[14:]),
			}
			if len(body) > 18 {
				f.extra = body[18:]
			}
		case "data":
			data = body
		}
		p += 8 + n + n&1
	}
	if f == nil || data == nil {
		return nil, errors.New("WAV without fmt or data")
	}
	if f.channels < 1 || f.channels > 2 {
		return nil, fmt.Errorf("%d channels", f.channels)
	}
	var src pcm
	switch {
	case f.tag == 1 && f.bits == 16:
		src = &pcm16{data: data, ch: int(f.channels)}
	case f.tag == 2:
		a, err := newADPCM(f, data)
		if err != nil {
			return nil, err
		}
		src = a
	default:
		return nil, fmt.Errorf("WAV format %d/%d bits not supported", f.tag, f.bits)
	}
	if f.rate != rate {
		src = &resample{src: src, step: float64(f.rate) / rate}
	}
	return src, nil
}

type pcm16 struct {
	data []byte
	ch   int
	pos  int
}

func (p *pcm16) read(out []float32) int {
	n := 0
	fb := 2 * p.ch
	for ; n*2+1 < len(out) && p.pos+fb <= len(p.data); n++ {
		l := float32(int16(binary.LittleEndian.Uint16(p.data[p.pos:]))) / 32768
		r := l
		if p.ch == 2 {
			r = float32(int16(binary.LittleEndian.Uint16(p.data[p.pos+2:]))) / 32768
		}
		out[n*2], out[n*2+1] = l, r
		p.pos += fb
	}
	return n
}

func (p *pcm16) rewind() { p.pos = 0 }

// adpcm decodes Microsoft ADPCM one block at a time.
type adpcm struct {
	data     []byte
	ch       int
	block    int
	perBlock int
	coef     [][2]int32
	pos      int       // next block offset
	buf      []float32 // decoded stereo frames of the current block
	at       int
}

var adaptTable = [16]int32{230, 230, 230, 230, 307, 409, 512, 614, 768, 614, 512, 409, 307, 230, 230, 230}

func newADPCM(f *wavFormat, data []byte) (*adpcm, error) {
	// extra (after cbSize): samples per block, coefficient count, pairs
	if len(f.extra) < 4 {
		return nil, errors.New("ADPCM without coefficients")
	}
	a := &adpcm{data: data, ch: int(f.channels), block: int(f.blockAlign)}
	a.perBlock = int(binary.LittleEndian.Uint16(f.extra))
	n := int(binary.LittleEndian.Uint16(f.extra[2:]))
	for i := 0; i < n && 4+i*4+4 <= len(f.extra); i++ {
		c1 := int32(int16(binary.LittleEndian.Uint16(f.extra[4+i*4:])))
		c2 := int32(int16(binary.LittleEndian.Uint16(f.extra[6+i*4:])))
		a.coef = append(a.coef, [2]int32{c1, c2})
	}
	if len(a.coef) == 0 || a.block < 7*a.ch {
		return nil, errors.New("bad ADPCM header")
	}
	a.buf = make([]float32, 0, a.perBlock*2)
	return a, nil
}

func (a *adpcm) rewind() { a.pos, a.at, a.buf = 0, 0, a.buf[:0] }

func (a *adpcm) read(out []float32) int {
	n := 0
	for n*2+1 < len(out) {
		if a.at >= len(a.buf) {
			if !a.decodeBlock() {
				break
			}
		}
		c := copy(out[n*2:], a.buf[a.at:])
		c &^= 1
		n += c / 2
		a.at += c
	}
	return n
}

func (a *adpcm) decodeBlock() bool {
	if a.pos+7*a.ch > len(a.data) {
		return false
	}
	b := a.data[a.pos:min(a.pos+a.block, len(a.data))]
	a.pos += a.block
	ch := a.ch
	var pred [2]int
	var delta, s1, s2 [2]int32
	for c := 0; c < ch; c++ {
		pred[c] = int(b[c])
		if pred[c] >= len(a.coef) {
			pred[c] = 0
		}
		delta[c] = int32(int16(binary.LittleEndian.Uint16(b[ch+2*c:])))
		s1[c] = int32(int16(binary.LittleEndian.Uint16(b[3*ch+2*c:])))
		s2[c] = int32(int16(binary.LittleEndian.Uint16(b[5*ch+2*c:])))
	}
	a.buf, a.at = a.buf[:0], 0
	put := func(v [2]int32) {
		l := float32(v[0]) / 32768
		r := l
		if ch == 2 {
			r = float32(v[1]) / 32768
		}
		a.buf = append(a.buf, l, r)
	}
	put(s2)
	put(s1)
	frames := 2
	var cur [2]int32
	c := 0
	nib := func(n int32) {
		co := a.coef[pred[c]]
		p := (s1[c]*co[0] + s2[c]*co[1]) >> 8
		sn := n
		if sn >= 8 {
			sn -= 16
		}
		v := p + sn*delta[c]
		v = max(-32768, min(32767, v))
		s2[c], s1[c] = s1[c], v
		delta[c] = adaptTable[n] * delta[c] >> 8
		if delta[c] < 16 {
			delta[c] = 16
		}
		cur[c] = v
		if c++; c == ch {
			c = 0
			if frames < a.perBlock {
				put(cur)
				frames++
			}
		}
	}
	for _, x := range b[7*ch:] {
		nib(int32(x >> 4))
		nib(int32(x & 15))
	}
	return true
}

// resample converts to the output rate by linear interpolation.
type resample struct {
	src    pcm
	step   float64
	t      float64
	a, b   [2]float32
	primed bool
	tmp    [2]float32
}

func (r *resample) rewind() { r.src.rewind(); r.t, r.primed = 0, false }

func (r *resample) pull() bool {
	r.a = r.b
	if r.src.read(r.tmp[:]) == 0 {
		return false
	}
	r.b = [2]float32{r.tmp[0], r.tmp[1]}
	return true
}

func (r *resample) read(out []float32) int {
	if !r.primed {
		if !r.pull() || !r.pull() {
			return 0
		}
		r.primed = true
	}
	n := 0
	for ; n*2+1 < len(out); n++ {
		for r.t >= 1 {
			if !r.pull() {
				return n
			}
			r.t--
		}
		f := float32(r.t)
		out[n*2] = r.a[0] + (r.b[0]-r.a[0])*f
		out[n*2+1] = r.a[1] + (r.b[1]-r.a[1])*f
		r.t += r.step
	}
	return n
}
