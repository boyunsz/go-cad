package drawing

import (
	"bytes"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// readDisplayEscape also handles legacy MIF streams that store a GBK character
// as separate bytes, e.g. \M+50088 followed by literal D (0x88, 0x44 = 圖).
// Recovery is local to an incomplete byte escape. Ordinary Unicode text and
// complete MIF characters keep their existing interpretation.
func readDisplayEscape(s string) (string, int, bool) {
	if r, n, ok := readTextEscape(s); ok {
		return string(r), n, true
	}
	return readSplitMIF(s)
}

// mifGBKBytes 从字符串开头的 MIF 码页 5 转义提取一个或两个编码字节。
// 兼容单字节的高位或低位补零；前缀、十六进制值无效或全零时返回 false。
func mifGBKBytes(s string) ([]byte, bool) {
	if len(s) < 8 || s[:4] != `\M+5` {
		return nil, false
	}
	v, err := strconv.ParseUint(s[4:8], 16, 16)
	if err != nil || v == 0 {
		return nil, false
	}
	hi, lo := byte(v>>8), byte(v)
	// Some old exports pad a single byte on either side with zero.
	if hi == 0 {
		return []byte{lo}, true
	}
	if lo == 0 {
		return []byte{hi}, true
	}
	return []byte{hi, lo}, true
}

// readSplitMIF 恢复从不完整 MIF 字节开始、跨转义或字面字符存储的 GBK 文本。
// 返回解码文本、消费的源字节数和成功标志；仅接受可双向验证的完整序列，
// 遇到格式控制、歧义或残缺字节时返回空结果，保留调用方的原始文本。
func readSplitMIF(s string) (string, int, bool) {
	first, ok := mifGBKBytes(s)
	if !ok || len(first) != 1 || first[0] < 0x81 || first[0] > 0xfe {
		return "", 0, false
	}
	raw := append([]byte(nil), first...)
	pending := true
	for i := 8; i < len(s); {
		var part []byte
		var n int
		if strings.HasPrefix(s[i:], `\M+5`) {
			part, ok = mifGBKBytes(s[i:])
			if !ok {
				break
			}
			n = 8
		} else {
			r, size := utf8.DecodeRuneInString(s[i:])
			if r == '\\' {
				var valid bool
				r, size, valid = readUnicodeEscape(s[i:])
				if !valid {
					break // Never cross a format command or another codepage.
				}
			}
			if r <= ' ' || r == '\\' || r == '{' || r == '}' || r == utf8.RuneError {
				break
			}
			n = size
			var err error
			part, err = simplifiedchinese.GBK.NewEncoder().Bytes([]byte(string(r)))
			if err != nil {
				break
			}
			back, err := simplifiedchinese.GBK.NewDecoder().Bytes(part)
			if err != nil || string(back) != string(r) {
				break
			}
		}
		for _, b := range part {
			if pending {
				if b < 0x40 || b > 0xfe || b == 0x7f {
					return "", 0, false
				}
				pending = false
			} else if b >= 0x81 && b <= 0xfe {
				pending = true
			} else if b < 0x20 || b > 0x7e {
				return "", 0, false
			}
		}
		raw = append(raw, part...)
		i += n
		if pending {
			continue
		}
		decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(raw)
		if err != nil || bytes.ContainsRune(decoded, utf8.RuneError) || strings.ContainsAny(string(decoded), `\{}`) {
			return "", 0, false
		}
		back, err := simplifiedchinese.GBK.NewEncoder().Bytes(decoded)
		if err != nil || !bytes.Equal(back, raw) {
			return "", 0, false
		}
		return string(decoded), i, true
	}
	// Incomplete or ambiguous sequences stay visible, with source text intact.
	return "", 0, false
}
