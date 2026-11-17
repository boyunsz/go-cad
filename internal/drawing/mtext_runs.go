package drawing

import (
	"strconv"
	"strings"
)

type TextRun struct {
	Text                   string
	VerticalAlign          int
	Bold, Italic           bool
	FontStyleSet           bool
	Height, Width, Oblique float64
	Font                   string
	Under, Over, Strike    bool
	Newline                bool
	AbsoluteHeight         bool
	AbsoluteWidth          bool
	Tracking               float64
	ColorIndex             int
	TrueColor              int64
	StackMode              byte
	Top, Bottom            string
}

// PlainDisplay 展开普通文字中的 Unicode、MIF 和百分号特殊码，保留不识别的原始序列。
// 不解析 MTEXT 的字体、段落等内联格式。
func PlainDisplay(s string) string { return expandPercentCodes(expandUnicodeEscapes(s)) }

// ParseTextRuns 将文字分解为携带字体、字号、宽度、颜色和对齐信息的格式片段。
// mtext 为 false 时仅解码显示字符；为 true 时解析嵌套格式及段落，不修改源字符串。
func ParseTextRuns(s string, mtext bool) []TextRun {
	state := TextRun{Height: 1, Width: 1, Tracking: 1, ColorIndex: -1, TrueColor: -1}
	var stack []TextRun
	var out []TextRun
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			r := state
			r.Text = expandPercentCodes(buf.String())
			out = append(out, r)
			buf.Reset()
		}
	}
	if !mtext {
		return []TextRun{{Text: PlainDisplay(s), Height: 1, Width: 1, Tracking: 1, ColorIndex: -1, TrueColor: -1}}
	}
	for i := 0; i < len(s); {
		c := s[i]
		if c == '{' {
			flush()
			stack = append(stack, state)
			i++
			continue
		}
		if c == '}' {
			flush()
			if len(stack) > 0 {
				state = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
			}
			i++
			continue
		}
		if c != '\\' || i+1 >= len(s) {
			buf.WriteByte(c)
			i++
			continue
		}
		if r, n, ok := readDisplayEscape(s[i:]); ok {
			buf.WriteString(r)
			i += n
			continue
		}
		cmd := s[i+1]
		i += 2
		switch cmd {
		case '\\', '{', '}':
			buf.WriteByte(cmd)
		case '~':
			buf.WriteRune('\u00a0')
		case 'P', 'X':
			flush()
			out = append(out, TextRun{Newline: true})
		case 'L', 'l', 'O', 'o', 'K', 'k':
			flush()
			switch cmd {
			case 'L':
				state.Under = true
			case 'l':
				state.Under = false
			case 'O':
				state.Over = true
			case 'o':
				state.Over = false
			case 'K':
				state.Strike = true
			case 'k':
				state.Strike = false
			}
		case 'H', 'W', 'Q', 'F', 'f', 'S', 'A', 'C', 'c', 'T', 'p':
			j := strings.IndexByte(s[i:], ';')
			if j < 0 {
				buf.WriteByte('\\')
				buf.WriteByte(cmd)
				continue
			}
			flush()
			arg := s[i : i+j]
			i += j + 1
			switch cmd {
			case 'H':
				v, e := strconv.ParseFloat(strings.TrimSuffix(arg, "x"), 64)
				if e == nil && v > 0 && v < 100 {
					state.Height = v
					state.AbsoluteHeight = !strings.HasSuffix(arg, "x")
				}
			case 'W':
				v, e := strconv.ParseFloat(strings.TrimSuffix(arg, "x"), 64)
				if e == nil && v > 0 && v < 100 {
					state.Width = v
					state.AbsoluteWidth = !strings.HasSuffix(arg, "x")
				}
			case 'T':
				v, e := strconv.ParseFloat(arg, 64)
				if e == nil && v > 0 && v < 100 {
					state.Tracking = v
				}
			case 'C':
				v, e := strconv.Atoi(arg)
				if e == nil && v >= 0 && v <= 256 {
					state.ColorIndex = v
					state.TrueColor = -1
				}
			case 'c':
				v, e := strconv.ParseInt(arg, 10, 64)
				if e == nil && v >= 0 && v <= 0xffffff {
					state.TrueColor = v
					state.ColorIndex = -1
				}
			case 'Q':
				v, e := strconv.ParseFloat(arg, 64)
				if e == nil {
					state.Oblique = v * .017453292519943295
				}
			case 'A':
				v, e := strconv.Atoi(arg)
				if e == nil && v >= 0 && v <= 2 {
					state.VerticalAlign = v
				}
			case 'F', 'f':
				parts := strings.Split(arg, "|")
				state.Font = parts[0]
				state.FontStyleSet = true
				state.Bold = false
				state.Italic = false
				for _, part := range parts[1:] {
					switch part {
					case "b1":
						state.Bold = true
					case "i1":
						state.Italic = true
					}
				}

			case 'S':
				j := strings.IndexAny(arg, "^/#")
				if j >= 0 {
					r := state
					r.StackMode = arg[j]
					r.Top = PlainDisplay(arg[:j])
					r.Bottom = PlainDisplay(arg[j+1:])
					out = append(out, r)
				} else {
					buf.WriteString(arg)
				}
			}
		default:
			buf.WriteByte('\\')
			buf.WriteByte(cmd)
		}
	}
	flush()
	return out
}
