package render

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/boyunsz/go-cad/internal/drawing"
	"image/color"
)

// TestSVGSmallCoordinatesPreserved 验证小数坐标序列化保留微小几何偏移。
func TestSVGSmallCoordinatesPreserved(t *testing.T) {
	for _, v := range []int64{0, 1, 5, 10, 40, 105, 1005, -1, -5, -40, -1005} {
		encoded := string(appendFixed(nil, v, 3))
		got, err := strconv.ParseFloat(encoded, 64)
		if err != nil || math.Abs(got-float64(v)/1000) > 1e-12 {
			t.Errorf("SVG coordinate %d milli-units encoded as %q; want %g", v, encoded, float64(v)/1000)
		}
	}
}

// TestSVGRelativeSegmentsDoNotLosePrecision 验证相对线段坐标写出不丢失小数精度。
func TestSVGRelativeSegmentsDoNotLosePrecision(t *testing.T) {
	for _, v := range []int64{1005, -1005, 100005, -100005, 123456789} {
		encoded := strings.TrimSpace(string(appendSepNum(nil, v)))
		got, err := strconv.ParseFloat(encoded, 64)
		if err != nil || math.Abs(got-float64(v)/1000) > 1e-9 {
			t.Errorf("relative coordinate lost precision: %d -> %s", v, encoded)
		}
	}
}

// TestSVGEmitterKeepsSmallStroke 验证 SVG 写出器不会吸附或改写微小描边端点。
func TestSVGEmitterKeepsSmallStroke(t *testing.T) {
	em := newSVGEmitter(drawing.Box2{MinX: 0, MinY: 0, MaxX: 1, MaxY: 1}, 0.01, color.RGBA{255, 255, 255, 255}, nil)
	em.emit(&drawing.Document{}, &drawing.Primitive{Kind: 0, Strokes: []drawing.Stroke{{X1: 0, Y1: 0, X2: 0.005, Y2: 0.04}}}, true)
	svg := string(em.finish(1000, 1000, 1, 1))
	if !strings.Contains(svg, "l0.005-0.04") {
		t.Fatalf("SVG changed the actual stroke coordinates: %s", svg)
	}
}
