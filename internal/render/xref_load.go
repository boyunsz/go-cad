package render

import (
	"crypto/sha256"
	"fmt"
	"github.com/boyunsz/go-cad/internal/drawing"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type XrefUse struct {
	Block                       uint64
	Name, File, SHA256, Version string
	SourceEntities, Skipped     int
}

// bindExternalDrawings 按显式映射或自定义加载器递归读取外部 DWG，并把引用绑定到主文档的浅拷贝。
// 映射键按文件基名忽略大小写匹配；同路径/键只解析一次以处理重复和循环引用。
// 歧义映射或文件读取/解析失败返回错误，不复制或移动外部文件。
func bindExternalDrawings(doc *drawing.Document, files map[string]string, loader func(name, declaredPath string) ([]byte, error)) (*drawing.Document, error) {
	if (len(files) == 0 && loader == nil) || doc.Xrefs != nil {
		return doc, nil
	}
	key := func(s string) string {
		s = strings.ReplaceAll(s, "\\", "/")
		s = filepath.Base(s)
		return strings.TrimSuffix(strings.ToLower(s), ".dwg")
	}
	paths := map[string]string{}
	for n, p := range files {
		a, e := filepath.Abs(p)
		if e != nil {
			return nil, e
		}
		k := key(n)
		if old, ok := paths[k]; ok && old != a {
			return nil, fmt.Errorf("ambiguous external DWG mapping %q", n)
		}
		paths[k] = a
	}
	cache := map[string]*drawing.XrefDocument{}
	var bind func(*drawing.Document) error
	bind = func(d *drawing.Document) error {
		d.Xrefs = map[uint64]*drawing.XrefDocument{}
		hs := make([]uint64, 0)
		for h, g := range d.InternalObjs {
			if g.Name == "BLOCK_HEADER" {
				hs = append(hs, h)
			}
		}
		sort.Slice(hs, func(i, j int) bool { return hs[i] < hs[j] })
		for _, h := range hs {
			g := d.InternalObjs[h]
			declared, _ := g.Field("xref_pname").(string)
			if declared == "" {
				continue
			}
			name, _ := g.Field("name").(string)
			path := paths[key(declared)]
			if path == "" {
				path = paths[key(name)]
			}
			cacheKey := path
			if cacheKey == "" && loader != nil {
				cacheKey = "loader:" + key(declared)
				if cacheKey == "loader:" {
					cacheKey = "loader:" + key(name)
				}
			}
			if cacheKey == "" {
				continue
			}
			resource := cache[cacheKey]
			if resource == nil {
				var b []byte
				var e error
				sourceLabel := path
				if path != "" {
					b, e = os.ReadFile(path)
					if e != nil {
						return fmt.Errorf("external DWG %s: %w", path, e)
					}
				} else if loader != nil {
					b, e = loader(name, declared)
					if e != nil {
						return fmt.Errorf("external DWG %s: %w", declared, e)
					}
					if len(b) == 0 {
						continue
					}
					sourceLabel = declared
				}
				parsed, e := drawing.Parse(b)
				if e != nil {
					return fmt.Errorf("external DWG %s: %w", sourceLabel, e)
				}
				parsed.SourceFile = sourceLabel
				resource = &drawing.XrefDocument{Document: parsed, Path: sourceLabel, SHA256: fmt.Sprintf("%x", sha256.Sum256(b))}
				cache[cacheKey] = resource
				if e = bind(parsed); e != nil {
					return e
				}
			}
			d.Xrefs[h] = resource
		}
		return nil
	}
	copy := *doc
	if e := bind(&copy); e != nil {
		return nil, e
	}
	return &copy, nil
}

// externalInventory 遍历已绑定外参，汇总源文件哈希、版本及实体统计，并防止循环遍历。
func externalInventory(d *drawing.Document) []XrefUse {
	var out []XrefUse
	seen := map[*drawing.Document]bool{}
	var visit func(*drawing.Document)
	visit = func(doc *drawing.Document) {
		if seen[doc] {
			return
		}
		seen[doc] = true
		hs := make([]uint64, 0, len(doc.Xrefs))
		for h := range doc.Xrefs {
			hs = append(hs, h)
		}
		sort.Slice(hs, func(i, j int) bool { return hs[i] < hs[j] })
		for _, h := range hs {
			r := doc.Xrefs[h]
			name, _ := doc.InternalObjs[h].Field("name").(string)
			out = append(out, XrefUse{Block: h, Name: name, File: r.Path, SHA256: r.SHA256, Version: r.Document.Version(), SourceEntities: r.Document.EntityCount(), Skipped: r.Document.Skipped()})
			visit(r.Document)
		}
	}
	visit(d)
	return out
}

// primitiveDocument 返回图元所属的源文档；图元未指定独立源文档时使用主文档。
func primitiveDocument(root *drawing.Document, p drawing.Primitive) *drawing.Document {
	if p.SourceDocument != nil {
		return p.SourceDocument
	}
	return root
}
