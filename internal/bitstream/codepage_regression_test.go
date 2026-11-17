package bitstream

import (
	"encoding/hex"
	"testing"
	"unicode/utf8"
)

// TestDWGCodepage41TraditionalChinese 验证 DWG 码页 41 按 Big5 解码繁体中文。
func TestDWGCodepage41TraditionalChinese(t *testing.T) {
	raw, err := hex.DecodeString("a7deb34ead6ea844")
	if err != nil {
		t.Fatal(err)
	}
	got := DecodeCodepage(raw, 41)
	if !utf8.ValidString(got) || got != "技術要求" {
		t.Fatalf("DWG codepage 41: got %q, want 技術要求", got)
	}
}
