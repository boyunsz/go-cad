package render

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

type cachedFontMember struct {
	font       *opentype.Font
	fullName   string
	postScript string
	subfamily  string
	isBold     bool
	isItalic   bool
	isRegular  bool
}

type cachedFontFile struct {
	modTime int64
	size    int64
	sha256  string
	members []cachedFontMember
	invalid bool
}

var (
	fontFileCacheMu sync.RWMutex
	fontFileCache   = map[string]*cachedFontFile{}
)

// loadCachedFontFile 读取并缓存单个字体文件的解析结果与 SHA-256 哈希。
// 以 (绝对路径, 修改时间, 文件大小) 校验有效性；文件变更时自动失效重建。
// 并发只读安全，*opentype.Font 本身支持多协程配合各自 sfnt.Buffer 并发访问。
func loadCachedFontFile(file string) *cachedFontFile {
	if file == "" {
		return nil
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		abs = file
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		return nil
	}
	modTime, size := st.ModTime().UnixNano(), st.Size()

	fontFileCacheMu.RLock()
	if entry, ok := fontFileCache[abs]; ok && entry.modTime == modTime && entry.size == size {
		fontFileCacheMu.RUnlock()
		return entry
	}
	fontFileCacheMu.RUnlock()

	data, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	entry := &cachedFontFile{
		modTime: modTime,
		size:    size,
		sha256:  fmt.Sprintf("%x", sha256.Sum256(data)),
	}
	var buf sfnt.Buffer
	extractMember := func(f *opentype.Font) cachedFontMember {
		full, _ := f.Name(&buf, sfnt.NameIDFull)
		ps, _ := f.Name(&buf, sfnt.NameIDPostScript)
		sub, _ := f.Name(&buf, sfnt.NameIDSubfamily)
		low := strings.ToLower(sub)
		return cachedFontMember{
			font:       f,
			fullName:   full,
			postScript: ps,
			subfamily:  sub,
			isBold:     strings.Contains(low, "bold") || strings.Contains(low, "black"),
			isItalic:   strings.Contains(low, "italic") || strings.Contains(low, "oblique"),
			isRegular:  low == "regular" || low == "book" || low == "normal",
		}
	}
	if f, err := opentype.Parse(data); err == nil {
		entry.members = []cachedFontMember{extractMember(f)}
	} else if c, err := opentype.ParseCollection(data); err == nil {
		entry.members = make([]cachedFontMember, 0, c.NumFonts())
		for i := 0; i < c.NumFonts(); i++ {
			if f, e := c.Font(i); e == nil {
				entry.members = append(entry.members, extractMember(f))
			}
		}
		if len(entry.members) == 0 {
			entry.invalid = true
		}
	} else {
		entry.invalid = true
	}

	fontFileCacheMu.Lock()
	fontFileCache[abs] = entry
	fontFileCacheMu.Unlock()
	return entry
}

// selectMember 按显式成员名或粗斜体要求从缓存字体文件中挑选最匹配的字体成员。
func (c *cachedFontFile) selectMember(selector string, bold, italic bool) *opentype.Font {
	if c == nil || c.invalid || len(c.members) == 0 {
		return nil
	}
	if selector != "" {
		for _, m := range c.members {
			if strings.EqualFold(m.fullName, selector) || strings.EqualFold(m.postScript, selector) {
				return m.font
			}
		}
		return nil
	}
	if len(c.members) == 1 {
		return c.members[0].font
	}
	var first, regular *opentype.Font
	for _, m := range c.members {
		if first == nil && (bold || !m.isBold) && (italic || !m.isItalic) {
			first = m.font
		}
		if regular == nil && m.isBold == bold && m.isItalic == italic && (bold || italic || m.isRegular) {
			regular = m.font
		}
	}
	if regular != nil {
		return regular
	}
	return first
}

// cachedFontSHA256 返回字体文件的 SHA-256 十六进制摘要（复用进程级字体缓存）。
func cachedFontSHA256(file string) string {
	if entry := loadCachedFontFile(file); entry != nil {
		return entry.sha256
	}
	return ""
}
