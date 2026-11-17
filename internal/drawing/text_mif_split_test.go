package drawing

import "testing"

// TestSplitMIFByteSequences 验证拆分 MIF 字节在文本提取、格式片段和普通显示中得到一致解码。
func TestSplitMIFByteSequences(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`繪\M+50088D﹕`, "繪圖﹕"},
		{`\M+5B8FC\M+5B8C4\M+50083\M+5C8C8\M+5DD00`, "更改內容"},
		{`第1\M+5008C\M+5D328`, "第1層("},
		{`\M+5008C\M+5008F核﹕`, "審核﹕"},
		{`片\M+5008F\M+5009D﹕`, "片彎﹕"},
		{`\M+50086挝\M+5BB00`, "單位"},
		{`\M+50086\U+631d\M+5BB00`, "單位"},
		{`膠前圈\M+50089罕橇\M+5BA00`, "膠前圈壓鼻梁"},
		{`\M+5008F溺R架背面水平看湮斗盒位置:`, "從鏡架背面水平看湮斗盒位置:"},
		{`{\H0.7x;繪\M+50088D}\P片\M+5008F\M+5009D`, "繪圖\n片彎"},
	} {
		if got := StripMTextFormat(tc.raw); got != tc.want {
			t.Errorf("extraction %q = %q, want %q", tc.raw, got, tc.want)
		}
		got := ""
		for _, r := range ParseTextRuns(tc.raw, true) {
			if r.Newline {
				got += "\n"
			}
			got += r.Text
		}
		if got != tc.want {
			t.Errorf("runs %q = %q, want %q", tc.raw, got, tc.want)
		}
		if tc.raw[0] != '{' {
			if got := PlainDisplay(tc.raw); got != tc.want {
				t.Errorf("plain %q = %q, want %q", tc.raw, got, tc.want)
			}
		}
	}
}

// TestSplitMIFDoesNotGuessIncompleteBytes 验证残缺、歧义或跨码页的 MIF 序列保持原样，不猜测替换字符。
func TestSplitMIFDoesNotGuessIncompleteBytes(t *testing.T) {
	for _, raw := range []string{`\M+5008C`, `\M+5BB00`, `\M+50088 `, `\M+50088\P圖`, `\M+50088{D}`, `\M+50088😀`} {
		if got := PlainDisplay(raw); got != raw {
			t.Errorf("changed invalid input %q to %q", raw, got)
		}
	}
	if got := PlainDisplay(`\M+50088\M+2A4A4`); got != `\M+50088中` {
		t.Errorf("crossed codepage boundary: %q", got)
	}
	if got := PlainDisplay(`\\M+50088D`); got != `\M+50088D` {
		t.Fatalf("literal escaped: %q", got)
	}
}
