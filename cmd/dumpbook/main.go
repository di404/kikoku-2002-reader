// dumpbook prints the compiled reading steps: dumpbook [GAMEDIR] [FROM [N]]
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"kikoku2002/internal/game"
	"kikoku2002/internal/nps"
)

func main() {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, "Games", "kikoku-2002")
	args := os.Args[1:]
	if len(args) > 0 {
		if _, err := strconv.Atoi(args[0]); err != nil {
			dir, args = args[0], args[1:]
		}
	}
	from, n := 0, 40
	if len(args) > 0 {
		from, _ = strconv.Atoi(args[0])
	}
	if len(args) > 1 {
		n, _ = strconv.Atoi(args[1])
	}
	s, err := game.Open(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b, err := nps.Compile(s, "脚本1")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, w := range b.Warnings {
		fmt.Println("warning:", w)
	}
	for _, sc := range b.Scenes {
		fmt.Printf("scene %s %s @%d\n", sc.File, sc.Title, sc.First)
	}
	fmt.Println("steps:", len(b.Steps))
	for i := from; i < len(b.Steps) && i < from+n; i++ {
		st := b.Steps[i]
		fmt.Printf("#%d box%d fade%d flash=%v music=%q se=%q\n", i, st.Box, st.Fade, st.Flash, st.Music, st.SE)
		for _, l := range st.Layers {
			fmt.Printf("    %s %s%s %d,%d %dx%d %.2f\n", l.Kind, l.Src, l.Color, l.X, l.Y, l.W, l.H, l.Opacity)
		}
		fmt.Println("   ", st.HTML)
	}
}
