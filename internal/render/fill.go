package render

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/drawing"
	"github.com/boyunsz/go-cad/internal/entity"
	"golang.org/x/image/vector"
	"image"
	"image/color"
	"math"
	"sort"
)

// insideLoop 通过射线交点的奇偶规则判断点是否位于多边形内部。
// 空轮廓返回 false；边界点沿用半开区间交点规则。
func insideLoop(p entity.Point2, loop []entity.Point2) bool {
	inside := false
	for i, j := 0, len(loop)-1; i < len(loop); j, i = i, i+1 {
		a, b := loop[i], loop[j]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X {
			inside = !inside
		}
	}
	return inside
}

// hatchLoops 按 HATCH 的岛屿处理样式筛选填充轮廓；普通样式直接使用原轮廓。
func hatchLoops(f *drawing.FillGeometry) [][]entity.Point2 {
	if f.Hatch == nil || f.Hatch.Style == 0 {
		return f.Loops
	}
	var out [][]entity.Point2
	for i, l := range f.Loops {
		if len(l) < 3 {
			continue
		}
		depth := 0
		for j, other := range f.Loops {
			if i != j && insideLoop(l[0], other) {
				depth++
			}
		}
		if (f.Hatch.Style == 1 && depth <= 1) || (f.Hatch.Style == 2 && depth == 0) {
			out = append(out, l)
		}
	}
	return out
}

// hatchStrokes 生成 HATCH 图案线，与有效轮廓裁切后转换到世界坐标。
// 缺少定义、零间距或超过线条/线段预算时返回错误，并可能同时返回已生成的部分线段。
func hatchStrokes(f *drawing.FillGeometry, tol float64) ([]drawing.Stroke, error) {
	h := f.Hatch
	if len(h.Deflines) == 0 {
		return nil, fmt.Errorf("hatch has no pattern definition")
	}
	// Pattern definition lines are stored with their actual coordinates/angles.
	// Applying the entity angle/scale a second time would rotate/scale them twice.
	local := &drawing.FillGeometry{Hatch: h}
	local.Loops = f.LocalLoops
	if len(local.Loops) == 0 {
		for _, p := range h.Paths {
			local.Loops = append(local.Loops, p.Points)
		}
	}
	loops := hatchLoops(local)
	var result []drawing.Stroke
	for _, dl := range h.Deflines {
		u := point(math.Cos(dl.Angle), math.Sin(dl.Angle))
		n := point(-u.Y, u.X)
		step := dl.Offset.X*n.X + dl.Offset.Y*n.Y
		if math.Abs(step) < 1e-12 {
			return result, fmt.Errorf("hatch definition offset has zero spacing")
		}
		low, high := math.Inf(1), math.Inf(-1)
		for _, l := range loops {
			for _, p := range l {
				d := (p.X-dl.Pt0.X)*n.X + (p.Y-dl.Pt0.Y)*n.Y
				low = math.Min(low, d)
				high = math.Max(high, d)
			}
		}
		a, b := low/step, high/step
		if a > b {
			a, b = b, a
		}
		first, last := int(math.Ceil(a)), int(math.Floor(b))
		if last-first > 20000 {
			return result, fmt.Errorf("hatch pattern exceeds 20000-line limit")
		}
		for k := first; k <= last; k++ {
			origin := point(dl.Pt0.X+float64(k)*dl.Offset.X, dl.Pt0.Y+float64(k)*dl.Offset.Y)
			var hits []float64
			for _, loop := range loops {
				for i, j := 0, len(loop)-1; i < len(loop); j, i = i, i+1 {
					p, q := loop[j], loop[i]
					pa := (p.X-origin.X)*n.X + (p.Y-origin.Y)*n.Y
					qa := (q.X-origin.X)*n.X + (q.Y-origin.Y)*n.Y
					if (pa > 0) == (qa > 0) {
						continue
					}
					v := pa / (pa - qa)
					x, y := p.X+v*(q.X-p.X), p.Y+v*(q.Y-p.Y)
					hits = append(hits, (x-origin.X)*u.X+(y-origin.Y)*u.Y)
				}
			}
			sort.Float64s(hits)
			for i := 0; i+1 < len(hits); i += 2 {
				start, end := hits[i], hits[i+1]
				p, q := point(origin.X+start*u.X, origin.Y+start*u.Y), point(origin.X+end*u.X, origin.Y+end*u.Y)
				parts := dashStrokes([]drawing.Stroke{{X1: p.X, Y1: p.Y, X2: q.X, Y2: q.Y}}, dl.Dashes, start, math.Max(tol, .01))
				for _, s := range parts {
					p, q := f.Transform.Apply(point(s.X1, s.Y1)), f.Transform.Apply(point(s.X2, s.Y2))
					result = append(result, drawing.Stroke{X1: p.X, Y1: p.Y, X2: q.X, Y2: q.Y})
				}
				if len(result) > 500000 {
					return result, fmt.Errorf("hatch pattern exceeds segment limit")
				}
			}
		}
	}
	return result, nil
}

// dashStrokes 按 CAD 正值划线、负值空隙的模式拆分描边，offset 指定起始相位。
// 零值模式项以正数 dot 代替；断开的线段重置相位，空模式返回原切片。
func dashStrokes(st []drawing.Stroke, pattern []float64, offset, dot float64) []drawing.Stroke {
	if len(pattern) == 0 {
		return st
	}
	pat := append([]float64(nil), pattern...)
	period := 0.0
	for i, v := range pat {
		if v == 0 {
			pat[i] = dot
		}
		period += math.Abs(pat[i])
	}
	if period <= 1e-12 {
		return st
	}
	phase := math.Mod(offset, period)
	if phase < 0 {
		phase += period
	}
	idx := 0
	for phase >= math.Abs(pat[idx]) && idx < len(pat)-1 {
		phase -= math.Abs(pat[idx])
		idx++
	}
	left := math.Abs(pat[idx]) - phase
	var out []drawing.Stroke
	var prev entity.Point2
	hasPrev := false
	for _, s := range st {
		p, q := point(s.X1, s.Y1), point(s.X2, s.Y2)
		if hasPrev && math.Hypot(p.X-prev.X, p.Y-prev.Y) > 1e-7 {
			idx = 0
			left = math.Abs(pat[0])
		}
		prev = q
		hasPrev = true
		dx, dy := q.X-p.X, q.Y-p.Y
		l := math.Hypot(dx, dy)
		if l == 0 {
			continue
		}
		pos := 0.0
		for pos < l-1e-12 {
			take := math.Min(left, l-pos)
			if pat[idx] >= 0 {
				out = append(out, drawing.Stroke{X1: p.X + dx*pos/l, Y1: p.Y + dy*pos/l, X2: p.X + dx*(pos+take)/l, Y2: p.Y + dy*(pos+take)/l})
			}
			pos += take
			left -= take
			if left < 1e-10 {
				idx = (idx + 1) % len(pat)
				left = math.Abs(pat[idx])
			}
		}
	}
	return out
}

// pixel 把世界坐标转换为当前画布的像素坐标，并将 Y 轴方向翻转。
func (p *PreparedDrawing) pixel(q entity.Point2) entity.Point2 {
	b := p.Audit.Bounds
	scale := float64(p.Audit.Width) / (b.MaxX - b.MinX)
	return point((q.X-b.MinX)*scale, (b.MaxY-q.Y)*scale)
}

// fillLoops 按奇偶填充规则在图像上绘制世界坐标轮廓。
// 每个像素行使用四个子采样行估算覆盖率，并裁切到目标图像范围。
func (p *PreparedDrawing) fillLoops(img *image.RGBA, loops [][]entity.Point2, col color.RGBA) {
	var px [][]entity.Point2
	minY, maxY := float64(img.Bounds().Max.Y), 0.0
	for _, l := range loops {
		var pp []entity.Point2
		for _, q := range l {
			v := p.pixel(q)
			pp = append(pp, v)
			minY = math.Min(minY, v.Y)
			maxY = math.Max(maxY, v.Y)
		}
		px = append(px, pp)
	}
	for y := max(0, int(math.Floor(minY))); y < min(img.Bounds().Max.Y, int(math.Ceil(maxY))); y++ {
		// Four subrows provide coverage antialiasing without a full supersized image.
		coverage := map[int]float64{}
		for sample := 0; sample < 4; sample++ {
			yy := float64(y) + (float64(sample)+.5)/4
			var xs []float64
			for _, l := range px {
				for i, j := 0, len(l)-1; i < len(l); j, i = i, i+1 {
					a, b := l[j], l[i]
					if (a.Y > yy) != (b.Y > yy) {
						xs = append(xs, a.X+(b.X-a.X)*(yy-a.Y)/(b.Y-a.Y))
					}
				}
			}
			sort.Float64s(xs)
			for i := 0; i+1 < len(xs); i += 2 {
				lo, hi := math.Max(0, xs[i]), math.Min(float64(img.Bounds().Max.X), xs[i+1])
				for x := int(math.Floor(lo)); x < int(math.Ceil(hi)); x++ {
					coverage[x] += math.Max(0, math.Min(hi, float64(x+1))-math.Max(lo, float64(x))) / 4
				}
			}
		}
		for x, c := range coverage {
			blendCoverage(img, x, y, col, c)
		}
	}
}

// paintOutline 将字形路径转换为像素路径并栅格化到目标图像。
// 按字形边界分配局部覆盖缓冲，空轮廓直接返回。
func (p *PreparedDrawing) paintOutline(img *image.RGBA, cmds []pathCommand, col color.RGBA) {
	if len(cmds) == 0 {
		return
	}
	minX, minY, maxX, maxY := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, cmd := range cmds {
		n := 1
		if cmd.Op == 'Q' {
			n = 2
		}
		if cmd.Op == 'C' {
			n = 3
		}
		for i := 0; i < n; i++ {
			q := p.pixel(cmd.P[i])
			minX = math.Min(minX, q.X)
			minY = math.Min(minY, q.Y)
			maxX = math.Max(maxX, q.X)
			maxY = math.Max(maxY, q.Y)
		}
	}
	rect := image.Rect(int(math.Floor(minX))-1, int(math.Floor(minY))-1, int(math.Ceil(maxX))+1, int(math.Ceil(maxY))+1).Intersect(img.Bounds())
	if rect.Empty() {
		return
	}
	rast := vector.NewRasterizer(rect.Dx(), rect.Dy())
	xy := func(q entity.Point2) (float32, float32) {
		v := p.pixel(q)
		return float32(v.X - float64(rect.Min.X)), float32(v.Y - float64(rect.Min.Y))
	}
	open := false
	for _, cmd := range cmds {
		x, y := xy(cmd.P[0])
		switch cmd.Op {
		case 'M':
			if open {
				rast.ClosePath()
			}
			rast.MoveTo(x, y)
			open = true
		case 'L':
			rast.LineTo(x, y)
		case 'Q':
			a, b := xy(cmd.P[1])
			rast.QuadTo(x, y, a, b)
		case 'C':
			a, b := xy(cmd.P[1])
			c, d := xy(cmd.P[2])
			rast.CubeTo(x, y, a, b, c, d)
		}
	}
	if open {
		rast.ClosePath()
	}
	rast.Draw(img, rect, image.NewUniform(col), image.Point{})
}
