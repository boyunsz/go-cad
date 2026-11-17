package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"math"
)

// mix2 按参数 u 对二维点 a、b 进行线性插值，不限制 u 的范围。
func mix2(a, b entity.Point2, u float64) entity.Point2 {
	return entity.Point2{X: a.X + (b.X-a.X)*u, Y: a.Y + (b.Y-a.Y)*u}
}

// CubicPoint 用 de Casteljau 算法计算三次 Bézier 曲线在参数 u 处的位置。
// 调用方通常传入 [0, 1] 内的参数；函数不执行截断。
func (s CurveSegment) CubicPoint(u float64) entity.Point2 {
	a, b, c := mix2(s.P0, s.C1, u), mix2(s.C1, s.C2, u), mix2(s.C2, s.P1, u)
	return mix2(mix2(a, b, u), mix2(b, c, u), u)
}

// cubicExtrema 求三次 Bézier 曲线 X、Y 分量在开区间 (0, 1) 内的驻点参数。
// 结果可包含重复参数，供解析包围盒计算使用。
func cubicExtrema(s CurveSegment) []float64 {
	var out []float64
	for _, v := range [][4]float64{{s.P0.X, s.C1.X, s.C2.X, s.P1.X}, {s.P0.Y, s.C1.Y, s.C2.Y, s.P1.Y}} {
		a, b, c := -v[0]+3*v[1]-3*v[2]+v[3], 2*(v[0]-2*v[1]+v[2]), v[1]-v[0]
		add := func(u float64) {
			if u > 0 && u < 1 {
				out = append(out, u)
			}
		}
		if math.Abs(a) < 1e-15 {
			if math.Abs(b) > 1e-15 {
				add(-c / b)
			}
			continue
		}
		d := b*b - 4*a*c
		if d >= 0 {
			add((-b + math.Sqrt(d)) / (2 * a))
			add((-b - math.Sqrt(d)) / (2 * a))
		}
	}
	return out
}

// cubicStrokes 按图纸单位容差离散三次 Bézier 曲线，返回线段列表。
// 需要判断细分预算是否耗尽时，应调用 cubicStrokesBounded。
func cubicStrokes(s CurveSegment, tol float64) []Stroke {
	out, _ := cubicStrokesBounded(s, tol)
	return out
}

// cubicStrokesBounded 以控制点到弦的距离为误差界，自适应细分三次 Bézier 曲线。
// 递归深度上限为 16；第二个返回值为 true 时，部分线段未达到 tol 要求。
func cubicStrokesBounded(s CurveSegment, tol float64) ([]Stroke, bool) {
	var out []Stroke
	limited := false
	var split func(CurveSegment, int)
	split = func(c CurveSegment, depth int) {
		// Convex hull distance to the chord bounds every point of the Bezier segment.
		flat := math.Max(pointLineDistance(c.C1, c.P0, c.P1), pointLineDistance(c.C2, c.P0, c.P1)) <= tol
		// At most 65536 segments per cubic; never imply the requested error was
		// reached when this resource bound stops subdivision.
		if flat || depth >= 16 {
			limited = limited || !flat
			out = append(out, Stroke{c.P0.X, c.P0.Y, c.P1.X, c.P1.Y})
			return
		}
		a, b, d := mix2(c.P0, c.C1, .5), mix2(c.C1, c.C2, .5), mix2(c.C2, c.P1, .5)
		e, f := mix2(a, b, .5), mix2(b, d, .5)
		m := mix2(e, f, .5)
		split(CurveSegment{P0: c.P0, C1: a, C2: e, P1: m, Cubic: true}, depth+1)
		split(CurveSegment{P0: m, C1: f, C2: d, P1: c.P1, Cubic: true}, depth+1)
	}
	split(s, 0)
	return out, limited
}

// fitSplineGeometry 按弦长参数构造通过拟合点的 C2 连续三次插值曲线，并应用变换 t。
// 无端点切线时使用自然边界；已存切线仅约束方向。缺失原控制框架、
// 重复点及闭合边界的近似会记录在 Warnings 中。tol 当前不参与插值求解。
func fitSplineGeometry(e *entity.EntSpline, t Xform, tol float64) *Geometry {
	g := &Geometry{}
	pts := make([]entity.Point3, 0, len(e.FitPoints))
	duplicate := false
	for _, p := range e.FitPoints {
		if len(pts) > 0 {
			a := pts[len(pts)-1]
			if math.Hypot(math.Hypot(p.X-a.X, p.Y-a.Y), p.Z-a.Z) < 1e-12 {
				duplicate = true
				continue
			}
		}
		pts = append(pts, p)
	}
	if duplicate {
		g.Warnings = append(g.Warnings, "duplicate fit points; zero-length spans omitted")
	}
	n := len(pts)
	if n < 2 {
		return g
	}
	h := make([]float64, n-1)
	for i := range h {
		a, b := pts[i], pts[i+1]
		h[i] = math.Sqrt((b.X-a.X)*(b.X-a.X) + (b.Y-a.Y)*(b.Y-a.Y) + (b.Z-a.Z)*(b.Z-a.Z))
	}
	a, b := e.FitTangents()
	norm := func(p entity.Point3) float64 { return math.Sqrt(p.X*p.X + p.Y*p.Y + p.Z*p.Z) }
	if e.Closed || e.Periodic {
		g.Warnings = append(g.Warnings, "closed/periodic fit spline currently uses endpoint constraints")
	}
	g.Warnings = append(g.Warnings, "fit spline reconstructed with chord-length C2 interpolation and stored endpoint tangent directions; original control frame unavailable")
	values := func(axis int, p entity.Point3) float64 {
		if axis == 0 {
			return p.X
		}
		if axis == 1 {
			return p.Y
		}
		return p.Z
	}
	var second [3][]float64
	for axis := 0; axis < 3; axis++ {
		lo, di, up, rhs := make([]float64, n), make([]float64, n), make([]float64, n), make([]float64, n)
		slope := func(i int) float64 { return (values(axis, pts[i+1]) - values(axis, pts[i])) / h[i] }
		di[0], di[n-1] = 1, 1
		if norm(a) > 1e-12 {
			di[0] = 2 * h[0]
			up[0] = h[0]
			rhs[0] = 6 * (slope(0) - values(axis, a)/norm(a))
		}
		if norm(b) > 1e-12 {
			lo[n-1] = h[n-2]
			di[n-1] = 2 * h[n-2]
			rhs[n-1] = 6 * (values(axis, b)/norm(b) - slope(n-2))
		}
		for i := 1; i < n-1; i++ {
			lo[i] = h[i-1]
			di[i] = 2 * (h[i-1] + h[i])
			up[i] = h[i]
			rhs[i] = 6 * (slope(i) - slope(i-1))
		}
		for i := 1; i < n; i++ {
			f := lo[i] / di[i-1]
			di[i] -= f * up[i-1]
			rhs[i] -= f * rhs[i-1]
		}
		for i := n - 1; i >= 0; i-- {
			if i < n-1 {
				rhs[i] -= up[i] * rhs[i+1]
			}
			rhs[i] /= di[i]
		}
		second[axis] = rhs
	}
	for i := 0; i < n-1; i++ {
		var c1, c2 [3]float64
		for axis := 0; axis < 3; axis++ {
			p, q := values(axis, pts[i]), values(axis, pts[i+1])
			m, k := second[axis][i], second[axis][i+1]
			c1[axis] = p + (q-p)/3 - h[i]*h[i]*(2*m+k)/18
			c2[axis] = q - (q-p)/3 - h[i]*h[i]*(m+2*k)/18
		}
		g.Segments = append(g.Segments, CurveSegment{P0: t.Project(pts[i]), P1: t.Project(pts[i+1]), C1: t.Project(entity.Point3{X: c1[0], Y: c1[1], Z: c1[2]}), C2: t.Project(entity.Point3{X: c2[0], Y: c2[1], Z: c2[2]}), Cubic: true})
	}
	return g
}
