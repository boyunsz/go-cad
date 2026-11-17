// 本文件根据解码时保留的原始位流回放实体记录。
//
// EncodeEntityR200x 按记录前导、数据区和句柄流的顺序写出原始位串，
// 保留源记录内部的位偏移关系。此路径依赖解析阶段采集的数据；实体
// 字段的结构化编码在 encode_forward.go 中实现。

package writer

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/bitstream"
	"github.com/boyunsz/go-cad/internal/container"
	"github.com/boyunsz/go-cad/internal/entity"
)

// EncodeEntityR200x 将实体重编码为完整记录 body 位流（R13-R2018 家族
// 通用）：preBits + headRawBits + RawHandleBits 三段原样回放，返回 body
// 字节与 handle 流起点位（datEnd，body 局部坐标，与源记录一致）。位串
// 未收集（生产路径结构异常或解码失败回退产物）时报错，由调用方按
// 能力边界处理。
func EncodeEntityR200x(ent any, ver container.DwgVersion) ([]byte, uint64, error) {
	_ = ver // 回放方案与版本无关：位串原样保留全部版本差异
	b := entity.EntityBase(ent)
	if b == nil || b.HeadRawBits == "" || b.RawHandleBits == "" {
		return nil, 0, fmt.Errorf("cad: 实体 round-trip 缺少位串收集")
	}
	w := bitstream.NewEncWriter()
	w.WriteBitsString(b.PreBits)
	w.WriteBitsString(b.HeadRawBits)
	datEnd := w.TellBits()
	w.WriteBitsString(b.RawHandleBits)
	return w.Bytes(), datEnd, nil
}
