package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/text/unicode/norm"

	"kikoku2002/internal/audio"
	"kikoku2002/internal/game"
	"kikoku2002/internal/movie"
	"kikoku2002/internal/nps"
)

const firstScript = "脚本1"

type Settings struct {
	GameDir    string  `json:"gameDir"`
	FontFamily string  `json:"fontFamily"`
	FontFile   string  `json:"fontFile"`
	FontScale  float64 `json:"fontScale"`
	LineHeight float64 `json:"lineHeight"`
	BoxOpacity float64 `json:"boxOpacity"`
	Music      float64 `json:"music"`
	Effect     float64 `json:"effect"`
	Last       int     `json:"last"`
}

func defaultSettings() Settings {
	home, _ := os.UserHomeDir()
	return Settings{
		GameDir:    filepath.Join(home, "Games", "kikoku-2002"),
		FontFamily: "Songti SC",
		FontScale:  1,
		LineHeight: 1.8,
		BoxOpacity: 0.72,
		Music:      0.8,
		Effect:     0.8,
	}
}

type App struct {
	ctx context.Context

	mu       sync.Mutex
	settings Settings
	store    *game.Store
	book     *nps.Book
	audio    *audio.Engine
	movies   *movie.Cache
	loadErr  error
	saveTick *time.Timer
}

func NewApp() *App { return &App{settings: defaultSettings()} }

func configDir() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "kikoku2002-reader")
}

func cacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "kikoku2002-reader")
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if b, err := os.ReadFile(filepath.Join(configDir(), "settings.json")); err == nil {
		s := defaultSettings()
		if json.Unmarshal(b, &s) == nil {
			a.settings = s
		}
	}
	a.movies = movie.NewCache(filepath.Join(cacheDir(), "movies"))
	a.loadErr = a.load(a.settings.GameDir)
}

// load opens the game folder and compiles the story.
func (a *App) load(dir string) error {
	store, err := game.Open(dir)
	if err != nil {
		return err
	}
	book, err := nps.Compile(store, firstScript)
	if err != nil {
		store.Close()
		return err
	}
	for _, w := range book.Warnings {
		log.Println("script:", w)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store != nil {
		a.store.Close()
	}
	a.store, a.book = store, book
	if a.audio == nil {
		if a.audio, err = audio.New(a.readSound); err != nil {
			log.Println("audio:", err)
			a.audio = nil
		}
	}
	if a.audio != nil {
		a.audio.SetMaster(a.settings.Music, a.settings.Effect)
	}
	return nil
}

func (a *App) readSound(name string) ([]byte, error) {
	a.mu.Lock()
	s := a.store
	a.mu.Unlock()
	if s == nil {
		return nil, errors.New("no game data")
	}
	return s.ReadSound(name)
}

// domReady sizes the window to about 1440 pixels tall, within the screen.
func (a *App) domReady(ctx context.Context) {
	screens, err := runtime.ScreenGetAll(ctx)
	if err != nil || len(screens) == 0 {
		return
	}
	sc := screens[0]
	for _, s := range screens {
		if s.IsCurrent {
			sc = s
		}
	}
	h := sc.Size.Height * 88 / 100
	scale := 1
	if sc.PhysicalSize.Height > sc.Size.Height && sc.Size.Height > 0 {
		scale = sc.PhysicalSize.Height / sc.Size.Height
	}
	if h*scale > 1440 {
		h = 1440 / scale
	}
	w := h * 4 / 3
	if w > sc.Size.Width*95/100 {
		w = sc.Size.Width * 95 / 100
		h = w * 3 / 4
	}
	runtime.WindowSetSize(ctx, w, h)
	runtime.WindowCenter(ctx)
}

func (a *App) shutdown(ctx context.Context) { a.saveNow() }

type Movie struct {
	File  string `json:"file"`
	Title string `json:"title"`
}

type InitResult struct {
	Error    string       `json:"error,omitempty"`
	Scenes   []nps.Scene  `json:"scenes"`
	Total    int          `json:"total"`
	Movies   []Movie      `json:"movies"`
	Opening  string       `json:"opening,omitempty"` // played before the first page
	Settings Settings     `json:"settings"`
	Fonts    []FontChoice `json:"fonts"`
}

type FontChoice struct {
	Label  string `json:"label"`
	Family string `json:"family"`
}

var presetFonts = []FontChoice{
	{"宋体（Songti SC）", "Songti SC"},
	{"苹方（PingFang SC）", "PingFang SC"},
	{"楷体（Kaiti SC）", "Kaiti SC"},
	{"仿宋（STFangsong）", "STFangsong"},
	{"冬青黑体（Hiragino Sans GB）", "Hiragino Sans GB"},
	{"华文黑体（STHeiti）", "STHeiti"},
	{"兰亭黑（Lantinghei SC）", "Lantinghei SC"},
}

// movieTitle names the three files that ship with the game. File names
// from macOS volumes come decomposed (テ + ゛), so compare in NFC.
func movieTitle(file string) string {
	file = norm.NFC.String(file)
	switch {
	case strings.Contains(file, "デモ_L_640"):
		return "开场动画"
	case strings.Contains(file, "デモ_L_320"):
		return "开场动画（小）"
	case strings.Contains(file, "座談会"):
		return "千代恋座谈会"
	}
	return strings.TrimSuffix(file, path.Ext(file))
}

func (a *App) Init() InitResult {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := InitResult{Settings: a.settings, Fonts: presetFonts}
	if a.loadErr != nil {
		r.Error = a.loadErr.Error()
		return r
	}
	r.Scenes = a.book.Scenes
	r.Total = len(a.book.Steps)
	files := a.store.Movies()
	for _, f := range files {
		t := movieTitle(f)
		if t == "开场动画（小）" && len(files) > 2 {
			continue // the same opening at 320x240
		}
		r.Movies = append(r.Movies, Movie{File: f, Title: t})
		if t == "开场动画" {
			r.Opening = f
		}
	}
	return r
}

// ChooseGameDir lets the player point at the game folder.
func (a *App) ChooseGameDir() InitResult {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择《鬼哭街》2002 原版目录（含 script.chs、IMAGE.PK2、SOUND.PK2 的文件夹）",
	})
	if err != nil || dir == "" {
		return a.Init()
	}
	err = a.load(dir)
	a.mu.Lock()
	a.loadErr = err
	if err == nil {
		a.settings.GameDir = dir
		a.settings.Last = 0
	}
	a.mu.Unlock()
	a.saveSoon()
	return a.Init()
}

type View struct {
	Index int      `json:"index"`
	Step  nps.Step `json:"step"`
	Next  []string `json:"next"` // images of the next steps, for preloading
}

// Show returns step i for display and starts its sound. jumped means the
// reader did not arrive by reading on (menu, back, resume): effects are
// skipped then.
func (a *App) Show(i int, jumped bool) (*View, error) {
	a.mu.Lock()
	book := a.book
	eng := a.audio
	a.mu.Unlock()
	if book == nil {
		return nil, errors.New("game not loaded")
	}
	if i < 0 || i >= len(book.Steps) {
		return nil, fmt.Errorf("step %d out of range", i)
	}
	st := book.Steps[i]
	v := &View{Index: i, Step: st}
	for k := i + 1; k < len(book.Steps) && k <= i+3; k++ {
		for _, l := range book.Steps[k].Layers {
			if l.Kind == "image" {
				v.Next = append(v.Next, l.Src)
			}
		}
	}
	if eng != nil {
		if jumped {
			eng.StopEffects()
		}
		eng.SetMusic(st.Music)
		if !jumped {
			eng.PlayEffects(st.SE)
		}
	}
	a.mu.Lock()
	a.settings.Last = i
	a.mu.Unlock()
	a.saveSoon()
	return v, nil
}

// Silence stops all sound while a movie plays.
func (a *App) Silence() {
	if a.audio != nil {
		a.audio.SetMusic("")
		a.audio.StopEffects()
	}
}

func (a *App) SaveSettings(s Settings) Settings {
	a.mu.Lock()
	s.GameDir = a.settings.GameDir
	s.Last = a.settings.Last
	a.settings = s
	eng := a.audio
	a.mu.Unlock()
	if eng != nil {
		eng.SetMaster(s.Music, s.Effect)
	}
	a.saveSoon()
	return s
}

// ChooseFontFile picks a .ttf/.otf/.ttc the page loads as "UserFont".
func (a *App) ChooseFontFile() string {
	p, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "选择字体文件",
		Filters: []runtime.FileFilter{{DisplayName: "字体 (*.ttf;*.otf;*.ttc)", Pattern: "*.ttf;*.otf;*.ttc;*.TTF;*.OTF;*.TTC"}},
	})
	if err != nil || p == "" {
		return ""
	}
	a.mu.Lock()
	a.settings.FontFile = p
	a.mu.Unlock()
	a.saveSoon()
	return p
}

func (a *App) ToggleFullscreen() {
	if runtime.WindowIsFullscreen(a.ctx) {
		runtime.WindowUnfullscreen(a.ctx)
	} else {
		runtime.WindowFullscreen(a.ctx)
	}
}

func (a *App) Quit() { runtime.Quit(a.ctx) }

func (a *App) saveSoon() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.saveTick != nil {
		a.saveTick.Stop()
	}
	a.saveTick = time.AfterFunc(800*time.Millisecond, a.saveNow)
}

func (a *App) saveNow() {
	a.mu.Lock()
	b, _ := json.MarshalIndent(a.settings, "", "  ")
	a.mu.Unlock()
	if err := os.MkdirAll(configDir(), 0o755); err == nil {
		_ = os.WriteFile(filepath.Join(configDir(), "settings.json"), b, 0o644)
	}
}

// ServeHTTP serves game resources to the page:
//
//	/img/<name>        images from IMAGE.PK2
//	/font/user         the font file chosen in settings
//	/movie/<file>.mp4  cutscenes, transcoded once with ffmpeg
type assetHandler struct{ a *App }

func (h assetHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a := h.a
	p := r.URL.Path
	a.mu.Lock()
	store, font, mc := a.store, a.settings.FontFile, a.movies
	a.mu.Unlock()
	switch {
	case strings.HasPrefix(p, "/img/") && store != nil:
		data, err := store.ReadImage(strings.TrimPrefix(p, "/img/"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", contentType(p))
		w.Header().Set("Cache-Control", "max-age=86400")
		_, _ = w.Write(data)
	case p == "/font/user" && font != "":
		w.Header().Set("Content-Type", contentType(font))
		http.ServeFile(w, r, font)
	case strings.HasPrefix(p, "/movie/") && store != nil:
		in, err := store.MoviePath(strings.TrimSuffix(strings.TrimPrefix(p, "/movie/"), ".mp4"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		out, err := mc.Get(in)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		http.ServeFile(w, r, out)
	default:
		http.NotFound(w, r)
	}
}

func contentType(p string) string {
	ext := strings.ToLower(path.Ext(p))
	if ext == ".ttc" {
		return "font/collection"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}
