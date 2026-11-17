package main

import (
	"bytes"
	"fmt"
	cad "github.com/boyunsz/go-cad"
	"image/png"
	"testing"
)

// TestShortPolylineArcDoesNotBecomeCircle 验证短 bulge 弧不会被错误扩大为整圆或异常大的渲染范围。
func TestShortPolylineArcDoesNotBecomeCircle(t *testing.T) {
	for _, bulge := range []float64{0.1, -0.1, 0.01, -0.01} {
		t.Run(fmt.Sprint(bulge), func(t *testing.T) {
			dxf := fmt.Sprintf("0\nSECTION\n2\nENTITIES\n0\nLWPOLYLINE\n90\n2\n70\n0\n10\n0\n20\n0\n42\n%.15g\n10\n10\n20\n0\n0\nENDSEC\n0\nEOF\n", bulge)
			doc, err := cad.ParseDXF([]byte(dxf))
			if err != nil {
				t.Fatal(err)
			}
			data, err := cad.RenderPNG(doc, cad.RenderOptions{Width: 1000})
			if err != nil {
				t.Fatal(err)
			}
			config, err := png.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			// A 10-unit chord with |bulge| <= 0.1 has sagitta <= 0.5 units.
			// It must be a shallow strip, not a nearly complete circle.
			if config.Height > 100 {
				t.Fatalf("short arc rendered as oversized circle: %dx%d (bulge %g)", config.Width, config.Height, bulge)
			}
		})
	}
}
