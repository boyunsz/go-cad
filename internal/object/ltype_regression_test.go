package object

import (
	"github.com/boyunsz/go-cad/internal/bitstream"
	"github.com/boyunsz/go-cad/internal/container"
	"reflect"
	"testing"
)

// TestR2000DashRecordConsumesAllFields 验证 R2000 线型解码消费全部模式字段并准确到达记录边界。
func TestR2000DashRecordConsumesAllFields(t *testing.T) {
	w := bitstream.NewEncWriter()
	w.WriteTV("DASH_DOT")
	w.WriteBitsMsb(0, 1)
	w.WriteBS(0)
	w.WriteBitsMsb(0, 1)
	w.WriteTV("dash gap dot gap")
	w.WriteBD(6)
	w.WriteRC(65)
	w.WriteRC(4)
	for _, d := range []float64{4, -1, 0, -1} {
		w.WriteBD(d)
		w.WriteBS(0)
		w.WriteRD(0)
		w.WriteRD(0)
		w.WriteBD(1)
		w.WriteBD(0)
		w.WriteBS(0)
	}
	for i := 0; i < 256; i++ {
		w.WriteRC(0)
	}
	w.WriteRC(173)
	r := bitstream.NewBitStream(w.Bytes())
	fr := &GfRead{R: r, Ver: container.VerR2000}
	g := &ObjGeneric{}
	if err := decodeGenericLTYPE(r, container.VerR2000, fr, g); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.Field("dash_lengths"), []float64{4, -1, 0, -1}) || g.Field("complex") != false {
		t.Fatal(g.Fields)
	}
	marker, err := r.ReadRC()
	if err != nil || marker != 173 {
		t.Fatal("record boundary", marker, err)
	}
}
