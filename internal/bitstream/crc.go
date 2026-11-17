// 本文件实现 DWG 使用的 CRC-16 计算及校验值写入。
//
// 算法使用反射多项式 0xA001，初始值由调用方指定。写入时先对齐字节边界，
// 再计算指定范围的校验值，并以小端双字节写出。
//
// 格式参考：LibreDWG 的 bit_calc_CRC 和 bit_write_CRC。

package bitstream

// crc16Table DWG CRC16 查询表：table[i] 为字节 i 独立贡献的 16 位余式，
// 由反射多项式 0xA001 展开 8 次生成（与 LibreDWG crctable 一致）。
var crc16Table = func() (t [256]uint16) {
	for i := 0; i < 256; i++ {
		c := uint16(i)
		for j := 0; j < 8; j++ {
			if c&1 != 0 {
				c = c>>1 ^ 0xA001
			} else {
				c >>= 1
			}
		}
		t[i] = c
	}
	return t
}()

// Crc16DWG 以 seed 为初值累积计算 data 的 DWG CRC16。
// seed 允许链式分段计算：Crc16DWG(Crc16DWG(s, a), b) == Crc16DWG(s, a||b)。
func Crc16DWG(seed uint16, Data []byte) uint16 {
	crc := seed
	for _, b := range Data {
		crc = crc>>8 ^ crc16Table[byte(crc)^b]
	}
	return crc
}

// WriteCRC 对齐到字节边界（未对齐时当前部分字节补零）后，对从 startBit
// （须为字节边界）到当前写入位覆盖的字节区间计算 CRC（seed 0xC0C1）并写入，
// 返回 CRC 值。区间不含 CRC 自身。对应 LibreDWG bit_write_CRC 的默认调用形式。
func (w *EncWriter) WriteCRC(startBit uint64) uint16 {
	return w.WriteCRCSeed(startBit, 0xC0C1)
}

// WriteCRCSeed 同 WriteCRC，seed 由调用方指定（对象图等场景用 0）。
func (w *EncWriter) WriteCRCSeed(startBit uint64, seed uint16) uint16 {
	w.AlignByte()
	start := (startBit + 7) / 8
	if start > uint64(len(w.Data)) {
		start = uint64(len(w.Data))
	}
	crc := Crc16DWG(seed, w.Data[start:])
	w.WriteRS(crc)
	return crc
}

// AlignByte 对齐到字节边界：位偏移非零时以 0 填满当前字节剩余位。
func (w *EncWriter) AlignByte() {
	if w.Bit != 0 {
		w.WriteBitsMsb(0, 8-w.Bit)
	}
}
