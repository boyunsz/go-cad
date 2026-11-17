package drawing

import (
	"github.com/boyunsz/go-cad/internal/entity"
	"testing"
)

// TestMTextUnicodeEscapes 验证 MTEXT 的 Unicode 转义被还原为预期字符。
func TestMTextUnicodeEscapes(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`R30\U+00B10.1`, "R30±0.1"},
		{`\U+4FA7\U+88C5\U+7535\U+9540`, "侧装电镀"},
		{`{\H0.7x;技術要求}\P\U+7F16\U+53F7:SH-111701-A`, "技術要求\n编号:SH-111701-A"},
		{`\\U+00B1`, `\U+00B1`},
		{`\U+007B測試\U+007D`, "{測試}"},
		{`\U+D83D\U+DE00`, "😀"},
		{`\U+ZZZZ`, `\U+ZZZZ`},
		{`\U+12`, `\U+12`},
		{`\U+D83D`, `\U+D83D`},
		{`\U+DE00`, `\U+DE00`},
		{`\L技術\l要求`, "技術要求"},
		{`\{測試\}`, "{測試}"},
	} {
		if got := StripMTextFormat(tc.raw); got != tc.want {
			t.Errorf("%q -> %q; want %q", tc.raw, got, tc.want)
		}
	}
}

// TestTextEntitiesDecodeUnicodeForDisplay 验证文字和属性实体只在显示阶段解码 Unicode 转义。
func TestTextEntitiesDecodeUnicodeForDisplay(t *testing.T) {
	ts := NewTessellator(&Document{})
	for _, ent := range []any{
		&entity.EntText{Text: `R30\U+00B10.1`, Height: 2.5},
		&entity.EntAttrib{Text: `R30\U+00B10.1`, Height: 2.5},
		&entity.EntMText{Text: `R30\U+00B10.1`, TextHeight: 2.5},
	} {
		ps := ts.AppendEntity(nil, ent, IdentityXform(), 0)
		if len(ps) != 1 || ps[0].Lb.Text != "R30±0.1" {
			t.Fatalf("%T: unexpected text primitives %+v", ent, ps)
		}
	}
}
