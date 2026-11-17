package drawing

import (
	"encoding/binary"
	"github.com/boyunsz/go-cad/internal/bitstream"
	"testing"
)

// TestHeaderStylePrefixAndTruncation 验证样式头前缀可正确解析，并拒绝截断输入。
func TestHeaderStylePrefixAndTruncation(t *testing.T) {
	w := bitstream.NewEncWriter()
	for i := 0; i < 4; i++ {
		w.WriteBD(1)
	}
	for i := 0; i < 4; i++ {
		w.WriteTV("")
	}
	w.WriteBL(24)
	w.WriteBL(0)
	w.WriteRC(0)
	for i := 0; i < 20; i++ {
		w.WriteBitsMsb(0, 1)
	}
	for i := 0; i < 27; i++ {
		w.WriteBS(0)
	}
	for i := 0; i < 21; i++ {
		x := 0.0
		if i == 0 {
			x = 2.5
		}
		if i == 1 {
			x = 3
		}
		if i == 20 {
			x = .8
		}
		w.WriteBD(x)
	}
	payload := w.Bytes()
	sec := make([]byte, 38+len(payload))
	binary.LittleEndian.PutUint32(sec[16:20], uint32(len(payload)))
	copy(sec[20:], payload)
	binary.LittleEndian.PutUint16(sec[20+len(payload):], bitstream.Crc16DWG(0xC0C1, sec[16:20+len(payload)]))
	v, err := readR2000StylePrefix(sec, 0)
	if err != nil {
		t.Fatal(err)
	}
	if v["LTSCALE"] != 2.5 || v["TEXTSIZE"] != 3.0 || v["CELTSCALE"] != .8 {
		t.Fatal(v)
	}
	if _, err = readR2000StylePrefix(sec[:len(sec)-1], 0); err == nil {
		t.Fatal("truncated header accepted")
	}
}
