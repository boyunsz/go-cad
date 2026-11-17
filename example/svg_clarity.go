package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type clarityStats struct {
	Paths, AdjustedStrokes, ReinforcedGlyphs int
}

// enhanceReviewSVG is an explicit display treatment for pdftocairo's vector
// output. It never changes path coordinates, text advances, transforms or dash
// spacing. It is not a DWG decoder and cannot recover source CAD objects.
func enhanceReviewSVG(data []byte, width int) ([]byte, clarityStats, error) {
	var out bytes.Buffer
	var stats clarityStats
	d := xml.NewDecoder(bytes.NewReader(data))
	e := xml.NewEncoder(&out)
	depth := 0
	rootSeen := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, stats, err
		}
		switch v := tok.(type) {
		case xml.ProcInst, xml.Directive:
			continue
		case xml.StartElement:
			if depth == 0 && (rootSeen || v.Name.Local != "svg") {
				return nil, stats, fmt.Errorf("expected one SVG root")
			}
			v.Name.Space = ""
			attrs := make([]xml.Attr, 0, len(v.Attr)+4)
			glyph := false
			view := ""
			for _, a := range v.Attr {
				if a.Name.Local == "xmlns" || a.Name.Space == "xmlns" {
					continue
				}
				a.Name.Space = ""
				if depth == 0 && (a.Name.Local == "width" || a.Name.Local == "height") {
					continue
				}
				if a.Name.Local == "viewBox" {
					view = a.Value
				}
				if v.Name.Local == "g" && a.Name.Local == "id" && strings.HasPrefix(a.Value, "glyph-") {
					glyph = true
				}
				if v.Name.Local == "path" && a.Name.Local == "stroke-width" {
					w, err := strconv.ParseFloat(a.Value, 64)
					if err == nil && w > 0 && w < 4 {
						a.Value = strconv.FormatFloat(w*1.12, 'f', 6, 64)
						stats.AdjustedStrokes++
					}
				}
				attrs = append(attrs, a)
			}
			attr := func(k, val string) { attrs = append(attrs, xml.Attr{Name: xml.Name{Local: k}, Value: val}) }
			if v.Name.Local == "path" {
				stats.Paths++
			}
			if glyph {
				// Small optical weight increase without replacing the embedded glyph.
				attr("stroke", "#000")
				attr("stroke-width", "0.06")
				attr("stroke-linejoin", "round")
				attr("paint-order", "stroke fill")
				stats.ReinforcedGlyphs++
			}
			if depth == 0 {
				fields := strings.Fields(view)
				if len(fields) != 4 || width < 1 || width > 16384 {
					return nil, stats, fmt.Errorf("invalid SVG viewport or width")
				}
				w, e1 := strconv.ParseFloat(fields[2], 64)
				h, e2 := strconv.ParseFloat(fields[3], 64)
				if e1 != nil || e2 != nil || w <= 0 || h <= 0 || math.IsInf(w+h, 0) || math.IsNaN(w+h) {
					return nil, stats, fmt.Errorf("invalid viewBox")
				}
				attr("xmlns", "http://www.w3.org/2000/svg")
				attr("width", strconv.Itoa(width))
				attr("height", strconv.FormatFloat(float64(width)*h/w, 'f', 6, 64))
				rootSeen = true
			}
			v.Attr = attrs
			if err = e.EncodeToken(v); err != nil {
				return nil, stats, err
			}
			if depth == 0 {
				bg := xml.StartElement{Name: xml.Name{Local: "rect"}, Attr: []xml.Attr{{Name: xml.Name{Local: "width"}, Value: "100%"}, {Name: xml.Name{Local: "height"}, Value: "100%"}, {Name: xml.Name{Local: "fill"}, Value: "#fff"}}}
				if err = e.EncodeToken(bg); err != nil {
					return nil, stats, err
				}
				if err = e.EncodeToken(bg.End()); err != nil {
					return nil, stats, err
				}
			}
			depth++
			continue
		case xml.EndElement:
			v.Name.Space = ""
			tok = v
			depth--
		}
		if err = e.EncodeToken(tok); err != nil {
			return nil, stats, err
		}
	}
	if !rootSeen {
		return nil, stats, fmt.Errorf("empty SVG")
	}
	if err := e.Flush(); err != nil {
		return nil, stats, err
	}
	return out.Bytes(), stats, nil
}

// writeClaritySVG 增强已有参考 SVG 的显示样式，并可附加一张模型 SVG 生成对照总览。
// 同时输出增强诊断；此路径不解析 DWG，也不代表 DWG 内容已完整还原。
func writeClaritySVG(input, appendSVG, out string, width int) error {
	b, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	clear, stats, err := enhanceReviewSVG(b, width)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(out, "清晰增强-主图.svg"), clear); err != nil {
		return err
	}
	if appendSVG != "" {
		model, err := os.ReadFile(appendSVG)
		if err != nil {
			return err
		}
		merged, err := combineOverviewSVG([]svgSheet{{Title: "01 · 原 CAD 主图（PDF 矢量增强，含原字体与零件详图）", Data: clear}, {Title: "02 · DWG 模型空间（由 Go-CAD 读取，供全范围核对）", Data: model}})
		if err != nil {
			return err
		}
		if err = atomicWrite(filepath.Join(out, "清晰增强-全部图纸.svg"), merged); err != nil {
			return err
		}
	}
	report := map[string]any{"source_svg": input, "appended_dwg_svg": appendSVG, "display_only": true, "stroke_factor": 1.12, "glyph_outline_points": 0.06, "stats": stats, "note": "PDF-derived reference, not a claim of complete go-cad DWG recovery; path coordinates and transforms are unchanged"}
	j, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(out, "clarity-audit.json"), j)
}
