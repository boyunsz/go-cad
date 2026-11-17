package render

import (
	"runtime"
	"testing"
)

// TestMacSystemFontSupportsDrawingChinese 验证可用的 macOS 字体包含图纸所需的中文字符，避免退化为 ASCII 字体。
func TestMacSystemFontSupportsDrawingChinese(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS font discovery")
	}
	f, fallback := loadFontChain("")
	if f == nil {
		t.Fatal("no macOS font loaded; PNG would fall back to ASCII-only text")
	}
	fonts := append(fallback[:len(fallback):len(fallback)], f)
	for _, r := range "技術要求左右脾外侧装电镀片後噴膠槳編號釘鉸鏡螺絲±°φ" {
		found := false
		for _, face := range fonts {
			g, err := face.GlyphIndex(nil, r)
			if err == nil && g != 0 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing drawing glyph %c", r)
		}
	}
}
