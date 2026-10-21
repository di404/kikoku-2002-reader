// Package nps compiles the 2002 engine's .nps scripts into reading steps:
// one page of text with the picture and sound current when it is shown.
//
// A script is a run of HTML-like tags and text. <k> waits for a click,
// <clear>/<background>/<bustup>/<LAYER> build the picture, <bgm>/<se>
// drive sound, and <a href="..."> jumps to the next file. Text between
// tags is GBK (the Chinese patch); attribute values were left in the
// original Shift-JIS because they name resources in the Japanese archives.
//
// Only the final state of each page is kept: wipes become a fade of the
// given length, waits are dropped, and noise wipes are cuts.
package nps

import (
	"bytes"
	"fmt"
	"html"
	"math/rand"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	ScreenW = 800
	ScreenH = 600
)

// Resources is what the compiler needs to know about the archives.
type Resources interface {
	// Image returns the stored name and size of an image.
	Image(name string) (file string, w, h int, ok bool)
	// Sound returns the stored name of a sound.
	Sound(name string) (file string, ok bool)
	// Script returns a file of the Chinese script.
	Script(name string) ([]byte, error)
	// Original returns the same file of the Japanese script, or nil.
	Original(name string) []byte
}

type Layer struct {
	Kind    string  `json:"kind"` // image or color
	Src     string  `json:"src,omitempty"`
	Color   string  `json:"color,omitempty"`
	X       int     `json:"x"`
	Y       int     `json:"y"`
	W       int     `json:"w"`
	H       int     `json:"h"`
	Opacity float64 `json:"opacity"`
}

type Step struct {
	Scene  int      `json:"scene"`
	HTML   string   `json:"html"`
	Box    int      `json:"box"` // 0 narration, 1 centre, 2 left, 3 right speech
	Layers []Layer  `json:"layers"`
	Fade   int      `json:"fade"`            // ms for the picture change
	Flash  bool     `json:"flash,omitempty"` // white flash before the page
	Music  string   `json:"music,omitempty"` // looping music file
	SE     []string `json:"se,omitempty"`    // one-shots started with this page
	Movie  string   `json:"movie,omitempty"` // cutscene played before the page
}

type Scene struct {
	File  string `json:"file"`
	Title string `json:"title"`
	First int    `json:"first"`
}

type Book struct {
	Steps    []Step   `json:"-"`
	Scenes   []Scene  `json:"scenes"`
	Warnings []string `json:"-"`
}

// state is the picture and sound the script has built so far.
type state struct {
	bg     Layer
	left   *Layer
	right  *Layer
	centre *Layer
	over   *Layer
	music  string
	box    int
}

type compiler struct {
	res   Resources
	book  *Book
	st    state
	text  strings.Builder
	se    []string
	fade  int
	flash bool
	rng   *rand.Rand
	file  string
	warn  map[string]bool
	ref   []tag // resource tags of the Japanese file
	nth   int   // resource tags seen in the current file
}

// Compile follows the <a href> chain from first and compiles every file.
func Compile(res Resources, first string) (*Book, error) {
	c := &compiler{
		res:  res,
		book: &Book{},
		rng:  rand.New(rand.NewSource(1)),
		warn: map[string]bool{},
	}
	c.st.bg = color("#000000")
	seen := map[string]bool{}
	for name := first; name != ""; {
		if seen[name] {
			break
		}
		seen[name] = true
		src, err := res.Script(name + ".nps")
		if err != nil {
			if name == first {
				return nil, err
			}
			c.warnf("%s: %v", name, err)
			break
		}
		c.file = name
		c.book.Scenes = append(c.book.Scenes, Scene{File: name, Title: title(name), First: len(c.book.Steps)})
		name = c.run(src, res.Original(name+".nps"))
	}
	if len(c.book.Steps) == 0 {
		return nil, fmt.Errorf("%s: no text", first)
	}
	return c.book, nil
}

// title turns "脚本5_1" into "第五章（上）".
func title(file string) string {
	n, part, _ := strings.Cut(strings.TrimPrefix(file, "脚本"), "_")
	nums := []string{"", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
	k, err := strconv.Atoi(n)
	if err != nil || k >= len(nums) {
		return file
	}
	return "第" + nums[k] + "章" + map[string]string{"1": "（上）", "2": "（下）"}[part]
}

func (c *compiler) warnf(f string, a ...any) {
	m := fmt.Sprintf(f, a...)
	if !c.warn[m] {
		c.warn[m] = true
		c.book.Warnings = append(c.book.Warnings, m)
	}
}

// item is a piece of a script: text (GBK bytes) or a tag.
type item struct {
	text []byte
	tag  tag
}

// lex splits a script into text and tags. "<!-- ... >" comments out a
// tag, which may itself contain tags: "<!--<bgm src="M03">>".
func lex(src []byte) []item {
	var out []item
	for len(src) > 0 {
		i := bytes.IndexByte(src, '<') // never inside a GBK/SJIS character
		if i != 0 {
			if i < 0 {
				i = len(src)
			}
			out = append(out, item{text: src[:i]})
			src = src[i:]
			continue
		}
		depth, j := 0, 0
		for ; j < len(src); j++ {
			if src[j] == '<' {
				depth++
			} else if src[j] == '>' {
				if depth--; depth == 0 {
					break
				}
			}
		}
		if j == len(src) {
			break
		}
		body := src[1:j]
		src = src[j+1:]
		if !bytes.HasPrefix(body, []byte("!--")) {
			out = append(out, item{tag: parseTag(sjis(body))})
		}
	}
	return out
}

// named reports the attribute of t that names a resource.
func named(t tag) string {
	switch t.name {
	case "se", "bgm", "background", "layer":
		return "src"
	case "bustup":
		return "name"
	}
	return ""
}

// run compiles one file and returns the next file name, if any. ref is
// the Japanese original of the same file, used to repair resource names
// the Chinese patch garbled.
func (c *compiler) run(src, ref []byte) string {
	c.ref = c.ref[:0]
	for _, it := range lex(ref) {
		if it.tag.name != "" && named(it.tag) != "" {
			c.ref = append(c.ref, it.tag)
		}
	}
	c.nth = 0
	next := ""
	for _, it := range lex(src) {
		if it.text != nil {
			c.addText(it.text)
			continue
		}
		t := it.tag
		if named(t) != "" {
			c.repair(t)
			c.nth++
		}
		if href := c.tag(t); href != "" {
			next = href
		}
	}
	return next
}

// repair replaces garbled names in t with those of the matching tag in
// the Japanese script, found near the same position.
func (c *compiler) repair(t tag) {
	if c.exists(t) {
		return
	}
	for d := 0; d <= 4; d++ {
		for _, k := range []int{c.nth + d, c.nth - d} {
			if k < 0 || k >= len(c.ref) || c.ref[k].name != t.name {
				continue
			}
			r := c.ref[k]
			if !c.exists(r) {
				continue
			}
			for key := range t.attrs {
				if v, ok := r.attrs[key]; ok && key != "mode" && key != "option" && key != "align" {
					t.attrs[key] = v
				}
			}
			return
		}
	}
}

func (c *compiler) exists(t tag) bool {
	a := t.attrs
	switch t.name {
	case "se", "bgm":
		if strings.EqualFold(a["mode"], "off") || strings.EqualFold(a["mode"], "fadeout") {
			return true
		}
		if strings.EqualFold(a["option"], "random") {
			_, ok := c.res.Sound(a["src"] + "1")
			return ok
		}
		_, ok := c.res.Sound(a["src"])
		return ok
	case "background", "layer":
		if strings.HasPrefix(a["src"], "#") {
			return true
		}
		_, _, _, ok := c.res.Image(a["src"])
		return ok
	case "bustup":
		if strings.EqualFold(a["mode"], "off") {
			return true
		}
		_, _, _, ok := c.res.Image(a["name"] + "_" + a["face"])
		return ok
	}
	return true
}

func (c *compiler) addText(b []byte) {
	s, err := simplifiedchinese.GBK.NewDecoder().Bytes(b)
	if err != nil {
		c.warnf("%s: text not GBK", c.file)
		s = b
	}
	t := strings.NewReplacer("\r", "", "\n", "").Replace(string(s))
	c.text.WriteString(t)
}

type tag struct {
	name  string
	attrs map[string]string
}

func parseTag(s string) tag {
	s = strings.TrimSpace(s)
	f := strings.IndexAny(s, " \t")
	if f < 0 {
		f = len(s)
	}
	t := tag{name: strings.ToLower(s[:f]), attrs: map[string]string{}}
	rest := s[f:]
	for {
		rest = strings.TrimSpace(rest)
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(rest[:eq]))
		rest = strings.TrimSpace(rest[eq+1:])
		var val string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				end = len(rest) - 1
			}
			val, rest = rest[1:end+1], rest[min(end+2, len(rest)):]
		} else {
			end := strings.IndexAny(rest, " \t")
			if end < 0 {
				end = len(rest)
			}
			val, rest = rest[:end], rest[end:]
		}
		t.attrs[key] = strings.Trim(val, `"`)
	}
	return t
}

func sjis(b []byte) string {
	s, err := japanese.ShiftJIS.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(s)
}

func color(c string) Layer {
	return Layer{Kind: "color", Color: c, W: ScreenW, H: ScreenH, Opacity: 1}
}

// tag applies one tag; it returns the jump target for <a href>.
func (c *compiler) tag(t tag) string {
	a := t.attrs
	switch t.name {
	case "k":
		c.page()
	case "box":
		c.st.box, _ = strconv.Atoi(a["type"])
	case "clear":
		c.st.left, c.st.right, c.st.centre, c.st.over = nil, nil, nil, nil
	case "background":
		if l, ok := c.picture(a["src"], 100); ok {
			c.st.bg = l
		}
	case "layer":
		op, err := strconv.Atoi(a["transparent"])
		if err != nil {
			op = 100
		}
		if l, ok := c.picture(a["src"], op); ok {
			c.st.over = &l
		}
	case "bustup":
		c.bustup(a)
	case "wipe":
		ms := 0
		if a["effect"] != "noize" {
			n, _ := strconv.Atoi(a["time"])
			ms = n * 33
		}
		c.fade = max(c.fade, ms)
	case "whiteout":
		c.flash = true
	case "bgm":
		switch strings.ToLower(a["mode"]) {
		case "fadeout", "off":
			c.st.music = ""
		default:
			if f, ok := c.res.Sound(a["src"]); ok {
				c.st.music = f
			} else {
				c.warnf("%s: no music %q", c.file, a["src"])
			}
		}
	case "se":
		c.soundEffect(a)
	case "a":
		return a["href"]
	case "wait", "scroll":
	default:
		c.warnf("%s: unknown tag <%s>", c.file, t.name)
	}
	return ""
}

func (c *compiler) picture(src string, opacity int) (Layer, bool) {
	if strings.HasPrefix(src, "#") {
		l := color(src)
		l.Opacity = float64(opacity) / 100
		return l, true
	}
	f, w, h, ok := c.res.Image(src)
	if !ok {
		c.warnf("%s: no image %q", c.file, src)
		return Layer{}, false
	}
	return Layer{Kind: "image", Src: f, W: w, H: h, Opacity: float64(opacity) / 100}, true
}

// bustup shows a character on one side; the stored image is name_face.
// mode="off" takes the side down (the name is then a placeholder).
func (c *compiler) bustup(a map[string]string) {
	var slot **Layer
	switch a["align"] {
	case "left":
		slot = &c.st.left
	case "right":
		slot = &c.st.right
	default:
		slot = &c.st.centre
	}
	if strings.EqualFold(a["mode"], "off") {
		*slot = nil
		return
	}
	l, ok := c.picture(a["name"]+"_"+a["face"], 100)
	if !ok {
		return
	}
	l.Y = ScreenH - l.H
	switch a["align"] {
	case "left":
		l.X = 0
	case "right":
		l.X = ScreenW - l.W
	default:
		l.X = (ScreenW - l.W) / 2
	}
	// One figure per side: a new one on the left replaces a centred one.
	if a["align"] != "" {
		c.st.centre = nil
	}
	*slot = &l
}

// soundEffect queues a one-shot. option="random" names a family
// ("b_刀の打ち合い" for b_刀の打ち合い1..7) to pick from.
func (c *compiler) soundEffect(a map[string]string) {
	if strings.EqualFold(a["mode"], "off") {
		return
	}
	src := a["src"]
	if strings.EqualFold(a["option"], "random") {
		var pick []string
		for i := 1; i <= 9; i++ {
			if f, ok := c.res.Sound(src + strconv.Itoa(i)); ok {
				pick = append(pick, f)
			}
		}
		if len(pick) > 0 {
			c.se = append(c.se, pick[c.rng.Intn(len(pick))])
			return
		}
	}
	if f, ok := c.res.Sound(src); ok {
		c.se = append(c.se, f)
	} else {
		if src != "音名" { // placeholder left in the original
			c.warnf("%s: no sound %q", c.file, src)
		}
	}
}

// page ends the current text at <k>.
func (c *compiler) page() {
	text := strings.TrimSpace(c.text.String())
	c.text.Reset()
	if text == "" {
		return
	}
	layers := []Layer{c.st.bg}
	for _, l := range []*Layer{c.st.centre, c.st.left, c.st.right, c.st.over} {
		if l != nil {
			layers = append(layers, *l)
		}
	}
	c.book.Steps = append(c.book.Steps, Step{
		Scene:  len(c.book.Scenes) - 1,
		HTML:   html.EscapeString(text),
		Box:    c.st.box,
		Layers: layers,
		Fade:   c.fade,
		Flash:  c.flash,
		Music:  c.st.music,
		SE:     c.se,
	})
	c.se, c.fade, c.flash = nil, 0, false
}
