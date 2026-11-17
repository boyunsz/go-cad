package drawing

import (
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// readUnicodeEscape reads one CAD UTF-16 escape, or a paired surrogate.
// Invalid sequences remain literal rather than becoming replacement characters.
func readUnicodeEscape(s string) (rune, int, bool) {
	unit := func(s string) (rune, bool) {
		if len(s) < 7 || s[:3] != `\U+` {
			return 0, false
		}
		v, err := strconv.ParseUint(s[3:7], 16, 16)
		return rune(v), err == nil
	}
	r, ok := unit(s)
	if !ok {
		return 0, 0, false
	}
	if r >= 0xD800 && r <= 0xDBFF {
		lo, ok := unit(s[7:])
		if !ok || lo < 0xDC00 || lo > 0xDFFF {
			return 0, 0, false
		}
		return utf16.DecodeRune(r, lo), 14, true
	}
	if utf16.IsSurrogate(r) {
		return 0, 0, false
	}
	return r, 7, true
}

// readTextEscape decodes a display character without changing the stored DWG
// string. MIF uses a codepage selector followed by exactly four hex digits;
// following dimension digits must never be consumed as part of the escape.
func readTextEscape(s string) (rune, int, bool) {
	if r, n, ok := readUnicodeEscape(s); ok {
		return r, n, true
	}
	if len(s) < 8 || s[:3] != `\M+` {
		return 0, 0, false
	}
	var enc encoding.Encoding
	switch s[3] {
	case '1':
		enc = japanese.ShiftJIS
	case '2':
		enc = traditionalchinese.Big5
	case '3':
		enc = korean.EUCKR
	case '5':
		enc = simplifiedchinese.GBK
	default: // Johab (4) and unknown encodings remain visible for diagnosis.
		return 0, 0, false
	}
	v, err := strconv.ParseUint(s[4:8], 16, 16)
	if err != nil {
		return 0, 0, false
	}
	b := []byte{byte(v >> 8), byte(v)}
	if v <= 0xff {
		b = b[1:]
	}
	decoded, err := enc.NewDecoder().Bytes(b)
	r, size := utf8.DecodeRune(decoded)
	if err != nil || r == utf8.RuneError || r < 0x20 || r == 0x7f || size != len(decoded) {
		return 0, 0, false
	}
	return r, 8, true
}

// expandUnicodeEscapes 扫描并展开文字中的 Unicode/MIF 显示转义，保留不能解码的源字节。
// TEXT 和 ATTRIB 可复用此函数，无需启用 MTEXT 格式解析。
func expandUnicodeEscapes(s string) string {
	if !strings.Contains(s, `\U+`) && !strings.Contains(s, `\M+`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '\\' {
			b.WriteByte('\\')
			i += 2
		} else if r, n, ok := readDisplayEscape(s[i:]); ok {
			b.WriteString(r)
			i += n
		} else {
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}
