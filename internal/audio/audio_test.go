package audio

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"kikoku2002/internal/game"
)

// TestDecodeGame decodes the game's music and an effect when the game is
// installed in ~/Games/kikoku-2002.
func TestDecodeGame(t *testing.T) {
	home, _ := os.UserHomeDir()
	s, err := game.Open(filepath.Join(home, "Games", "kikoku-2002"))
	if err != nil {
		t.Skip(err)
	}
	defer s.Close()
	for _, name := range []string{"m06.wav", "cyborg_00_arm01.wav", "コン跳躍.wav"} {
		b, err := s.ReadSound(name)
		if err != nil {
			t.Fatal(err)
		}
		src, err := decodeWAV(b)
		if err != nil {
			t.Fatal(name, err)
		}
		buf := make([]float32, 8192)
		frames, peak, sum := 0, 0.0, 0.0
		for {
			n := src.read(buf)
			if n == 0 {
				break
			}
			for _, v := range buf[:n*2] {
				peak = math.Max(peak, math.Abs(float64(v)))
				sum += float64(v) * float64(v)
			}
			frames += n
		}
		rms := math.Sqrt(sum / float64(frames*2))
		t.Logf("%s: %.1fs peak %.2f rms %.3f", name, float64(frames)/rate, peak, rms)
		if frames < rate/10 || peak > 1.01 || rms < 0.005 {
			t.Errorf("%s: implausible decode", name)
		}
	}
}
