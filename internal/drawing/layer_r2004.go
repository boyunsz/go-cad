package drawing

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/bitstream"
	"github.com/boyunsz/go-cad/internal/objrec"
)

// decodeLayerRecordR2004 解析 AC1018 图层的内联名称、颜色、打印标志、线宽和线型句柄。
// 使用 cp 解码名称，并验证记录句柄及数据/句柄流边界；不一致时返回错误。
func decodeLayerRecordR2004(rec *objrec.ObjectRecord, handle uint64, cp uint16) (LayerColor, error) {
	var lc LayerColor
	h, err := objrec.ParseObjHeader(rec)
	if err != nil {
		return lc, err
	}
	r := rec.BodyBitStream()
	r.SetBitPos(h.DataStartBit)
	p := plotBits{r: r, cp: cp}
	end := uint64(p.rl()) + rec.BodyBitOffset
	if hd := p.h(); hd != handle {
		return lc, fmt.Errorf("layer handle mismatch %X != %X", hd, handle)
	}
	if e := skipLayerEED(r); e != nil {
		return lc, e
	}
	reactors := p.bl()
	missing := p.b()
	lc.Name = p.tv()
	p.b()
	p.bs()
	p.b() // AC1018 common table flags
	flags := p.bs()
	lc.Off = flags&2 != 0
	lc.Frozen = flags&1 != 0
	lc.NoPlot = flags&16 == 0
	lc.PlotKnown = true
	lc.Lineweight = (flags >> 5) & 31
	lc.Index = p.bs()
	rgb := p.bl()
	cf := p.rc()
	if cf&1 != 0 {
		p.tv()
	}
	if cf&2 != 0 {
		p.tv()
	}
	if p.err != nil {
		return LayerColor{}, p.err
	}
	if end != r.TellBits() {
		return LayerColor{}, fmt.Errorf("AC1018 layer data boundary %d != %d", r.TellBits(), end)
	}
	switch rgb >> 24 {
	case 0xc3:
		lc.Index = uint16(rgb & 255)
	case 0xc2:
		lc.HasTrue = true
		lc.TrueColor = rgb & 0xffffff
	}
	if lc.Index&0x8000 != 0 {
		lc.Off = true
		lc.Index = uint16(-int16(lc.Index))
	}
	if reactors > 100000 {
		return LayerColor{}, fmt.Errorf("too many layer reactors")
	}
	// owner, reactors, optional extension dictionary, external table reference,
	// plot style, then linetype (material was introduced after AC1018).
	count := uint32(1) + reactors + 1 + 1
	if missing == 0 {
		count++
	}
	for i := uint32(0); i < count; i++ {
		if _, e := objrec.ReadHandleReference(r, handle); e != nil {
			return LayerColor{}, e
		}
	}
	lc.Ltype, err = objrec.ReadHandleReference(r, handle)
	if err != nil {
		return LayerColor{}, err
	}
	lc.StyleKnown = true
	return lc, nil
}

type plotBits struct {
	r   *bitstream.BitStream
	err error
	cp  uint16
}

// b 读取单个位 B。
// 首次读取错误保存在 p.err 中；已有错误时不再推进游标并返回零值。
func (p *plotBits) b() (v uint8) {
	if p.err == nil {
		v, p.err = p.r.ReadB()
	}
	return
}

// rc 读取原始字节 RC。
// 首次读取错误保存在 p.err 中；已有错误时不再推进游标并返回零值。
func (p *plotBits) rc() (v uint8) {
	if p.err == nil {
		v, p.err = p.r.ReadRC()
	}
	return
}

// bs 读取压缩短整数 BS。
// 首次读取错误保存在 p.err 中；已有错误时不再推进游标并返回零值。
func (p *plotBits) bs() (v uint16) {
	if p.err == nil {
		v, p.err = p.r.ReadBS()
	}
	return
}

// bl 读取压缩长整数 BL。
// 首次读取错误保存在 p.err 中；已有错误时不再推进游标并返回零值。
func (p *plotBits) bl() (v uint32) {
	if p.err == nil {
		v, p.err = p.r.ReadBL()
	}
	return
}

// rl 读取原始 32 位整数 RL。
// 首次读取错误保存在 p.err 中；已有错误时不再推进游标并返回零值。
func (p *plotBits) rl() (v uint32) {
	if p.err == nil {
		v, p.err = p.r.ReadRL()
	}
	return
}

// h 读取句柄 H 的数值部分，不解析相对引用。
// 首次读取错误保存在 p.err 中；已有错误时不再推进游标并返回零值。
func (p *plotBits) h() (v uint64) {
	if p.err == nil {
		h, e := p.r.ReadH()
		p.err = e
		v = h.Value
	}
	return
}

// tv 读取按配置码页解码的 TV 文本。
// 首次读取错误保存在 p.err 中；已有错误时不再推进游标并返回零值。
func (p *plotBits) tv() (v string) {
	if p.err == nil {
		v, p.err = p.r.ReadTV(p.cp)
	}
	return
}
