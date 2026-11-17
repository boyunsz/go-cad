package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"testing"
)

// TestClarityPreservesVectorGeometry 验证显示增强保留矢量几何数量、坐标、变换和已有属性。
func TestClarityPreservesVectorGeometry(t *testing.T) {
	b := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 50"><defs><g id="glyph-0-0"><path d="M1 2L3 4"/></g></defs><path d="M0 0L10 10" transform="matrix(0.14 0 0 -0.14 0 50)" stroke="#000" stroke-width="1.7717" stroke-dasharray="4 2"/><use href="#glyph-0-0" x="20" y="30"/></svg>`)
	out, s, err := enhanceReviewSVG(b, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if s.Paths != 2 || s.AdjustedStrokes != 1 || s.ReinforcedGlyphs != 1 {
		t.Fatal(s)
	}
	read := func(data []byte) []map[string]string {
		var list []map[string]string
		d := xml.NewDecoder(bytes.NewReader(data))
		for {
			tok, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			e, ok := tok.(xml.StartElement)
			if !ok {
				continue
			}
			if e.Name.Local != "path" && e.Name.Local != "use" {
				continue
			}
			m := map[string]string{}
			for _, a := range e.Attr {
				switch a.Name.Local {
				case "d", "transform", "href", "x", "y", "stroke-dasharray":
					m[a.Name.Local] = a.Value
				}
			}
			list = append(list, m)
		}
		return list
	}
	a, c := read(b), read(out)
	if len(a) != len(c) {
		t.Fatal("geometry count changed")
	}
	for i := range a {
		if len(a[i]) != len(c[i]) {
			t.Fatal("attributes lost")
		}
		for k, v := range a[i] {
			if c[i][k] != v {
				t.Fatalf("geometry changed: %s", k)
			}
		}
	}
}

// TestClarityRejectsInvalidView 验证显示增强拒绝无效的 SVG 视口输入。
func TestClarityRejectsInvalidView(t *testing.T) {
	for _, s := range []string{"", `<g/>`, `<svg viewBox="0 0 NaN 10"/>`, `<svg viewBox="0 0 100 0"/>`, `<svg viewBox="0 0 100 100">`} {
		if _, _, err := enhanceReviewSVG([]byte(s), 4096); err == nil {
			t.Fatalf("accepted invalid input %q", s)
		}
	}
}
