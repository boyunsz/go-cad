package entity

import "math"

// BulgeArcPoints samples a polyline segment in its original coordinates.
// The signed sweep is 4*atan(bulge); negative bulges stay clockwise.
// Callers apply block transforms to the returned points exactly once.
// A non-positive segments value selects a maximum angular step of 3 degrees.
func BulgeArcPoints(a, b Point2, bulge float64, segments int) []Point2 {
	if !IsFinite(a.X) || !IsFinite(a.Y) || !IsFinite(b.X) || !IsFinite(b.Y) || !IsFinite(bulge) {
		return nil
	}
	dx, dy := b.X-a.X, b.Y-a.Y
	chord := math.Hypot(dx, dy)
	if !IsFinite(chord) {
		return nil
	}
	if bulge == 0 || chord == 0 {
		return []Point2{a, b}
	}
	theta := 4 * math.Atan(bulge)
	if segments <= 0 {
		segments = int(math.Ceil(math.Abs(theta) / (3 * math.Pi / 180)))
	}
	if segments < 2 {
		segments = 2
	}
	if segments > 720 {
		segments = 720
	}
	// Include the midpoint (maximum sagitta), even for very shallow arcs.
	if segments%2 != 0 {
		segments++
	}
	ex, ey := dx/chord, dy/chord
	pts := make([]Point2, segments+1)
	pts[0], pts[segments] = a, b
	for i := 1; i < segments; i++ {
		angle := theta * float64(i) / float64(segments)
		s := math.Sin(angle)
		halfSin := math.Sin(angle / 2)
		oneMinusCos := 2 * halfSin * halfSin
		// In the chord frame the center is (chord/2, chord*(1/bulge-bulge)/4).
		// Rotate the start vector about that center. This form avoids subtracting
		// two huge center/radius values for shallow arcs and preserves major arcs.
		x := chord/2*oneMinusCos + chord/4*(s/bulge-bulge*s)
		y := chord/4*(oneMinusCos/bulge-bulge*oneMinusCos) - chord/2*s
		p := Point2{a.X + ex*x - ey*y, a.Y + ey*x + ex*y}
		if !IsFinite(p.X) || !IsFinite(p.Y) {
			return nil
		}
		pts[i] = p
	}
	return pts
}
