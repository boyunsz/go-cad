package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"math"
)

// HatchBoundary uses the original edge definitions instead of the decoder's
// fixed sample count. The returned points remain in pattern-local space.
func (ts *Tessellator) HatchBoundary(p entity.HatchPath, t Xform) ([]entity.Point2, []string) {
	g := &Geometry{}
	if len(p.PolyVerts) > 1 {
		n := len(p.PolyVerts)
		for i, a := range p.PolyVerts {
			b := p.PolyVerts[(i+1)%n]
			g.Segments = append(g.Segments, bulgeSegment(a.P, b.P, a.Bulge, IdentityXform()))
		}
	} else if len(p.Segs) > 0 {
		for _, s := range p.Segs {
			switch s.CurveType {
			case 1:
				g.Segments = append(g.Segments, lineSegment(s.First, s.Second, IdentityXform()))
			case 2, 3:
				u, v := entity.Point2{X: s.Radius}, entity.Point2{Y: s.Radius}
				if s.CurveType == 3 {
					u = s.Endpoint
					v = entity.Point2{X: -u.Y * s.Ratio, Y: u.X * s.Ratio}
				}
				start, span := s.StartAng, arcSpan(s.StartAng, s.EndAng)
				if !s.Ccw {
					start = -start
					span = -span
				}
				g.Segments = append(g.Segments, arcSegment(s.Center, u, v, start, span, IdentityXform()))
			case 4:
				sp := &entity.EntSpline{Degree: s.Degree, Knots: s.Knots, Weights: s.Weights}
				for _, p := range s.Ctrl {
					sp.ControlPoints = append(sp.ControlPoints, entity.Point3{X: p.X, Y: p.Y})
				}
				for _, p := range s.FitPts {
					sp.FitPoints = append(sp.FitPoints, entity.Point3{X: p.X, Y: p.Y})
				}
				sg := ts.entityGeometry(sp, IdentityXform())
				g.Segments = append(g.Segments, sg.Segments...)
				g.Warnings = append(g.Warnings, sg.Warnings...)
			}
		}
	}
	if len(g.Segments) == 0 {
		return append([]entity.Point2(nil), p.Points...), nil
	}
	m := t.Matrix()
	bound := math.Hypot(m[0], m[1]) + math.Hypot(m[2], m[3])
	if bound < 1 {
		bound = 1
	}
	tol := ts.Tolerance / bound
	var pts []entity.Point2
	for _, s := range g.Strokes(tol) {
		if len(pts) == 0 {
			pts = append(pts, entity.Point2{X: s.X1, Y: s.Y1})
		}
		pts = append(pts, entity.Point2{X: s.X2, Y: s.Y2})
	}
	return entity.ClosePath(pts), g.Warnings
}
