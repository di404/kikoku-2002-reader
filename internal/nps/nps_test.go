package nps

import "testing"

func TestTitle(t *testing.T) {
	for in, want := range map[string]string{"脚本1": "第一章", "脚本2_2": "第二章（下）", "脚本5_1": "第五章（上）"} {
		if got := title(in); got != want {
			t.Errorf("title(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseTag(t *testing.T) {
	g := parseTag(`bustup name="st a" face="b" align=left`)
	if g.name != "bustup" || g.attrs["name"] != "st a" || g.attrs["face"] != "b" || g.attrs["align"] != "left" {
		t.Errorf("%+v", g)
	}
	g = parseTag(`BOX TYPE=3`)
	if g.name != "box" || g.attrs["type"] != "3" {
		t.Errorf("%+v", g)
	}
}
