// 本文件将 CAD 文本中的 %% 控制码转换为显示字符。
//
// 转换规则供文本显示和提取路径复用，包括直径、角度与公差符号。
// 下划线和上划线开关在字符转换时移除；其他规则见 expandPercentCodes。
// 实体中的源文本由解析层保留。

package drawing

import "strings"

// expandPercentCodes 翻译 CAD %% 特殊码为显示字符：
//   - %%c / %%C → φ（直径，中文工程标注惯例字形）
//   - %%d / %%D → °（度）
//   - %%p / %%P → ±（公差）
//   - %%u / %%o（下/上划线开关）→ 移除（划线属渲染增强，不在字形层模拟）
//   - %%%      → %（转义）
//   - 其余 %%x → 原样保留
//
// 不含 %% 的文本直接返回原字符串。
func expandPercentCodes(s string) string {
	if !strings.Contains(s, "%%") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '%' || i+1 >= len(s) || s[i+1] != '%' {
			b.WriteByte(s[i])
			i++
			continue
		}
		if i+2 >= len(s) {
			// 尾部悬空 %%：按转义输出单个 %
			b.WriteByte('%')
			i += 2
			continue
		}
		switch s[i+2] {
		case 'c', 'C':
			b.WriteRune('φ')
			i += 3
		case 'd', 'D':
			b.WriteRune('°')
			i += 3
		case 'p', 'P':
			b.WriteRune('±')
			i += 3
		case '%':
			b.WriteByte('%')
			i += 3
		case 'u', 'U', 'o', 'O':
			i += 3
		default:
			b.WriteString("%%")
			i += 2
		}
	}
	return b.String()
}
