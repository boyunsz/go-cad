package drawing

import (
	"sort"
	"strconv"
	"strings"

	"github.com/boyunsz/go-cad/internal/entity"
)

// sortOrderKeys 单遍扫描文档中的 SORTENTSTABLE 对象，建立实体句柄到绘制顺序键的映射。
// 多个 SORTENTSTABLE 按句柄升序叠加，避免对每个索引重复线性扫描 Fields（O(N)）。
func (d *Document) sortOrderKeys() map[uint64]uint64 {
	var tables []uint64
	for h, g := range d.InternalObjs {
		if g.Name == "SORTENTSTABLE" {
			tables = append(tables, h)
		}
	}
	if len(tables) == 0 {
		return nil
	}
	sort.Slice(tables, func(i, j int) bool { return tables[i] < tables[j] })
	keys := map[uint64]uint64{}
	for _, h := range tables {
		g := d.InternalObjs[h]
		var numEnts int64 = -1
		var ents, sortEnts map[int64]int64
		for _, f := range g.Fields {
			if f.Key == "num_ents" {
				if v, ok := f.Val.(int64); ok {
					numEnts = v
				}
				continue
			}
			if strings.HasPrefix(f.Key, "ents[") && strings.HasSuffix(f.Key, "]") {
				idx, err := strconv.ParseInt(f.Key[5:len(f.Key)-1], 10, 64)
				if err == nil {
					if v, ok := f.Val.(int64); ok {
						if ents == nil {
							ents = map[int64]int64{}
						}
						ents[idx] = v
					}
				}
				continue
			}
			if strings.HasPrefix(f.Key, "sort_ents[") && strings.HasSuffix(f.Key, "]") {
				idx, err := strconv.ParseInt(f.Key[10:len(f.Key)-1], 10, 64)
				if err == nil {
					if v, ok := f.Val.(int64); ok {
						if sortEnts == nil {
							sortEnts = map[int64]int64{}
						}
						sortEnts[idx] = v
					}
				}
			}
		}
		if numEnts <= 0 || len(ents) == 0 || len(sortEnts) == 0 {
			continue
		}
		for i := int64(0); i < numEnts; i++ {
			a, ok := ents[i]
			b, ok2 := sortEnts[i]
			if ok && ok2 {
				keys[uint64(a)] = uint64(b)
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

// OrderedEntities respects sort-handle mappings without grouping by display style.
// Equal sort handles retain source order; zero is the documented last-drawn key.
func (d *Document) OrderedEntities(in []any) []any {
	return orderedWithKeys(in, d.sortOrderKeys())
}

// orderedWithKeys 使用预构建的 SORTENTSTABLE 键值表对实体列表进行稳定排序。
func orderedWithKeys(in []any, keys map[uint64]uint64) []any {
	if len(keys) == 0 || len(in) < 2 {
		return in
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
	if !ts.Faithful {
		return in
	}
	if !ts.sortKeysLoaded {
		ts.sortKeys = ts.Doc.sortOrderKeys()
		ts.sortKeysLoaded = true
	}
	return orderedWithKeys(in, ts.sortKeys)
}
