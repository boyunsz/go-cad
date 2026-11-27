package render

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
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

// appendNum 以六位小数将 SVG 数值追加到 dst，把舍入后接近零的值统一写为正零。
func appendNum(dst []byte, v float64) []byte {
	if math.Abs(v) < .0000005 {
		v = 0
	}
	return strconv.AppendFloat(dst, v, 'f', 6, 64)
}

// num 以六位小数序列化 SVG 数值，把舍入后接近零的值统一写为正零。
func num(v float64) string {
	var buf [32]byte
	return string(appendNum(buf[:0], v))
}

// esc 转义 XML 文本和属性中的特殊字符，返回可嵌入 SVG 的字符串。
// 无 XML 特殊字符时直接返回原字符串，避免不必要的内存分配。
func esc(s string) string {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '&', '<', '>', '"', '\'', '\t', '\n', '\r':
			var b bytes.Buffer
			b.Grow(len(s) + 8)
			_ = xml.EscapeText(&b, []byte(s))
			return b.String()
		}
	}
	return s
}

// appendHexColor 以 #rrggbb 格式将颜色追加到 dst。
func appendHexColor(dst []byte, c color.RGBA) []byte {
	const hex = "0123456789abcdef"
	return append(dst, '#', hex[c.R>>4], hex[c.R&15], hex[c.G>>4], hex[c.G&15], hex[c.B>>4], hex[c.B&15])
}

// appendSVGPoint 把图纸坐标平移到 SVG 视口原点并翻转 Y 轴，直接追加到 dst。
func (p *PreparedDrawing) appendSVGPoint(dst []byte, q entity.Point2) []byte {
	dst = appendNum(dst, q.X-p.Audit.Bounds.MinX)
	dst = append(dst, ' ')
	return appendNum(dst, p.Audit.Bounds.MaxY-q.Y)
}

// geometryPath 将解析几何序列化为连续 SVG 路径，保留直线、三次曲线和椭圆弧。
func (p *PreparedDrawing) geometryPath(g *drawing.Geometry) string {
	return p.geometryPathWithDash(g, false)
}

// geometryPathWithDash 序列化解析曲线，并按 dashed 和原多段线设置决定是否在顶点重启子路径。
// 椭圆弧由仿射轴还原半轴和方向；退化弧按直线输出。
func (p *PreparedDrawing) geometryPathWithDash(g *drawing.Geometry, dashed bool) string {
	return string(p.appendGeometryPathWithDash(make([]byte, 0, len(g.Segments)*32), g, dashed))
}

// appendGeometryPathWithDash 将解析几何路径直接追加到 dst，避免中间字符串分配。
func (p *PreparedDrawing) appendGeometryPathWithDash(dst []byte, g *drawing.Geometry, dashed bool) []byte {
	reset := dashed && g.ResetDashEachSegment
	var prev entity.Point2
	has := false
	for _, s := range g.Segments {
		if reset || !has || math.Hypot(prev.X-s.P0.X, prev.Y-s.P0.Y) > 1e-8 {
			dst = append(dst, 'M')
			dst = p.appendSVGPoint(dst, s.P0)
		}
		if s.Cubic {
			dst = append(dst, 'C')
			dst = p.appendSVGPoint(dst, s.C1)
			dst = append(dst, ' ')
			dst = p.appendSVGPoint(dst, s.C2)
			dst = append(dst, ' ')
			dst = p.appendSVGPoint(dst, s.P1)
			prev = s.P1
			has = true
			continue
		}
		if !s.Arc {
			dst = append(dst, 'L')
			dst = p.appendSVGPoint(dst, s.P1)
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
		sweep := byte('0')
		if s.Sweep*(ux*vy-uy*vx) > 0 {
			sweep = '1'
		}
		for j := 1; j <= parts; j++ {
			q := s.Point(s.Start + s.Sweep*float64(j)/float64(parts))
			if j == parts {
				q = s.P1
			}
			if rx < 1e-12 || ry < 1e-12 {
				dst = append(dst, 'L')
				dst = p.appendSVGPoint(dst, q)
			} else {
				dst = append(dst, 'A')
				dst = appendNum(dst, rx)
				dst = append(dst, ' ')
				dst = appendNum(dst, ry)
				dst = append(dst, ' ')
				dst = appendNum(dst, angle)
				dst = append(dst, ' ', '0', ' ', sweep, ' ')
				dst = p.appendSVGPoint(dst, q)
			}
		}
		prev = s.P1
		has = true
	}
	if g.Closed && has && !reset {
		dst = append(dst, 'Z')
	}
	return dst
}

// strokePath 把有限线段序列化为 SVG 路径，连续端点共享子路径，非有限坐标跳过。
func (p *PreparedDrawing) strokePath(st []drawing.Stroke) string {
	return string(p.appendStrokePath(make([]byte, 0, len(st)*32), st))
}

// appendStrokePath 将有限线段路径直接追加到 dst，避免中间字符串分配。
func (p *PreparedDrawing) appendStrokePath(dst []byte, st []drawing.Stroke) []byte {
	var prev entity.Point2
	has := false
	for _, s := range st {
		a, c := point(s.X1, s.Y1), point(s.X2, s.Y2)
		if !finite(a.X) || !finite(a.Y) || !finite(c.X) || !finite(c.Y) {
			continue
		}
		if !has || a != prev {
			dst = append(dst, 'M')
			dst = p.appendSVGPoint(dst, a)
		}
		dst = append(dst, 'L')
		dst = p.appendSVGPoint(dst, c)
		prev = c
		has = true
	}
	return dst
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
	buf := make([]byte, 0, len(result)*12)
	for i, v := range result {
		if i > 0 {
			buf = append(buf, ' ')
		}
		buf = appendNum(buf, v)
	}
	return string(buf)
}

// outlinePath 把世界坐标字形轮廓序列化为 SVG 路径，并闭合每个轮廓子路径。
func (p *PreparedDrawing) outlinePath(cmds []pathCommand) string {
	return string(p.appendOutlinePath(make([]byte, 0, len(cmds)*24), cmds))
}

// appendOutlinePath 将字形轮廓路径直接追加到 dst，避免中间字符串分配。
func (p *PreparedDrawing) appendOutlinePath(dst []byte, cmds []pathCommand) []byte {
	open := false
	for _, c := range cmds {
		if c.Op == 'M' && open {
			dst = append(dst, 'Z')
		}
		dst = append(dst, c.Op)
		n := 1
		if c.Op == 'Q' {
			n = 2
		}
		if c.Op == 'C' {
			n = 3
		}
		for i := 0; i < n; i++ {
			if i > 0 {
				dst = append(dst, ' ')
			}
			dst = p.appendSVGPoint(dst, c.P[i])
		}
		open = true
	}
	if open {
		dst = append(dst, 'Z')
	}
	return dst
}

// SVG 将已准备的渲染计划输出为 SVG，包含视口裁切、字体轮廓或文本及诊断元数据。
// 不重新解析 DWG；返回的字节可直接写入文件。
func (p *PreparedDrawing) SVG() ([]byte, error) {
	est := len(p.Primitives)*192 + p.Audit.Glyphs*320 + 4096
	b := make([]byte, 0, est)
	box := p.Audit.Bounds
	b = append(b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `...)
	b = appendNum(b, box.MaxX-box.MinX)
	b = append(b, ' ')
	b = appendNum(b, box.MaxY-box.MinY)
	b = append(b, `" width="`...)
	b = strconv.AppendInt(b, int64(p.Audit.Width), 10)
	b = append(b, `" height="`...)
	b = strconv.AppendInt(b, int64(p.Audit.Height), 10)
	b = append(b, "\">\n"...)
	meta, _ := json.Marshal(struct {
		Generator string
		Audit     RenderAudit
	}{"go-cad-faithful", p.Audit})
	b = append(b, "<metadata>"...)
	b = append(b, esc(string(meta))...)
	b = append(b, "</metadata>\n"...)
	bg := p.Options.Background
	b = append(b, `<rect width="100%" height="100%" fill="`...)
	b = appendHexColor(b, bg)
	b = append(b, "\"/>\n"...)
	clipIDs := map[*drawing.Box2]int{}
	for _, e := range p.Primitives {
		if e.Clip != nil {
			if _, ok := clipIDs[e.Clip]; !ok {
				id := len(clipIDs)
				clipIDs[e.Clip] = id
				q := e.Clip
				b = append(b, `<defs><clipPath id="vp`...)
				b = strconv.AppendInt(b, int64(id), 10)
				b = append(b, `"><rect x="`...)
				b = appendNum(b, q.MinX-box.MinX)
				b = append(b, `" y="`...)
				b = appendNum(b, box.MaxY-q.MaxY)
				b = append(b, `" width="`...)
				b = appendNum(b, q.MaxX-q.MinX)
				b = append(b, `" height="`...)
				b = appendNum(b, q.MaxY-q.MinY)
				b = append(b, "\"/></clipPath></defs>\n"...)
			}
		}
	}
	var pathBuf []byte
	for i, e := range p.Primitives {
		if e.Clip != nil {
			b = append(b, `<g clip-path="url(#vp`...)
			b = strconv.AppendInt(b, int64(clipIDs[e.Clip]), 10)
			b = append(b, ")\">\n"...)
		}
		c := p.color(e)
		layer := primitiveDocument(p.Doc, e).LayerColors[e.Layer].DisplayName()
		b = append(b, `<g data-h="`...)
		b = append(b, strings.ToUpper(strconv.FormatUint(e.Source.Handle, 16))...)
		b = append(b, `" data-instance="`...)
		b = strconv.AppendInt(b, int64(i), 10)
		b = append(b, `" data-type="`...)
		b = append(b, esc(e.Source.Type)...)
		b = append(b, `" data-layer="`...)
		b = append(b, esc(layer)...)
		b = append(b, '"')
		if e.Source.File != "" {
			b = append(b, ` data-source-file="`...)
			b = append(b, esc(e.Source.File)...)
			b = append(b, '"')
		}
		b = append(b, ">\n"...)
		switch e.Kind {
		case 4:
			if im := p.Images[i]; im != nil {
				m := im.Matrix
				b = append(b, `<image width="`...)
				b = strconv.AppendInt(b, int64(im.Pixels.Bounds().Dx()), 10)
				b = append(b, `" height="`...)
				b = strconv.AppendInt(b, int64(im.Pixels.Bounds().Dy()), 10)
				b = append(b, `" href="data:image/png;base64,`...)
				b = append(b, im.Encoded...)
				b = append(b, `" transform="matrix(`...)
				b = appendNum(b, m[0])
				b = append(b, ' ')
				b = appendNum(b, -m[1])
				b = append(b, ' ')
				b = appendNum(b, m[2])
				b = append(b, ' ')
				b = appendNum(b, -m[3])
				b = append(b, ' ')
				b = appendNum(b, m[4]-box.MinX)
				b = append(b, ' ')
				b = appendNum(b, box.MaxY-m[5])
				b = append(b, ")\"/>\n"...)
			}
		case 0:
			pathBuf = pathBuf[:0]
			if e.Geometry != nil {
				pathBuf = p.appendGeometryPathWithDash(pathBuf, e.Geometry, len(e.Style.Dash) > 0)
			} else {
				pathBuf = p.appendStrokePath(pathBuf, e.Strokes)
			}
			if len(pathBuf) > 0 {
				b = append(b, `<path d="`...)
				b = append(b, pathBuf...)
				b = append(b, `" fill="none" stroke="`...)
				b = appendHexColor(b, c)
				b = append(b, `" stroke-width="`...)
				sw := p.strokeWidth(e)
				b = appendNum(b, sw)
				b = append(b, `" stroke-linecap="round" stroke-linejoin="round"`...)
				if ds := dashArray(e.Style.Dash, sw); ds != "" {
					b = append(b, ` stroke-dasharray="`...)
					b = append(b, ds...)
					b = append(b, '"')
				}
				b = append(b, "/>\n"...)
			}
		case 2:
			b = append(b, `<path d="`...)
			for _, l := range hatchLoops(e.Fill) {
				if len(l) < 3 {
					continue
				}
				b = append(b, 'M')
				b = p.appendSVGPoint(b, l[0])
				for _, q := range l[1:] {
					b = append(b, 'L')
					b = p.appendSVGPoint(b, q)
				}
				b = append(b, 'Z')
			}
			b = append(b, `" fill="`...)
			b = appendHexColor(b, c)
			b = append(b, "\" fill-rule=\"evenodd\"/>\n"...)
		case 1:
			layout := p.Texts[i]
			escText := esc(e.Lb.Text)
			b = append(b, `<g aria-label="`...)
			b = append(b, escText...)
			b = append(b, `" fill="`...)
			b = appendHexColor(b, c)
			b = append(b, `"><title>`...)
			b = append(b, escText...)
			b = append(b, "</title>\n"...)
			if p.Options.TextMode == "path" {
				for _, g := range layout.Glyphs {
					gc := p.glyphColor(e, g)
					b = append(b, `<path fill="`...)
					b = appendHexColor(b, gc)
					b = append(b, `" d="`...)
					b = p.appendOutlinePath(b, g.Path)
					b = append(b, "\"/>\n"...)
				}
			} else {
				for _, g := range layout.Glyphs {
					m := g.Matrix
					gc := p.glyphColor(e, g)
					// Browser text uses upward glyph coordinates relative to the baseline.
					b = append(b, `<text fill="`...)
					b = appendHexColor(b, gc)
					b = append(b, `" font-size="1" font-family="`...)
					b = append(b, esc(strings.TrimSuffix(filepath.Base(g.Font), filepath.Ext(g.Font)))...)
					b = append(b, `" transform="matrix(`...)
					b = appendNum(b, m[0])
					b = append(b, ' ')
					b = appendNum(b, -m[1])
					b = append(b, ' ')
					b = appendNum(b, m[2])
					b = append(b, ' ')
					b = appendNum(b, -m[3])
					b = append(b, ' ')
					b = appendNum(b, m[4]-box.MinX)
					b = append(b, ' ')
					b = appendNum(b, box.MaxY-m[5])
					b = append(b, `)">`...)
					b = append(b, esc(string(g.Rune))...)
					b = append(b, "</text>\n"...)
				}
			}
			if len(layout.Decor) > 0 {
				b = append(b, `<path d="`...)
				b = p.appendStrokePath(b, layout.Decor)
				b = append(b, `" fill="none" stroke="`...)
				b = appendHexColor(b, c)
				b = append(b, `" stroke-width="`...)
				b = appendNum(b, e.Lb.H*.04)
				b = append(b, `"/>`...)
			}
			b = append(b, "</g>\n"...)
		}
		b = append(b, "</g>\n"...)
		if e.Clip != nil {
			b = append(b, "</g>\n"...)
		}
	}
	b = append(b, "</svg>\n"...)
	return b, nil
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
