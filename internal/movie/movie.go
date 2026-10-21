// Package movie transcodes the game's MPEG-1 cutscenes to H.264 MP4 with
// ffmpeg so the web view can play them. Each file is converted once.
package movie

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Cache struct {
	dir string

	mu   sync.Mutex
	runs map[string]*run
}

type run struct {
	once sync.Once
	out  string
	err  error
}

func NewCache(dir string) *Cache { return &Cache{dir: dir, runs: map[string]*run{}} }

// ffmpeg finds ffmpeg also when the app is started from Finder, whose
// PATH lacks Homebrew.
func ffmpeg() (string, error) {
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("播放过场需要 ffmpeg（brew install ffmpeg）")
}

// Get returns the MP4 for the movie file in, converting on first use.
func (c *Cache) Get(in string) (string, error) {
	st, err := os.Stat(in)
	if err != nil {
		return "", err
	}
	h := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", in, st.Size(), st.ModTime().Unix())))
	out := filepath.Join(c.dir, hex.EncodeToString(h[:8])+".mp4")
	c.mu.Lock()
	r := c.runs[out]
	if r == nil {
		r = &run{out: out}
		c.runs[out] = r
	}
	c.mu.Unlock()
	r.once.Do(func() { r.err = convert(in, out) })
	if r.err != nil {
		c.mu.Lock()
		delete(c.runs, out) // try again next time
		c.mu.Unlock()
	}
	return r.out, r.err
}

func convert(in, out string) error {
	if _, err := os.Stat(out); err == nil {
		return nil
	}
	ff, err := ffmpeg()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	tmp := out + ".part.mp4"
	audio := []string{"-c:a", "aac", "-b:a", "160k", "-movflags", "+faststart", tmp}
	// x264 sizes the stream to the picture (the sources are 320x240 and
	// 640x480 MPEG-1); VideoToolbox is the fallback for builds without it.
	tries := [][]string{
		{"-c:v", "libx264", "-crf", "19", "-preset", "fast", "-pix_fmt", "yuv420p"},
		{"-c:v", "h264_videotoolbox", "-b:v", "2500k", "-pix_fmt", "yuv420p"},
	}
	var errs []string
	for _, v := range tries {
		args := append(append([]string{"-y", "-v", "error", "-i", in}, v...), audio...)
		b, err := exec.Command(ff, args...).CombinedOutput()
		if err == nil {
			return os.Rename(tmp, out)
		}
		errs = append(errs, fmt.Sprintf("%v %s", err, b))
	}
	os.Remove(tmp)
	return fmt.Errorf("ffmpeg: %s", strings.Join(errs, "; "))
}
