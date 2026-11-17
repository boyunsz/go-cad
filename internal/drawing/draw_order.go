package drawing

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/entity"
	"sort"
)

// OrderedEntities respects sort-handle mappings without grouping by display style.
// Equal sort handles retain source order; zero is the documented last-drawn key.
func (d *Document) OrderedEntities(in []any) []any {
	keys := map[uint64]uint64{}
	var tables []uint64
	for h, g := range d.InternalObjs {
		if g.Name == "SORTENTSTABLE" {
			tables = append(tables, h)
		}
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i] < tables[j] })
	for _, h := range tables {
		g := d.InternalObjs[h]
		n, _ := g.Field("num_ents").(int64)
		for i := int64(0); i < n; i++ {
			a, ok := g.Field(fmt.Sprintf("ents[%d]", i)).(int64)
			b, ok2 := g.Field(fmt.Sprintf("sort_ents[%d]", i)).(int64)
			if ok && ok2 {
				keys[uint64(a)] = uint64(b)
			}
		}
	}
	active := false
	for _, e := range in {
		if c, ok := e.(entity.EntityCommon); ok {
			if _, ok := keys[c.Common().Handle]; ok {
				active = true
				break
			}
		}
	}
	if !active {
		return in
	}
	out := append([]any(nil), in...)
	key := func(e any) uint64 {
		c, ok := e.(entity.EntityCommon)
		if !ok {
			return 0
		}
		h := c.Common().Handle
		if k, ok := keys[h]; ok {
			if k == 0 {
				return ^uint64(0)
			}
			return k
		}
		return h
	}
	sort.SliceStable(out, func(i, j int) bool { return key(out[i]) < key(out[j]) })
	return out
}

// ordered 在 faithful 模式下应用源文档的绘制顺序；预览模式保留输入顺序。
func (ts *Tessellator) ordered(in []any) []any {
	if ts.Faithful {
		return ts.Doc.OrderedEntities(in)
	}
	return in
}
