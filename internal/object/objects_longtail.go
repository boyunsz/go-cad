// 本文件解析 IDBUFFER、INDEX、LAYER_INDEX 和 PROXY_OBJECT。
//
// 解析内容包括句柄缓冲、索引时间戳、图层索引项及代理对象数据。
// 字段布局依据格式定义实现；合成位流验证不代表真实图纸兼容性已充分覆盖。
//
// 格式参考：LibreDWG dwg.spec 中对应的对象定义。

package object

import (
	"fmt"
	"github.com/boyunsz/go-cad/internal/bitstream"
	"github.com/boyunsz/go-cad/internal/container"
	"github.com/boyunsz/go-cad/internal/entity"
	"github.com/boyunsz/go-cad/internal/objrec"
)

// decodeGenericIDBUFFER 解析 IDBUFFER（dwg.spec DWG_OBJECT(IDBUFFER)，
// AcDbIdBuffer）：dat 流 = RC unknown(0) + BL num_obj_ids(0，上限 10000)；
// handle 流 = owner + reactors + xdic + obj_ids×num_obj_ids（HANDLE_VECTOR
// code 4，经 handleVectorKey 框架统一读取）。
func decodeGenericIDBUFFER(R *bitstream.BitStream, Ver container.DwgVersion, fr *GfRead, g *ObjGeneric) error {
	if err := fr.RC("unknown", g); err != nil {
		return err
	}
	if err := fr.BL("num_obj_ids", g); err != nil {
		return err
	}
	// spec VALUEOUTOFBOUNDS(num_obj_ids, 10000)：越界视为流错位
	if v, _ := g.Field("num_obj_ids").(int64); v > 10000 {
		return fmt.Errorf("cad: IDBUFFER num_obj_ids 越界 %d", v)
	}
	return nil
}

// readTimeBLLFields 读 FIELD_TIMEBLL（R13+ 布局：BL days + BL ms），
// 以 参考 JSON 的 [days, ms] 数组形状记录。
func readTimeBLLFields(R *bitstream.BitStream, key string, g *ObjGeneric) error {
	days, err := R.ReadBL()
	if err != nil {
		return err
	}
	ms, err := R.ReadBL()
	if err != nil {
		return err
	}
	g.Fields = append(g.Fields, ObjField{key, []int64{int64(days), int64(ms)}})
	return nil
}

// decodeGenericINDEX 解析 INDEX（dwg.spec DWG_OBJECT(INDEX)，AcDbIndex）：
// dat 流仅 TIMEBLL last_updated(40)；handle 流为公共三段（框架统一）。
func decodeGenericINDEX(R *bitstream.BitStream, Ver container.DwgVersion, fr *GfRead, g *ObjGeneric) error {
	return readTimeBLLFields(R, "last_updated", g)
}

// decodeGenericLAYER_INDEX 解析 LAYER_INDEX（dwg.spec
// DWG_OBJECT(LAYER_INDEX)，AcDbLayerIndex）：dat 流 = TIMEBLL
// last_updated(40) + BL num_entries(0，上限 20000) + entries×num_entries
// 的 numlayers BL + name T（R2007+ 走字符串流）；每条目的 layer handle
// 引用在 handle 流（owner/reactors/xdic 之后），由
// decodeGenericLAYER_INDEX_HDL 读取。
func decodeGenericLAYER_INDEX(R *bitstream.BitStream, Ver container.DwgVersion, fr *GfRead, g *ObjGeneric) error {
	if err := readTimeBLLFields(R, "last_updated", g); err != nil {
		return err
	}
	num, err := R.ReadBL()
	if err != nil {
		return err
	}
	// spec VALUEOUTOFBOUNDS(num_entries, 20000)
	if num > 20000 {
		return fmt.Errorf("cad: LAYER_INDEX num_entries 越界 %d", num)
	}
	g.Fields = append(g.Fields, ObjField{"num_entries", int64(num)})
	for i := 0; i < int(num); i++ {
		nl, err := R.ReadBL()
		if err != nil {
			return err
		}
		g.Fields = append(g.Fields, ObjField{fmt.Sprintf("entries[%d].numlayers", i), int64(nl)})
		if err := fr.T(fmt.Sprintf("entries[%d].name", i), g); err != nil {
			return err
		}
	}
	return nil
}

// decodeGenericLAYER_INDEX_HDL LAYER_INDEX 的 handle 流附加引用：
// entries×num_entries 的 layer handle（code 5），与 dat 流条目按下标对应。
func decodeGenericLAYER_INDEX_HDL(R *bitstream.BitStream, Ver container.DwgVersion, fr *GfRead, g *ObjGeneric) error {
	num, _ := g.Field("num_entries").(int64)
	for i := 0; i < int(num); i++ {
		h, e := objrec.ReadHandleReference(R, g.Handle)
		if e != nil {
			return e
		}
		g.Handles = append(g.Handles, h)
		g.Fields = append(g.Fields, ObjField{fmt.Sprintf("entries[%d].handle", i), int64(h)})
	}
	return nil
}

// decodeGenericPROXY_OBJECT 解析 PROXY_OBJECT（dwg.spec
// DWG_OBJECT(PROXY_OBJECT)，AcDbProxyObject，固定码 0x1F3）：
// dat 流 = BL proxy_id(90，恒 499) + [PRE R2018：BLx version(95，高 8 位
// maint/低 8 位 dwg)；R2018+：dwg_version/maint_version BLx 对] +
// B from_dxf(70，R2000b+) + 原始数据位捕获（当前位置到 handle 流起点，
// LibreDWG DECODER 的 data_numbits/data 语义）。
// R2010+ 布局的 bitsize 由框架在 decode 之后推导，此路径下 data 段并入
// headRawBits 原样保留（回放无损）但不产出 data_numbits 字段。
// handle 流 = owner + reactors + xdic + 剩余句柄全量（objids）。
func decodeGenericPROXY_OBJECT(R *bitstream.BitStream, Ver container.DwgVersion, fr *GfRead, g *ObjGeneric) error {
	proxyID, err := R.ReadBL()
	if err != nil {
		return err
	}
	g.Fields = append(g.Fields, ObjField{"proxy_id", int64(proxyID)})
	if Ver >= container.VerR2018 {
		dv, err := R.ReadBL()
		if err != nil {
			return err
		}
		mv, err := R.ReadBL()
		if err != nil {
			return err
		}
		g.Fields = append(g.Fields, ObjField{"dwg_version", int64(dv)}, ObjField{"maint_version", int64(mv)})
	} else {
		v, err := R.ReadBL()
		if err != nil {
			return err
		}
		g.Fields = append(g.Fields,
			ObjField{"version", int64(v)},
			ObjField{"maint_version", int64(v >> 8)},
			ObjField{"dwg_version", int64(v & 0xFF)})
	}
	if Ver != container.VerR13 && Ver != container.VerR14 { // SINCE (R_2000b)
		f, err := R.ReadB()
		if err != nil {
			return err
		}
		g.Fields = append(g.Fields, ObjField{"from_dxf", f == 1})
	}
	// 原始数据位捕获：当前位置到 handle 流起点（bitsize，R13~R2007 内联）。
	// data 以 hex 字符串记录（对齐 dwgread JSON 的二进制输出形状）。
	if g.ObjSizeBit > 0 {
		if pos := R.TellBits(); g.ObjSizeBit > pos {
			n := g.ObjSizeBit - pos
			if n > entity.ProxyDataMaxBits {
				return fmt.Errorf("cad: PROXY_OBJECT data 位长异常 %d", n)
			}
			g.Fields = append(g.Fields, ObjField{"data_numbits", int64(n)})
			// LibreDWG bit_read_bits：整字节部分顺序读取，余数位按 LSB 序
			// 填入末字节低位（chain[bytes] |= bit << i），高位补 0——
			// 与 MSB 延续打包不同，这是该位串键的特有形状
			fullBytes := int(n / 8)
			data, err := R.ReadBitsBytes(fullBytes)
			if err != nil {
				return err
			}
			if rest := int(n % 8); rest != 0 {
				last := uint8(0)
				for i := 0; i < rest; i++ {
					b, berr := R.ReadB()
					if berr != nil {
						return berr
					}
					last |= (b & 1) << i
				}
				data = append(data, last)
			}
			g.Fields = append(g.Fields, ObjField{"data", fmt.Sprintf("%X", data)})
		}
	}
	return nil
}

// decodeGenericPROXY_OBJECT_HDL PROXY_OBJECT 的 handle 流附加引用：
// LibreDWG while(hdl_dat->byte < hdl_dat->size-1) 的剩余句柄全量收集
// （objids），尾部 CRC/padding 字节被当作句柄的差异与参考实现一致。
func decodeGenericPROXY_OBJECT_HDL(R *bitstream.BitStream, Ver container.DwgVersion, fr *GfRead, g *ObjGeneric) error {
	for R.TellBits()+8 <= uint64(len(R.Src))*8 {
		h, e := objrec.ReadHandleReference(R, g.Handle)
		if e != nil {
			return nil // 宽容终止：尾部非句柄位串
		}
		g.Handles = append(g.Handles, h)
	}
	return nil
}
