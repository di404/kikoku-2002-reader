// Package game opens a 2002 Kikokugai folder: the Chinese script
// (script.chs), IMAGE.PK2, SOUND.PK2 and the movie folder.
package game

import (
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"kikoku2002/internal/pk2"
)

type Store struct {
	Dir    string
	script *pk2.Archive
	orig   *pk2.Archive // Japanese script, optional
	image  *pk2.Archive
	sound  *pk2.Archive

	mu    sync.Mutex
	sizes map[string][2]int
}

// Open checks that dir holds the patched game and opens its archives.
func Open(dir string) (*Store, error) {
	s := &Store{Dir: dir, sizes: map[string][2]int{}}
	var err error
	if s.script, err = pk2.Open(filepath.Join(dir, "script.chs")); err != nil {
		return nil, missing(dir, "script.chs（汉化剧本）", err)
	}
	s.orig, _ = pk2.Open(filepath.Join(dir, "SCRIPT.PK2"))
	if s.image, err = pk2.Open(filepath.Join(dir, "IMAGE.PK2")); err != nil {
		s.Close()
		return nil, missing(dir, "IMAGE.PK2", err)
	}
	if s.sound, err = pk2.Open(filepath.Join(dir, "SOUND.PK2")); err != nil {
		s.Close()
		return nil, missing(dir, "SOUND.PK2", err)
	}
	return s, nil
}

func missing(dir, what string, err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s 里没有 %s", dir, what)
	}
	return err
}

func (s *Store) Close() {
	for _, a := range []*pk2.Archive{s.script, s.orig, s.image, s.sound} {
		if a != nil {
			a.Close()
		}
	}
}

func (s *Store) Script(name string) ([]byte, error) { return s.script.ReadFile(name) }

func (s *Store) Original(name string) []byte {
	if s.orig == nil {
		return nil
	}
	b, _ := s.orig.ReadFile(name)
	return b
}

func (s *Store) Image(name string) (string, int, int, bool) {
	e, ok := s.image.Find(name)
	if !ok {
		return "", 0, 0, false
	}
	s.mu.Lock()
	sz, ok := s.sizes[e.Name]
	s.mu.Unlock()
	if !ok {
		b, err := s.image.Read(e)
		if err != nil {
			return "", 0, 0, false
		}
		cfg, _, err := image.DecodeConfig(strings.NewReader(string(b)))
		if err != nil {
			return "", 0, 0, false
		}
		sz = [2]int{cfg.Width, cfg.Height}
		s.mu.Lock()
		s.sizes[e.Name] = sz
		s.mu.Unlock()
	}
	return e.Name, sz[0], sz[1], true
}

func (s *Store) Sound(name string) (string, bool) {
	e, ok := s.sound.Find(name)
	return e.Name, ok
}

func (s *Store) ReadImage(name string) ([]byte, error) { return s.image.ReadFile(name) }
func (s *Store) ReadSound(name string) ([]byte, error) { return s.sound.ReadFile(name) }

// Movies lists the cutscenes in movie/, sorted by name.
func (s *Store) Movies() []string {
	m, _ := filepath.Glob(filepath.Join(s.Dir, "movie", "*.mpg"))
	for i := range m {
		m[i] = filepath.Base(m[i])
	}
	sort.Strings(m)
	return m
}

// MoviePath returns the file for a name from Movies.
func (s *Store) MoviePath(name string) (string, error) {
	if name != filepath.Base(name) {
		return "", errors.New("bad movie name")
	}
	p := filepath.Join(s.Dir, "movie", name)
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	_, err = f.Read(make([]byte, 4))
	f.Close()
	if err != nil && err != io.EOF {
		return "", err
	}
	return p, nil
}
