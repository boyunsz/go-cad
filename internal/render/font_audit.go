package render

import (
	"crypto/sha256"
	"fmt"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"os"
	"strings"
)

// FontFace records the actual face, including explicit collection selection.
// Hashes are calculated once per file per rendering operation.
type FontFace struct {
	Family, Subfamily, FullName, PostScript, Weight, SHA256 string
	Italic                                                  bool
}

// face 读取实际字体成员的名称、样式和文件哈希，用于字体诊断。
// 结果按字体对象缓存，文件哈希按路径缓存；缺失字体返回空记录。
func (r *fontResolver) face(f *opentype.Font, path string) FontFace {
	if f == nil {
		return FontFace{}
	}
	if info, ok := r.faces[f]; ok {
		return info
	}
	name := func(id sfnt.NameID) string { v, _ := f.Name(nil, id); return v }
	info := FontFace{Family: name(sfnt.NameIDFamily), Subfamily: name(sfnt.NameIDSubfamily), FullName: name(sfnt.NameIDFull), PostScript: name(sfnt.NameIDPostScript)}
	// Weight is the font's declared style name, not a fabricated numeric weight.
	info.Weight = info.Subfamily
	s := strings.ToLower(info.Subfamily)
	info.Italic = strings.Contains(s, "italic") || strings.Contains(s, "oblique")
	file, _, _ := strings.Cut(path, "#")
	digest, ok := r.hashes[file]
	if !ok {
		if b, e := os.ReadFile(file); e == nil {
			digest = fmt.Sprintf("%x", sha256.Sum256(b))
		}
		r.hashes[file] = digest
	}
	info.SHA256 = digest
	r.faces[f] = info
	return info
}

// fallback 加载并缓存指定路径的回退字体，包括缓存加载失败的 nil 结果。
func (r *fontResolver) fallback(path string) *opentype.Font {
	key := "fallback-file:" + path
	if f, ok := r.cache[key]; ok {
		return f
	}
	f := parseFontFile(path)
	r.cache[key] = f
	return f
}
