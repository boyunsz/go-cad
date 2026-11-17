package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"math"
)

// CurveSegment is a line, a cubic Bezier, or an affine ellipse C+U*cos(a)+V*sin(a).
type CurveSegment struct {
	P0, P1, C, U, V entity.Point2
	Start, Sweep    float64
	Arc             bool
	Cubic           bool
	C1, C2          entity.Point2
}
type Geometry struct {
	Closed               bool
	ResetDashEachSegment bool // PLINEGEN disabled: restart at original vertices, not tessellation samples.
	Segments             []CurveSegment
	Warnings             []string
}

// Point 按弧度参数 a 计算仿射椭圆上的点，仅用于 Arc 曲线段。
func (s CurveSegment) Point(a float64) entity.Point2 {
	return entity.Point2{X: s.C.X + s.U.X*math.Cos(a) + s.V.X*math.Sin(a), Y: s.C.Y + s.U.Y*math.Cos(a) + s.V.Y*math.Sin(a)}
}

// Strokes 按图纸单位容差 tol 将曲线离散为描边线段，非正容差使用 0.01。
// 细分达到预算上限时向接收者的 Warnings 添加说明，此时不保证达到目标容差。
func (g *Geometry) Strokes(tol float64) []Stroke {
	if tol <= 0 {
		tol = .01
	}
	var out []Stroke
	for _, s := range g.Segments {
		if s.Cubic {
			strokes, limited := cubicStrokesBounded(s, tol)
			if limited {
				const warning = "cubic subdivision budget exceeded; requested tolerance not guaranteed"
				if len(g.Warnings) == 0 || g.Warnings[len(g.Warnings)-1] != warning {
					g.Warnings = append(g.Warnings, warning)
				}
			}
			out = append(out, strokes...)
			continue
		}
		n := 1
		if s.Arc {
			r := math.Hypot(s.U.X, s.U.Y) + math.Hypot(s.V.X, s.V.Y)
			step := 2 * math.Acos(math.Max(-1, math.Min(1, 1-tol/math.Max(r, tol))))
			if step < 1e-6 {
				step = 1e-6
			}
			n = int(math.Ceil(math.Abs(s.Sweep) / step))
			if n < 2 {
				n = 2
			}
			if n > 65536 {
				if len(g.Warnings) == 0 || g.Warnings[len(g.Warnings)-1] != "ellipse subdivision budget exceeded" {
					g.Warnings = append(g.Warnings, "ellipse subdivision budget exceeded")
				}
				n = 65536
			}
		}
		prev := s.P0
		for j := 1; j <= n; j++ {
			p := s.P1
			if j < n {
				p = s.Point(s.Start + s.Sweep*float64(j)/float64(n))
			}
			out = append(out, Stroke{prev.X, prev.Y, p.X, p.Y})
			prev = p
		}
	}
	return out
}

// Bounds 计算端点及曲线内部极值共同确定的包围盒，不依赖离散精度。
// 没有曲线段时返回未扩展的无效包围盒。
func (g *Geometry) Bounds() Box2 {
	b := Box2{MinX: math.Inf(1), MinY: math.Inf(1), MaxX: math.Inf(-1), MaxY: math.Inf(-1)}
	for _, s := range g.Segments {
		b.Extend(s.P0.X, s.P0.Y)
		b.Extend(s.P1.X, s.P1.Y)
		if s.Cubic {
			for _, u := range cubicExtrema(s) {
				p := s.CubicPoint(u)
				b.Extend(p.X, p.Y)
			}
		}
		if s.Arc {
			for _, a := range []float64{math.Atan2(s.V.X, s.U.X), math.Atan2(s.V.X, s.U.X) + math.Pi, math.Atan2(s.V.Y, s.U.Y), math.Atan2(s.V.Y, s.U.Y) + math.Pi} {
				if inArc(a, s.Start, s.Sweep) {
					p := s.Point(a)
					b.Extend(p.X, p.Y)
				}
			}
		}
	}
	return b
}

// inArc 判断弧度参数 a 是否落在从 start 开始、带方向的 sweep 弧段内。
// 跨零点与整圆按周期处理，端点比较保留浮点容差。
func inArc(a, start, sweep float64) bool {
	if math.Abs(sweep) >= 2*math.Pi-1e-10 {
		return true
	}
	d := a - start
	if sweep < 0 {
		d = -d
	}
	d = math.Mod(d, 2*math.Pi)
	if d < 0 {
		d += 2 * math.Pi
	}
	return d <= math.Abs(sweep)+1e-10
}

// arcSegment 将中心和两个轴向量应用变换 t，构造保留解析表示的椭圆弧段。
// start 和 sweep 使用弧度；非均匀缩放与镜像包含在变换后的轴向量中。
func arcSegment(c, u, v entity.Point2, start, sweep float64, t Xform) CurveSegment {
	p := t.Apply(c)
	pu := t.Apply(entity.Point2{X: c.X + u.X, Y: c.Y + u.Y})
	pv := t.Apply(entity.Point2{X: c.X + v.X, Y: c.Y + v.Y})
	s := CurveSegment{C: p, U: entity.Point2{X: pu.X - p.X, Y: pu.Y - p.Y}, V: entity.Point2{X: pv.X - p.X, Y: pv.Y - p.Y}, Start: start, Sweep: sweep, Arc: true}
	s.P0 = s.Point(start)
	s.P1 = s.Point(start + sweep)
	return s
}

// lineSegment 变换两个端点并构造直线段。
func lineSegment(a, b entity.Point2, t Xform) CurveSegment {
	return CurveSegment{P0: t.Apply(a), P1: t.Apply(b)}
}

// bulgeSegment 由两端点和 bulge 值构造有向圆弧，再应用仿射变换。
// 接近零的 bulge 或重合端点按直线处理，输出端点保持与输入变换结果一致。
func bulgeSegment(a, b entity.Point2, v float64, t Xform) CurveSegment {
	if math.Abs(v) < 1e-12 {
		return lineSegment(a, b, t)
	}
	dx, dy := b.X-a.X, b.Y-a.Y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return lineSegment(a, b, t)
	}
	off := l * (1 - v*v) / (4 * v)
	c := entity.Point2{X: (a.X+b.X)/2 - dy/l*off, Y: (a.Y+b.Y)/2 + dx/l*off}
	r := l * (1 + v*v) / (4 * math.Abs(v))
	s := arcSegment(c, entity.Point2{X: r}, entity.Point2{Y: r}, math.Atan2(a.Y-c.Y, a.X-c.X), 4*math.Atan(v), t)
	s.P0 = t.Apply(a)
	s.P1 = t.Apply(b)
	return s
}

// arcSpan 把起止弧度转换为 (0, 2π] 范围的正向扫角；重合角视为整圆。
func arcSpan(a, b float64) float64 {
	d := b - a
	if math.Abs(d) >= 2*math.Pi-1e-10 {
		return 2 * math.Pi
	}
	d = math.Mod(d, 2*math.Pi)
	if d <= 0 {
		d += 2 * math.Pi
	}
	return d
}

// entityGeometry 为支持的圆、圆弧、椭圆、多段线和样条生成世界坐标几何。
// 细分近似和未还原的宽度记入 Warnings；不支持的类型或不足的顶点数据返回 nil。
func (ts *Tessellator) entityGeometry(ent any, t Xform) *Geometry {
	g := &Geometry{}
	switch e := ent.(type) {
	case *entity.EntCircle:
		g.Closed = true
		g.Segments = append(g.Segments, arcSegment(entity.Point2{X: e.Center.X, Y: e.Center.Y}, entity.Point2{X: e.Radius}, entity.Point2{Y: e.Radius}, 0, 2*math.Pi, t))
	case *entity.EntArc:
		g.Segments = append(g.Segments, arcSegment(entity.Point2{X: e.Center.X, Y: e.Center.Y}, entity.Point2{X: e.Radius}, entity.Point2{Y: e.Radius}, e.AngleStart, arcSpan(e.AngleStart, e.AngleEnd), t))
	case *entity.EntEllipse:
		// ELLIPSE center and major axis are WCS, unlike ARC/CIRCLE OCS coordinates.
		f := ocsFrame(e.Extrusion).SpatialMatrix()
		n := entity.Point3{X: f[2], Y: f[6], Z: f[10]}
		u := e.MajorAxis
		v := entity.Point3{X: (n.Y*u.Z - n.Z*u.Y) * e.Ratio, Y: (n.Z*u.X - n.X*u.Z) * e.Ratio, Z: (n.X*u.Y - n.Y*u.X) * e.Ratio}
		c := t.Project(e.Center)
		pu := t.Project(entity.Point3{X: e.Center.X + u.X, Y: e.Center.Y + u.Y, Z: e.Center.Z + u.Z})
		pv := t.Project(entity.Point3{X: e.Center.X + v.X, Y: e.Center.Y + v.Y, Z: e.Center.Z + v.Z})
		seg := CurveSegment{C: c, U: entity.Point2{X: pu.X - c.X, Y: pu.Y - c.Y}, V: entity.Point2{X: pv.X - c.X, Y: pv.Y - c.Y}, Start: e.StartAng, Sweep: arcSpan(e.StartAng, e.EndAng), Arc: true}
		seg.P0 = seg.Point(seg.Start)
		seg.P1 = seg.Point(seg.Start + seg.Sweep)
		g.Segments = append(g.Segments, seg)

	case *entity.EntLwPolyline:
		g.Closed = e.Flags&512 != 0
		g.ResetDashEachSegment = e.Flags&256 == 0
		n := len(e.Vertices)
		if n < 2 {
			return nil
		}
		count := n - 1
		if e.Flags&512 != 0 && !e.IsClosedByGeometry() {
			count = n
		}
		for i := 0; i < count; i++ {
			b := 0.0
			if i < len(e.Bulges) {
				b = e.Bulges[i]
			}
			g.Segments = append(g.Segments, bulgeSegment(e.Vertices[i], e.Vertices[(i+1)%n], b, t))
		}
		if e.ConstWidth != 0 || len(e.Widths) > 0 {
			g.Warnings = append(g.Warnings, "polyline geometric widths rendered as centerlines")
		}
	case *entity.EntPolyline2d:
		g.Closed = e.Flags&1 != 0
		g.ResetDashEachSegment = e.Flags&128 == 0
		var vs []*entity.EntVertex2d
		for _, h := range e.OwnedHandles {
			if v := ts.vertex2d[h]; v != nil {
				vs = append(vs, v)
			}
		}
		n := len(vs)
		count := n - 1
		if e.Flags&1 != 0 {
			count = n
		}
		for i := 0; i < count; i++ {
			a, b := vs[i], vs[(i+1)%n]
			g.Segments = append(g.Segments, bulgeSegment(entity.Point2{X: a.Position.X, Y: a.Position.Y}, entity.Point2{X: b.Position.X, Y: b.Position.Y}, a.Bulge, t))
		}
		if e.WidthStart != 0 || e.WidthEnd != 0 {
			g.Warnings = append(g.Warnings, "polyline geometric widths rendered as centerlines")
		}
	case *entity.EntSpline:
		if len(e.ControlPoints) == 0 && len(e.FitPoints) >= 2 {
			return fitSplineGeometry(e, t, ts.Tolerance)
		}
		degree := int(e.Degree)
		n := len(e.ControlPoints)
		if degree < 1 || degree >= n || len(e.Knots) < n+degree+1 {
			g.Warnings = append(g.Warnings, "spline fit data uses Catmull-Rom approximation")
			for _, s := range TessSpline(e, t) {
				g.Segments = append(g.Segments, CurveSegment{P0: entity.Point2{X: s.X1, Y: s.Y1}, P1: entity.Point2{X: s.X2, Y: s.Y2}})
			}
			return g
		}
		tol := ts.Tolerance
		if tol <= 0 {
			tol = .01
		}
		for k := degree; k < n; k++ {
			lo, hi := e.Knots[k], e.Knots[k+1]
			if hi <= lo {
				continue
			}
			at := func(u float64) entity.Point2 {
				q := entity.DeBoor(e.ControlPoints, e.Weights, e.Knots, degree, k, u)
				return t.Project(q)
			}
			var split func(float64, float64, entity.Point2, entity.Point2, int)
			split = func(a, b float64, p, q entity.Point2, depth int) {
				mid := (a + b) / 2
				m := at(mid)
				err := pointLineDistance(m, p, q)
				err = math.Max(err, pointLineDistance(at(a+(b-a)/4), p, q))
				err = math.Max(err, pointLineDistance(at(a+3*(b-a)/4), p, q))
				if err <= tol || depth >= 18 || len(g.Segments) >= 65536 {
					if err > tol {
						g.Warnings = append(g.Warnings, "spline subdivision budget exceeded")
					}
					g.Segments = append(g.Segments, CurveSegment{P0: p, P1: q})
					return
				}
				split(a, mid, p, m, depth+1)
				split(mid, b, m, q, depth+1)
			}
			split(lo, hi, at(lo), at(hi), 0)
		}
	default:
		return nil
	}
	return g
}

// pointLineDistance 计算点 p 到闭合线段 ab 的最短距离；退化线段按单点计算。
func pointLineDistance(p, a, b entity.Point2) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	den := dx*dx + dy*dy
	if den == 0 {
		return math.Hypot(p.X-a.X, p.Y-a.Y)
	}
	v := math.Max(0, math.Min(1, ((p.X-a.X)*dx+(p.Y-a.Y)*dy)/den))
	return math.Hypot(p.X-a.X-v*dx, p.Y-a.Y-v*dy)
}
