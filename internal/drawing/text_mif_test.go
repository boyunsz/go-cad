package drawing

import "testing"

// TestLegacyMIFDisplayEscapes 验证旧式 MIF 转义在普通文字、MTEXT 和文本提取中一致显示。
func TestLegacyMIFDisplayEscapes(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`\M+5D28E\M+5B8F1`, "規格"},
		{`\M+5D6D0\M+5C1BA`, "中梁"},
		{`8.9\M+5A1C00.1`, "8.9±0.1"},
		{`\M+2A4A4`, "中"},
		{`\M+182A0`, "あ"},
		{`\M+3B0A1`, "가"},
		{`\U+5C3A\M+5A1C0`, "尺±"},
		{`\\M+5A1C0`, `\M+5A1C0`},
		{`\M+50088D`, "圖"},
		{`\M+5008C`, `\M+5008C`},
		{`\M+5ZZZZ`, `\M+5ZZZZ`},
		{`\M+5A1`, `\M+5A1`},
		{`\M+4ABCD`, `\M+4ABCD`},
	} {
		if got := PlainDisplay(tc.raw); got != tc.want {
			t.Errorf("PlainDisplay(%q)=%q want %q", tc.raw, got, tc.want)
		}
		var got string
		for _, r := range ParseTextRuns(tc.raw, true) {
			got += r.Text
		}
		if got != tc.want {
			t.Errorf("MTEXT(%q)=%q want %q", tc.raw, got, tc.want)
		}
		if got := StripMTextFormat(tc.raw); got != tc.want {
			t.Errorf("extraction(%q)=%q want %q", tc.raw, got, tc.want)
		}
	}
}
