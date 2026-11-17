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

	cad "github.com/boyunsz/go-cad"
)

type svgSheet struct {
	Title   string
	Data    []byte
	ViewBox *[4]float64
	PaperMM [2]float64
}

// combineSVG stacks paper sheets; geometry stays unchanged inside each viewport.
// Reference IDs are isolated because pages reuse clip/paint server names.
func combineSVG(sheets []svgSheet) ([]byte, error) { return combineSheets(sheets, false) }

// combineOverviewSVG 生成带标题和等宽卡片的对照总览，复用统一的 SVG 合并实现。
func combineOverviewSVG(sheets []svgSheet) ([]byte, error) { return combineSheets(sheets, true) }

// combineSheets 将多张 SVG 按输入顺序纵向组合，隔离各页 ID 和引用以防裁切定义冲突。
// overview 使用带标题的等宽卡片；普通模式保留可验证的纸张比例，解析失败返回错误。
func combineSheets(sheets []svgSheet, overview bool) ([]byte, error) {
	type page struct {
		sheet         svgSheet
		Width, Height float64
	}
	pages := make([]page, 0, len(sheets))
	total, canvasWidth := 0.0, 0.0
	const pxPerMM = 96.0 / 25.4
	gap, left, header := 12.0, 0.0, 0.0
	if overview {
		gap, left, header = 36, 24, 64
	}
	for _, s := range sheets {
		d := xml.NewDecoder(bytes.NewReader(s.Data))
		var root xml.StartElement
		for {
			tok, err := d.Token()
			if err != nil {
				return nil, err
			}
			if e, ok := tok.(xml.StartElement); ok {
				root = e
				break
			}
		}
		if root.Name.Local != "svg" {
			return nil, fmt.Errorf("%s: root is not SVG", s.Title)
		}
		var w, h float64
		for _, a := range root.Attr {
			if a.Name.Local == "viewBox" {
				v := strings.Fields(strings.ReplaceAll(a.Value, ",", " "))
				if len(v) == 4 {
					w, _ = strconv.ParseFloat(v[2], 64)
					h, _ = strconv.ParseFloat(v[3], 64)
				}
			}
		}
		if w <= 0 || h <= 0 || math.IsNaN(w+h) || math.IsInf(w+h, 0) {
			return nil, fmt.Errorf("%s: invalid viewBox", s.Title)
		}
		if s.ViewBox != nil {
			w, h = s.ViewBox[2], s.ViewBox[3]
		}
		if s.PaperMM[0] > 0 && s.PaperMM[1] > 0 {
			w, h = s.PaperMM[0], s.PaperMM[1]
		}
		if overview {
			w, h = 2000, 2000*h/w
		} else {
			w *= pxPerMM
			h *= pxPerMM
		}
		if w <= 0 || h <= 0 || math.IsNaN(w+h) || math.IsInf(w+h, 0) {
			return nil, fmt.Errorf("%s: invalid paper size", s.Title)
		}
		pages = append(pages, page{s, w, h})
		canvasWidth = math.Max(canvasWidth, w)
		total += h + gap + header
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("no SVG sheets")
	}
	var out bytes.Buffer
	total -= gap
	if overview {
		total += left + gap
		canvasWidth += 2 * left
	}
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" width="%.6f" height="%.6f" viewBox="0 0 %.6f %.6f"><title>DWG 图纸布局合并</title><rect width="100%%" height="100%%" fill="white"/>`, canvasWidth, total, canvasWidth, total)
	y := left
	for i, p := range pages {
		fmt.Fprintf(&out, `<g data-page="%d"><title>`, i+1)
		if err := xml.EscapeText(&out, []byte(p.sheet.Title)); err != nil {
			return nil, err
		}
		out.WriteString("</title>")
		if overview {
			fmt.Fprintf(&out, `<text x="24" y="%.6f" fill="#34383c" font-family="sans-serif" font-size="25">`, y+34)
			if err := xml.EscapeText(&out, []byte(p.sheet.Title)); err != nil {
				return nil, err
			}
			out.WriteString("</text>")
		}
		d := xml.NewDecoder(bytes.NewReader(p.sheet.Data))
		e := xml.NewEncoder(&out)
		depth := 0
		prefix := fmt.Sprintf("sheet%d-", i)
		for {
			tok, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			switch v := tok.(type) {
			case xml.ProcInst:
				continue
			case xml.Directive:
				continue
			case xml.StartElement:
				v.Name.Space = ""
				attrs := make([]xml.Attr, 0, len(v.Attr)+3)
				for _, a := range v.Attr {
					if a.Name.Local == "xmlns" || a.Name.Space == "xmlns" {
						continue
					}
					// Our generated SVG uses SVG2-compatible href; keep embedded data URIs.
					a.Name.Space = ""
					if depth == 0 && (a.Name.Local == "x" || a.Name.Local == "y" || a.Name.Local == "width" || a.Name.Local == "height") {
						continue
					}
					if depth == 0 && a.Name.Local == "viewBox" && p.sheet.ViewBox != nil {
						v := p.sheet.ViewBox
						a.Value = fmt.Sprintf("%.9f %.9f %.9f %.9f", v[0], v[1], v[2], v[3])
					}
					if depth == 0 && a.Name.Local == "overflow" {
						continue
					}
					if a.Name.Local == "id" {
						a.Value = prefix + a.Value
					}
					if a.Name.Local == "href" && strings.HasPrefix(a.Value, "#") {
						a.Value = "#" + prefix + a.Value[1:]
					}
					a.Value = strings.ReplaceAll(a.Value, "url(#", "url(#"+prefix)
					attrs = append(attrs, a)
				}
				if depth == 0 {
					for _, a := range []xml.Attr{{Name: xml.Name{Local: "x"}, Value: fmt.Sprintf("%.6f", left)}, {Name: xml.Name{Local: "y"}, Value: fmt.Sprintf("%.6f", y+header)}, {Name: xml.Name{Local: "width"}, Value: fmt.Sprintf("%.6f", p.Width)}, {Name: xml.Name{Local: "height"}, Value: fmt.Sprintf("%.6f", p.Height)}} {
						attrs = append(attrs, a)
					}
				}
				if depth == 0 {
					attrs = append(attrs, xml.Attr{Name: xml.Name{Local: "overflow"}, Value: "hidden"})
				}
				v.Attr = attrs
				tok = v
				depth++
			case xml.EndElement:
				v.Name.Space = ""
				tok = v
				depth--
			}
			if err = e.EncodeToken(tok); err != nil {
				return nil, err
			}
		}
		if err := e.Flush(); err != nil {
			return nil, err
		}
		out.WriteString("</g>")
		y += p.Height + gap + header
	}
	out.WriteString("</svg>\n")
	return out.Bytes(), nil
}

// writeCombinedSVG 读取 DWG 布局顺序及输出目录中的单页 SVG，生成各指定样式的合并文件。
// includeModel 决定是否追加模型总览；没有纸张布局时使用模型输出，缺少所需产物返回错误。
func writeCombinedSVG(input, out, style string, includeModel bool) error {
	b, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	doc, err := cad.Parse(b)
	if err != nil {
		return err
	}
	styles := []string{style}
	if style == "both" {
		styles = []string{"original", "soft"}
	}
	for _, style := range styles {
		suffix := ".faithful"
		if style == "soft" || style == "clear" {
			suffix += "-" + style
		}
		name := filepath.Base(input) + suffix + ".svg"
		type inputSheet struct {
			title, path string
			handle      uint64
		}
		paths := []inputSheet{}
		for _, layout := range doc.Layouts() {
			if layout.Model {
				continue
			}
			dir := fmt.Sprintf("%X-", layout.Handle) + strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(layout.Name)
			path := filepath.Join(out, "layouts", dir, name)
			if _, err := os.Stat(path); os.IsNotExist(err) && layout.RawName != "" {
				oldDir := fmt.Sprintf("%X-", layout.Handle) + strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(layout.RawName)
				path = filepath.Join(out, "layouts", oldDir, name)
			}
			paths = append(paths, inputSheet{layout.Name, path, layout.Handle})
		}
		if includeModel || len(paths) == 0 {
			paths = append(paths, inputSheet{"模型空间", filepath.Join(out, name), 0})
		}
		sheets := []svgSheet{}
		for _, p := range paths {
			b, err := os.ReadFile(p.path)
			if err != nil {
				return err
			}
			sheet := svgSheet{Title: p.title, Data: b}
			if g := doc.InternalObjects()[p.handle]; g != nil {
				if box, mm, ok := paperFootprint(g); ok {
					var audit struct{ Bounds cad.Box2 }
					auditBytes, err := os.ReadFile(filepath.Join(filepath.Dir(p.path), "render-audit.json"))
					if err != nil {
						return fmt.Errorf("paper merge requires render-audit.json: %w", err)
					}
					if err = json.Unmarshal(auditBytes, &audit); err != nil {
						return err
					}
					if !validPaperBox(audit.Bounds) {
						return fmt.Errorf("invalid audit bounds: %s", p.path)
					}
					sheet.ViewBox = &[4]float64{box.MinX - audit.Bounds.MinX, audit.Bounds.MaxY - box.MaxY, box.MaxX - box.MinX, box.MaxY - box.MinY}
					sheet.PaperMM = mm
				} else {
					fmt.Printf("布局 %s：纸张范围未通过验证，保留完整 SVG 范围\n", p.title)
				}
			}
			sheets = append(sheets, sheet)
		}
		merged, err := combineSVG(sheets)
		if err != nil {
			return err
		}
		target := filepath.Join(out, filepath.Base(input)+".combined-"+style+".svg")
		if err = atomicWrite(target, merged); err != nil {
			return err
		}
		fmt.Printf("合并 %d 页 → %s (%d 字节)\n", len(sheets), target, len(merged))
	}
	return nil
}

// paperFootprint 验证布局界限与毫米纸张尺寸、旋转方向及显式打印比例是否一致。
// 成功返回图纸坐标边界、纸张宽高和 true；无法验证时返回 false，避免按错误界限裁图。
func paperFootprint(g interface{ Field(string) any }) (cad.Box2, [2]float64, bool) {
	number := func(k string) float64 { v, _ := g.Field(k).(float64); return v }
	lo, _ := g.Field("LIMMIN").([]float64)
	hi, _ := g.Field("LIMMAX").([]float64)
	unit, _ := g.Field("plotsettings.plot_paper_unit").(int64)
	kind, _ := g.Field("plotsettings.plot_type").(int64)
	rotation, _ := g.Field("plotsettings.plot_rotation_mode").(int64)
	w, h := number("plotsettings.paper_width"), number("plotsettings.paper_height")
	if rotation == 1 {
		w, h = h, w
	}
	scale := number("plotsettings.paper_units") / number("plotsettings.drawing_units")
	if len(lo) != 2 || len(hi) != 2 || unit != 1 || (kind != 4 && kind != 5) || rotation < 0 || rotation > 1 || w <= 0 || h <= 0 || scale <= 0 || math.IsNaN(w+h+scale) || math.IsInf(w+h+scale, 0) {
		return cad.Box2{}, [2]float64{}, false
	}
	box := cad.Box2{MinX: lo[0], MinY: lo[1], MaxX: hi[0], MaxY: hi[1]}
	if !validPaperBox(box) || math.Abs((hi[0]-lo[0])*scale-w) > .05 || math.Abs((hi[1]-lo[1])*scale-h) > .05 {
		return cad.Box2{}, [2]float64{}, false
	}
	return box, [2]float64{w, h}, true
}

// validPaperBox 判断纸张边界的坐标是否有限，且宽度与高度均为正数。
func validPaperBox(b cad.Box2) bool {
	for _, v := range []float64{b.MinX, b.MinY, b.MaxX, b.MaxY} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return b.MaxX > b.MinX && b.MaxY > b.MinY
}
