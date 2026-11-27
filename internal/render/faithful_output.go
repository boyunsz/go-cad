package render

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

// num 以六位小数序列化 SVG 数值，把舍入后接近零的值统一写为正零。
func num(v float64) string {
	if math.Abs(v) < .0000005 {
		v = 0
	}
	return strconv.FormatFloat(v, 'f', 6, 64)
}

// esc 转义 XML 文本和属性中的特殊字符，返回可嵌入 SVG 的字符串。
func esc(s string) string { var b bytes.Buffer; _ = xml.EscapeText(&b, []byte(s)); return b.String() }

// svgPoint 把图纸坐标平移到 SVG 视口原点并翻转 Y 轴，输出坐标数值对。
func (p *PreparedDrawing) svgPoint(q entity.Point2) string {
	return num(q.X-p.Audit.Bounds.MinX) + " " + num(p.Audit.Bounds.MaxY-q.Y)
}

// geometryPath 将解析几何序列化为连续 SVG 路径，保留直线、三次曲线和椭圆弧。
func (p *PreparedDrawing) geometryPath(g *drawing.Geometry) string {
	return p.geometryPathWithDash(g, false)
}

// geometryPathWithDash 序列化解析曲线，并按 dashed 和原多段线设置决定是否在顶点重启子路径。
// 椭圆弧由仿射轴还原半轴和方向；退化弧按直线输出。
func (p *PreparedDrawing) geometryPathWithDash(g *drawing.Geometry, dashed bool) string {
	reset := dashed && g.ResetDashEachSegment
	var b strings.Builder
	var prev entity.Point2
	has := false
	for _, s := range g.Segments {
		if reset || !has || math.Hypot(prev.X-s.P0.X, prev.Y-s.P0.Y) > 1e-8 {
			b.WriteString("M" + p.svgPoint(s.P0))
		}
		if s.Cubic {
			b.WriteString("C" + p.svgPoint(s.C1) + " " + p.svgPoint(s.C2) + " " + p.svgPoint(s.P1))
			prev = s.P1
			has = true
			continue
		}
		if !s.Arc {
			b.WriteString("L" + p.svgPoint(s.P1))
			prev = s.P1
			has = true
			continue
		}
		// Singular values of the affine ellipse, with Y flipped into SVG space.
		ux, uy, vx, vy := s.U.X, -s.U.Y, s.V.X, -s.V.Y
		xx, yy, xy := ux*ux+vx*vx, uy*uy+vy*vy, ux*uy+vx*vy
		disc := math.Hypot(xx-yy, 2*xy)
		rx := math.Sqrt(math.Max(0, (xx+yy+disc)/2))
		ry := math.Sqrt(math.Max(0, (xx+yy-disc)/2))
		angle := math.Atan2(2*xy, xx-yy) * 90 / math.Pi
		parts := int(math.Ceil(math.Abs(s.Sweep) / math.Pi))
		if parts < 1 {
			parts = 1
		}
		sweep := 0
		if s.Sweep*(ux*vy-uy*vx) > 0 {
			sweep = 1
		}
		for j := 1; j <= parts; j++ {
			q := s.Point(s.Start + s.Sweep*float64(j)/float64(parts))
			if j == parts {
				q = s.P1
			}
			if rx < 1e-12 || ry < 1e-12 {
				b.WriteString("L" + p.svgPoint(q))
			} else {
				fmt.Fprintf(&b, "A%s %s %s 0 %d %s", num(rx), num(ry), num(angle), sweep, p.svgPoint(q))
			}
		}
		prev = s.P1
		has = true
	}
	if g.Closed && has && !reset {
		b.WriteByte('Z')
	}
	return b.String()
}

// strokePath 把有限线段序列化为 SVG 路径，连续端点共享子路径，非有限坐标跳过。
func (p *PreparedDrawing) strokePath(st []drawing.Stroke) string {
	var b strings.Builder
	var prev entity.Point2
	has := false
	for _, s := range st {
		a, c := point(s.X1, s.Y1), point(s.X2, s.Y2)
		if !finite(a.X) || !finite(a.Y) || !finite(c.X) || !finite(c.Y) {
			continue
		}
		if !has || a != prev {
			b.WriteString("M" + p.svgPoint(a))
		}
		b.WriteString("L" + p.svgPoint(c))
		prev = c
		has = true
	}
	return b.String()
}

// dashArray 将 CAD 的有符号划线/间隔序列转换为 SVG stroke-dasharray。
// 零长度划线用 dot 代替，并补齐交替项和偶数项约定。
func dashArray(ds []float64, dot float64) string {
	if len(ds) == 0 {
		return ""
	}
	var result []float64
	draw := true
	for _, v := range ds {
		yes := v >= 0
		if yes != draw {
			result = append(result, 0)
			draw = !draw
		}
		length := math.Abs(v)
		if length == 0 {
			length = dot
		}
		result = append(result, length)
		draw = !draw
	}
	if len(result)%2 != 0 {
		result = append(result, 0)
	}
	vals := make([]string, len(result))
	for i, v := range result {
		vals[i] = num(v)
	}
	return strings.Join(vals, " ")
}

// outlinePath 把世界坐标字形轮廓序列化为 SVG 路径，并闭合每个轮廓子路径。
func (p *PreparedDrawing) outlinePath(cmds []pathCommand) string {
	var b strings.Builder
	open := false
	for _, c := range cmds {
		if c.Op == 'M' && open {
			b.WriteByte('Z')
		}
		b.WriteByte(c.Op)
		n := 1
		if c.Op == 'Q' {
			n = 2
		}
		if c.Op == 'C' {
			n = 3
		}
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(p.svgPoint(c.P[i]))
		}
		open = true
	}
	if open {
		b.WriteByte('Z')
	}
	return b.String()
}

// SVG 将已准备的渲染计划输出为 SVG，包含视口裁切、字体轮廓或文本及诊断元数据。
// 不重新解析 DWG；返回的字节可直接写入文件。
func (p *PreparedDrawing) SVG() ([]byte, error) {
	var b strings.Builder
	box := p.Audit.Bounds
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 %s %s\" width=\"%d\" height=\"%d\">\n", num(box.MaxX-box.MinX), num(box.MaxY-box.MinY), p.Audit.Width, p.Audit.Height)
	meta, _ := json.Marshal(struct {
		Generator string
		Audit     RenderAudit
	}{"go-cad-faithful", p.Audit})
	b.WriteString("<metadata>" + esc(string(meta)) + "</metadata>\n")
	bg := p.Options.Background
	fmt.Fprintf(&b, "<rect width=\"100%%\" height=\"100%%\" fill=\"#%02x%02x%02x\"/>\n", bg.R, bg.G, bg.B)
	clipIDs := map[*drawing.Box2]int{}
	for _, e := range p.Primitives {
		if e.Clip != nil {
			if _, ok := clipIDs[e.Clip]; !ok {
				id := len(clipIDs)
				clipIDs[e.Clip] = id
				q := e.Clip
				fmt.Fprintf(&b, "<defs><clipPath id=\"vp%d\"><rect x=\"%s\" y=\"%s\" width=\"%s\" height=\"%s\"/></clipPath></defs>\n", id, num(q.MinX-box.MinX), num(box.MaxY-q.MaxY), num(q.MaxX-q.MinX), num(q.MaxY-q.MinY))
			}
		}
	}
	for i, e := range p.Primitives {
		if e.Clip != nil {
			fmt.Fprintf(&b, "<g clip-path=\"url(#vp%d)\">\n", clipIDs[e.Clip])
		}
		c := p.color(e)
		hex := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
		layer := primitiveDocument(p.Doc, e).LayerColors[e.Layer].DisplayName()
		fileAttr := ""
		if e.Source.File != "" {
			fileAttr = " data-source-file=\"" + esc(e.Source.File) + "\""
		}
		fmt.Fprintf(&b, "<g data-h=\"%X\" data-instance=\"%d\" data-type=\"%s\" data-layer=\"%s\"%s>\n", e.Source.Handle, i, esc(e.Source.Type), esc(layer), fileAttr)
		switch e.Kind {
		case 4:
			if im := p.Images[i]; im != nil {
				m := im.Matrix
				fmt.Fprintf(&b, "<image width=\"%d\" height=\"%d\" href=\"data:image/png;base64,%s\" transform=\"matrix(%s %s %s %s %s %s)\"/>\n", im.Pixels.Bounds().Dx(), im.Pixels.Bounds().Dy(), im.Encoded, num(m[0]), num(-m[1]), num(m[2]), num(-m[3]), num(m[4]-box.MinX), num(box.MaxY-m[5]))
			}
		case 0:
			d := p.strokePath(e.Strokes)
			if e.Geometry != nil {
				d = p.geometryPathWithDash(e.Geometry, len(e.Style.Dash) > 0)
			}
			if d != "" {
				fmt.Fprintf(&b, "<path d=\"%s\" fill=\"none\" stroke=\"%s\" stroke-width=\"%s\" stroke-linecap=\"round\" stroke-linejoin=\"round\"", d, hex, num(p.strokeWidth(e)))
				if ds := dashArray(e.Style.Dash, p.strokeWidth(e)); ds != "" {
					fmt.Fprintf(&b, " stroke-dasharray=\"%s\"", ds)
				}
				b.WriteString("/>\n")
			}
		case 2:
			var d strings.Builder
			for _, l := range hatchLoops(e.Fill) {
				if len(l) < 3 {
					continue
				}
				d.WriteString("M" + p.svgPoint(l[0]))
				for _, q := range l[1:] {
					d.WriteString("L" + p.svgPoint(q))
				}
				d.WriteByte('Z')
			}
			fmt.Fprintf(&b, "<path d=\"%s\" fill=\"%s\" fill-rule=\"evenodd\"/>\n", d.String(), hex)
		case 1:
			layout := p.Texts[i]
			fmt.Fprintf(&b, "<g aria-label=\"%s\" fill=\"%s\"><title>%s</title>\n", esc(e.Lb.Text), hex, esc(e.Lb.Text))
			if p.Options.TextMode == "path" {
				for _, g := range layout.Glyphs {
					c := p.glyphColor(e, g)
					fmt.Fprintf(&b, "<path fill=\"#%02x%02x%02x\" d=\"%s\"/>\n", c.R, c.G, c.B, p.outlinePath(g.Path))
				}
			} else {
				for _, g := range layout.Glyphs {
					m := g.Matrix
					gc := p.glyphColor(e, g)
					// Browser text uses upward glyph coordinates relative to the baseline.
					fmt.Fprintf(&b, "<text fill=\"#%02x%02x%02x\" font-size=\"1\" font-family=\"%s\" transform=\"matrix(%s %s %s %s %s %s)\">%s</text>\n", gc.R, gc.G, gc.B, esc(strings.TrimSuffix(filepath.Base(g.Font), filepath.Ext(g.Font))), num(m[0]), num(-m[1]), num(m[2]), num(-m[3]), num(m[4]-box.MinX), num(box.MaxY-m[5]), esc(string(g.Rune)))
				}
			}
			if len(layout.Decor) > 0 {
				fmt.Fprintf(&b, "<path d=\"%s\" fill=\"none\" stroke=\"%s\" stroke-width=\"%s\"/>", p.strokePath(layout.Decor), hex, num(e.Lb.H*.04))
			}
			b.WriteString("</g>\n")
		}
		b.WriteString("</g>\n")
		if e.Clip != nil {
			b.WriteString("</g>\n")
		}
	}
	b.WriteString("</svg>\n")
	return []byte(b.String()), nil
}

// PNG 按已准备的画布尺寸绘制几何、字形和图片，应用视口裁切后编码为 PNG。
// 编码错误通过 error 返回；应先通过 PrepareDrawing 创建有效渲染计划。
func (p *PreparedDrawing) PNG() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, p.Audit.Width, p.Audit.Height))
	drawRect(img, img.Bounds(), p.Options.Background)
	scale := float64(p.Audit.Width) / (p.Audit.Bounds.MaxX - p.Audit.Bounds.MinX)
	var activeClip *drawing.Box2
	target := img
	flush := func() {
		if activeClip != nil {
			q := activeClip
			lo, hi := p.pixel(point(q.MinX, q.MaxY)), p.pixel(point(q.MaxX, q.MinY))
			r := image.Rect(int(math.Ceil(lo.X)), int(math.Ceil(lo.Y)), int(math.Ceil(hi.X)), int(math.Ceil(hi.Y))).Intersect(img.Bounds())
			draw.Draw(img, r, target, r.Min, draw.Over)
		}
	}
	for i, e := range p.Primitives {
		if e.Clip != activeClip {
			flush()
			activeClip = e.Clip
			target = img
			if activeClip != nil {
				target = image.NewRGBA(img.Bounds())
			}
		}
		c := p.color(e)
		switch e.Kind {
		case 4:
			p.paintImage(target, p.Images[i])
		case 0:
			st := dashStrokes(e.Strokes, e.Style.Dash, 0, p.strokeWidth(e))
			if e.Geometry != nil && e.Geometry.ResetDashEachSegment && len(e.Style.Dash) > 0 {
				st = nil
				for _, seg := range e.Geometry.Segments {
					g := drawing.Geometry{Segments: []drawing.CurveSegment{seg}}
					st = append(st, dashStrokes(g.Strokes(p.Options.CurveTolerance), e.Style.Dash, 0, p.strokeWidth(e))...)
				}
			}
			for _, s := range st {
				p.paintStroke(target, s, p.strokeWidth(e)*scale, c)
			}
		case 2:
			p.fillLoops(target, hatchLoops(e.Fill), c)
		case 1:
			layout := p.Texts[i]
			for _, g := range layout.Glyphs {
				p.paintOutline(target, g.Path, p.glyphColor(e, g))
			}
			for _, s := range layout.Decor {
				p.paintStroke(target, s, e.Lb.H*.04*scale, c)
			}
		}
	}
	flush()
	var b bytes.Buffer
	err := png.Encode(&b, img)
	return b.Bytes(), err
}

// paintStroke 以胶囊形线段的解析覆盖率绘制抗锯齿描边，坐标和宽度均为像素单位。
func (p *PreparedDrawing) paintStroke(img *image.RGBA, s drawing.Stroke, width float64, c color.RGBA) {
	a, b := p.pixel(point(s.X1, s.Y1)), p.pixel(point(s.X2, s.Y2))
	if !finite(a.X) || !finite(a.Y) || !finite(b.X) || !finite(b.Y) {
		return
	}
	radius := math.Max(.15, width/2)
	extent := radius + .5
	dx, dy := b.X-a.X, b.Y-a.Y
	den := dx*dx + dy*dy
	y0, y1 := max(0, int(math.Floor(math.Min(a.Y, b.Y)-extent))), min(img.Bounds().Max.Y, int(math.Ceil(math.Max(a.Y, b.Y)+extent)))
	for y := y0; y < y1; y++ {
		yy := float64(y) + .5
		lo, hi := math.Min(a.X, b.X)-extent, math.Max(a.X, b.X)+extent
		if math.Abs(dy) > 1e-9 {
			t0 := math.Max(0, math.Min(1, (yy-extent-a.Y)/dy))
			t1 := math.Max(0, math.Min(1, (yy+extent-a.Y)/dy))
			lo = math.Min(a.X+t0*dx, a.X+t1*dx) - extent
			hi = math.Max(a.X+t0*dx, a.X+t1*dx) + extent
		}
		for x := max(0, int(math.Floor(lo))); x < min(img.Bounds().Max.X, int(math.Ceil(hi))); x++ {
			xx := float64(x) + .5
			t := 0.0
			if den > 0 {
				t = math.Max(0, math.Min(1, ((xx-a.X)*dx+(yy-a.Y)*dy)/den))
			}
			dist := math.Hypot(xx-a.X-t*dx, yy-a.Y-t*dy)
			cov := math.Max(0, math.Min(1, extent-dist))
			if cov > 0 {
				blendCoverage(img, x, y, c, cov)
			}
		}
	}
}

// glyphColor 按文字片段的真彩色或索引色覆盖图元颜色，再应用当前显示样式。
// 颜色覆盖只作用于图元副本，不修改文档实体。
func (p *PreparedDrawing) glyphColor(e drawing.Primitive, g laidGlyph) color.RGBA {
	if p.Options.Monochrome {
		return color.RGBA{0, 0, 0, 255}
	}
	if g.TrueColor >= 0 {
		e.Color = entity.EntColor{HasTrue: true, TrueColor: uint32(g.TrueColor)}
	} else if g.ColorIndex > 0 && g.ColorIndex < 256 {
		e.Color = entity.EntColor{HasIndex: true, Index: uint16(g.ColorIndex)}
	}
	return p.color(e)
}
