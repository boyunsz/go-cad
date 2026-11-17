package drawing

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/entity"
	"sort"
)

// InsertAttributes merges the stored first/last handle list with explicit owner
// records. R2000 INSERT stores endpoints, not a complete attribute array.
// Dedupe by handle within this INSERT; different block instances stay separate.
func (d *Document) InsertAttributes(ins *entity.EntInsert) []*entity.EntAttrib {
	seen := map[uint64]bool{}
	var out []*entity.EntAttrib
	add := func(a *entity.EntAttrib) {
		if a != nil && a.TypeName != "ATTDEF" && !seen[a.Handle] {
			seen[a.Handle] = true
			out = append(out, a)
		}
	}
	for _, h := range ins.Attribs {
		add(d.Attribs[h])
	}
	var more []*entity.EntAttrib
	for _, e := range d.Blocks[ins.Handle] {
		if a, ok := e.(*entity.EntAttrib); ok && a.Owner == ins.Handle {
			more = append(more, a)
		}
	}
	sort.Slice(more, func(i, j int) bool { return more[i].Handle < more[j].Handle })
	for _, a := range more {
		add(a)
	}
	return out
}

// attributeFlags 读取 ATTRIB 扩展字段中的标志；字段缺失或类型不符时返回 0。
func attributeFlags(a *entity.EntAttrib) int64 { v, _ := a.Extra["flags"].(int64); return v }

// AttributeRecord is source data, independent of the selected layout or fonts.
type AttributeRecord struct {
	Handle      uint64        `json:"handle"`
	HandleHex   string        `json:"handle_hex"`
	Owner       uint64        `json:"owner"`
	Type        string        `json:"type"`
	Tag         string        `json:"tag"`
	Value       string        `json:"value"`
	Position    entity.Point3 `json:"position"`
	WidthFactor float64       `json:"width_factor"`
	Flags       int64         `json:"flags"`
}

// AttributeRecords 汇总按句柄索引保存的属性记录并排序，保留源文本、标签和归属信息。
// 不根据属性定义合成实例值，也不改写原文档。
func (d *Document) AttributeRecords() []AttributeRecord {
	var out []AttributeRecord
	for h, e := range d.ByHandle {
		if a, ok := e.(*entity.EntAttrib); ok {
			out = append(out, AttributeRecord{h, fmt.Sprintf("%X", h), a.Owner, a.TypeName, a.Tag, a.Text, a.Insertion, a.WidthFactor, attributeFlags(a)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Handle < out[j].Handle })
	return out
}
