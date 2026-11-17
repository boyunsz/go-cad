package drawing

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/boyunsz/go-cad/internal/testfixtures"
	"os"
	"testing"

	"github.com/boyunsz/go-cad/internal/entity"
)

// TestLocalSourceOwnershipAndCloneOffsets 用私有基准验证源实体归属及标注副本偏移，防止块子实体泄漏到模型根节点。
func TestLocalSourceOwnershipAndCloneOffsets(t *testing.T) {
	var fixture struct {
		SourceHash string                           `json:"source_sha256"`
		Owned      []struct{ Handle, Owner uint64 } `json:"owned_records"`
		Clones     []struct {
			Handle uint64
			Point  []float64
		} `json:"dimension_clone_offsets"`
	}
	b, err := os.ReadFile(testfixtures.Path(t, "placement"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(testfixtures.Path(t, "drawing-a"))
	if os.IsNotExist(err) {
		t.Skip("local sample unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != fixture.SourceHash {
		t.Skip("local sample differs from independently checked fixture")
	}
	doc, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	roots := map[uint64]bool{}
	for _, e := range doc.SelectedModelSpace() {
		roots[entity.EntityBase(e).Handle] = true
	}
	if len(fixture.Owned) != 103 || len(fixture.Clones) != 47 {
		t.Fatal("incomplete placement fixture")
	}
	for _, r := range fixture.Owned {
		e := doc.ByHandle[r.Handle]
		if e == nil {
			t.Fatalf("missing record %X", r.Handle)
		}
		b := entity.EntityBase(e)
		if b.Owner != r.Owner || b.Mode != 0 || roots[r.Handle] {
			t.Fatalf("block child %X leaked into model roots: owner %X want %X, mode %d", r.Handle, b.Owner, r.Owner, b.Mode)
		}
	}
	for _, r := range fixture.Clones {
		d, ok := doc.ByHandle[r.Handle].(*entity.EntDimension)
		if !ok || !d.HasInsertPoint || len(r.Point) != 2 || d.InsertPoint.X != r.Point[0] || d.InsertPoint.Y != r.Point[1] {
			t.Fatalf("dimension clone offset %X was lost", r.Handle)
		}
	}
}
