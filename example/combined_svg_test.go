package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"math"
	"strconv"
	"strings"
	"testing"
)

// TestCombinedSVGIsolatesClipsAndKeepsGeometry 验证多页合并隔离 ID/裁切引用，同时保留原几何属性和转义后的标题。
func TestCombinedSVGIsolatesClipsAndKeepsGeometry(t *testing.T) {
	one := []byte(`<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="100" height="50" viewBox="10 20 100 50"><defs><clipPath id="c"><rect x="10" y="20" width="10" height="10"/></clipPath><path id="glyph" d="M1 2L3 4"/></defs><g clip-path="url(#c)"><use xlink:href="#glyph"/></g></svg>`)
	merged, err := combineSVG([]svgSheet{{Title: "A & B", Data: one}, {Title: "C", Data: one}})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	refs := []string{}
	roots := 0
	paths := 0
	d := xml.NewDecoder(bytes.NewReader(merged))
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
		if e.Name.Local == "svg" {
			roots++
		}
		for _, a := range e.Attr {
			switch a.Name.Local {
			case "id":
				if ids[a.Value] {
					t.Fatal("colliding id", a.Value)
				}
				ids[a.Value] = true
			case "href":
				refs = append(refs, strings.TrimPrefix(a.Value, "#"))
			case "clip-path":
				refs = append(refs, strings.TrimSuffix(strings.TrimPrefix(a.Value, "url(#"), ")"))
			case "d":
				if a.Value != "M1 2L3 4" {
					t.Fatal("geometry changed")
				}
				paths++
			}
		}
	}
	if roots != 3 || paths != 2 {
		t.Fatal("page or geometry lost", roots, paths)
	}
	for _, r := range refs {
		if !ids[r] {
			t.Fatal("broken reference", r)
		}
	}
	if !bytes.Contains(merged, []byte("A &amp; B")) {
		t.Fatal("title escaping lost")
	}
}

// TestCombinedPagesKeepRelativePaperSizeWithoutAddedLabels 验证普通合并保留不同纸张的相对尺寸，并且不添加原图以外的标题。
func TestCombinedPagesKeepRelativePaperSizeWithoutAddedLabels(t *testing.T) {
	a := []byte(`<svg viewBox="0 0 420 297"><path d="M0 0L420 297"/></svg>`)
	b := []byte(`<svg viewBox="0 0 210 297"><path d="M0 0L210 297"/></svg>`)
	result, err := combineSVG([]svgSheet{{Title: "main", Data: a}, {Title: "part", Data: b}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(result, []byte("<text")) {
		t.Error("merge adds text outside source drawings")
	}
	d := xml.NewDecoder(bytes.NewReader(result))
	var widths []float64
	for {
		v, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		s, ok := v.(xml.StartElement)
		if !ok || s.Name.Local != "svg" {
			continue
		}
		for _, a := range s.Attr {
			if a.Name.Local == "width" {
				w, _ := strconv.ParseFloat(a.Value, 64)
				widths = append(widths, w)
			}
		}
	}
	if len(widths) != 3 || math.Abs(widths[1]/widths[2]-2) > .001 {
		t.Errorf("A3 landscape / A4 portrait relative widths lost: %v", widths)
	}
}

type testLayoutFields map[string]any

// Field 返回测试布局的指定字段，用于实现纸张信息读取接口。
func (m testLayoutFields) Field(k string) any { return m[k] }

// TestPaperFootprintValidatesLimitsBeforeCropping 验证纸张边界须与打印尺寸一致，并拒绝不匹配或非有限的裁切范围。
func TestPaperFootprintValidatesLimitsBeforeCropping(t *testing.T) {
	fields := testLayoutFields{"LIMMIN": []float64{-7.5, -20}, "LIMMAX": []float64{202.5, 277}, "plotsettings.plot_paper_unit": int64(1), "plotsettings.plot_type": int64(5), "plotsettings.plot_rotation_mode": int64(0), "plotsettings.paper_width": 210.0, "plotsettings.paper_height": 297.0, "plotsettings.paper_units": 1.0, "plotsettings.drawing_units": 1.0}
	b, mm, ok := paperFootprint(fields)
	if !ok || b.MinX != -7.5 || mm != [2]float64{210, 297} {
		t.Fatal("paper limits lost", b, mm, ok)
	}
	fields["LIMMAX"] = []float64{400, 277}
	if _, _, ok := paperFootprint(fields); ok {
		t.Fatal("user limits must not silently crop a mismatching paper")
	}
	fields["LIMMAX"] = []float64{412.5, 277}
	fields["plotsettings.paper_width"] = 297.0
	fields["plotsettings.paper_height"] = 420.0
	fields["plotsettings.plot_rotation_mode"] = int64(1)
	_, mm, ok = paperFootprint(fields)
	if !ok || mm != [2]float64{420, 297} {
		t.Fatal("landscape orientation lost", mm, ok)
	}
	fields["LIMMIN"] = []float64{math.NaN(), -20}
	if _, _, ok := paperFootprint(fields); ok {
		t.Fatal("nonfinite paper bounds accepted")
	}

}

// TestLegacyOverviewStillHasEqualWidthCards 验证对照总览继续使用等宽卡片，避免普通纸张合并规则改变既有总览。
func TestLegacyOverviewStillHasEqualWidthCards(t *testing.T) {
	a := []byte(`<svg viewBox="0 0 420 297"/>`)
	b := []byte(`<svg viewBox="0 0 210 297"/>`)
	got, err := combineOverviewSVG([]svgSheet{{Title: "A", Data: a}, {Title: "B", Data: b}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(got, []byte(`width="2000.000000"`)) != 2 || !bytes.Contains(got, []byte(`<text`)) || !bytes.Contains(got, []byte(`width="2048.000000"`)) {
		t.Fatal("legacy comparison overview changed")
	}
}
