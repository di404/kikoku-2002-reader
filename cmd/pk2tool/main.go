// pk2tool lists or extracts an ARCV archive: pk2tool FILE [OUTDIR]
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"kikoku2002/internal/pk2"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pk2tool FILE [OUTDIR]")
		os.Exit(2)
	}
	a, err := pk2.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer a.Close()
	out := ""
	if len(os.Args) > 2 {
		out = os.Args[2]
		if err := os.MkdirAll(out, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	fmt.Printf("%s: %d entries\n", a.Name, len(a.Entries))
	for _, e := range a.Entries {
		if out == "" {
			fmt.Printf("%10d %s\n", e.Size, e.Name)
			continue
		}
		b, err := a.Read(e)
		if err != nil {
			fmt.Fprintln(os.Stderr, e.Name, err)
			continue
		}
		name := strings.ReplaceAll(e.Name, "\\", "_")
		if err := os.WriteFile(filepath.Join(out, name), b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
}
