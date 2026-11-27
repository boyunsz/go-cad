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

// TestDecodeCodepageASCIIFastPathBoundaries 验证空输入、纯 ASCII 快路径及 0x7F/0x80 边界字节的正确解码。
func TestDecodeCodepageASCIIFastPathBoundaries(t *testing.T) {
	if got := DecodeCodepage(nil, 31); got != "" {
		t.Fatalf("nil input: got %q", got)
	}
	ascii := []byte("Layer_0123-ABC\x7f")
	for _, cp := range []uint16{0, 1, 24, 30, 31, 39, 41} {
		if got := DecodeCodepage(ascii, cp); got != string(ascii) {
			t.Fatalf("cp=%d ascii mismatch: got %q", cp, got)
		}
	}
	// 0xB8 0xA3 = 福 in GBK (non-ASCII boundary >= 0x80)
	gbk, _ := hex.DecodeString("41b8a342")
	if got := DecodeCodepage(gbk, 31); got != "A福B" {
		t.Fatalf("mixed ASCII+GBK: got %q, want A福B", got)
	}
}
