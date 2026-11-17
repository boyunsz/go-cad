package drawing

import (
	"encoding/binary"
	"fmt"
	"github.com/boyunsz/go-cad/internal/bitstream"
	"github.com/boyunsz/go-cad/internal/container"
	"math"
)

// readR2000StyleHeader 读取 R2000 头段中的样式变量并更新 HeaderVars 和 Ltscale。
// 其他版本直接返回；解析失败时把原因保存在 STYLE_HEADER_ERROR 中。
func (d *Document) readR2000StyleHeader(data []byte) {
	if d.Ver != container.VerR2000 {
		return
	}
	sec, err := container.ReadR2000Section(data, container.R2000SecHeaderVars)
	if err == nil {
		var v map[string]any
		v, err = readR2000StylePrefix(sec, d.Codepage)
		if err == nil {
			d.HeaderVars = v
			d.Ltscale = v["LTSCALE"].(float64)
			return
		}
	}
	if d.HeaderVars == nil {
		d.HeaderVars = map[string]any{}
	}
	d.HeaderVars["STYLE_HEADER_ERROR"] = fmt.Sprint(err)
}

// readR2000StylePrefix 以 R2000 字段顺序解析头段前缀至 CELTSCALE，使用 cp 解码文本。
func readR2000StylePrefix(sec []byte, cp uint16) (map[string]any, error) {
	return readStylePrefix(sec, cp, false)
}

// readStylePrefix 校验头段长度和 CRC，并按版本分支读取至 CELTSCALE 的系统变量。
// cp 指定文本码页；单位枚举越界、非有限数值或非正 LTSCALE 均返回错误。
func readStylePrefix(sec []byte, cp uint16, r2004 bool) (map[string]any, error) {
	if len(sec) < 38 {
		return nil, fmt.Errorf("short R2000 header")
	}
	n := int(binary.LittleEndian.Uint32(sec[16:20]))
	if n < 1 || n > len(sec)-38 {
		return nil, fmt.Errorf("invalid R2000 header size")
	}
	if binary.LittleEndian.Uint16(sec[20+n:22+n]) != bitstream.Crc16DWG(0xC0C1, sec[16:20+n]) {
		return nil, fmt.Errorf("R2000 header CRC mismatch")
	}
	r := bitstream.NewBitStream(sec[20 : 20+n])
	v := map[string]any{}
	for i := 0; i < 4; i++ {
		if _, err := r.ReadBD(); err != nil {
			return nil, err
		}
	}
	for i := 0; i < 4; i++ {
		if _, err := r.ReadTV(cp); err != nil {
			return nil, err
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := r.ReadBL(); err != nil {
			return nil, err
		}
	}
	if !r2004 {
		if _, err := r.ReadH(); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"DIMASO", "DIMSHO", "PLINEGEN", "ORTHOMODE", "REGENMODE", "FILLMODE", "QTEXTMODE", "PSLTSCALE", "LIMCHECK", "USRTIMER", "SKPOLY", "ANGDIR", "SPLFRAME", "MIRRTEXT", "WORLDVIEW", "TILEMODE", "PLIMCHECK", "VISRETAIN", "DISPSILH", "PELLIPSE"} {
		a, err := r.ReadB()
		if err != nil {
			return nil, err
		}
		v[name] = a
		if r2004 && name == "LIMCHECK" {
			if _, err := r.ReadB(); err != nil {
				return nil, err
			}
		}
	}
	for _, name := range []string{"PROXYGRAPHICS", "TREEDEPTH", "LUNITS", "LUPREC", "AUNITS", "AUPREC", "ATTMODE", "PDMODE", "USERI1", "USERI2", "USERI3", "USERI4", "USERI5", "SPLINESEGS", "SURFU", "SURFV", "SURFTYPE", "SURFTAB1", "SURFTAB2", "SPLINETYPE", "SHADEDGE", "SHADEDIF", "UNITMODE", "MAXACTVP", "ISOLINES", "CMLJUST", "TEXTQLTY"} {
		a, err := r.ReadBS()
		if err != nil {
			return nil, err
		}
		v[name] = a
		if r2004 && name == "PDMODE" {
			for i := 0; i < 3; i++ {
				if _, err := r.ReadBL(); err != nil {
					return nil, err
				}
			}
		}
	}
	if v["LUNITS"].(uint16) > 5 || v["LUPREC"].(uint16) > 8 || v["AUNITS"].(uint16) > 4 || v["AUPREC"].(uint16) > 8 {
		return nil, fmt.Errorf("R2000 header units out of range")
	}
	for _, name := range []string{"LTSCALE", "TEXTSIZE", "TRACEWID", "SKETCHINC", "FILLETRAD", "THICKNESS", "ANGBASE", "PDSIZE", "PLINEWID", "USERR1", "USERR2", "USERR3", "USERR4", "USERR5", "CHAMFERA", "CHAMFERB", "CHAMFERC", "CHAMFERD", "FACETRES", "CMLSCALE", "CELTSCALE"} {
		a, err := r.ReadBD()
		if err != nil {
			return nil, err
		}
		if math.IsNaN(a) || math.IsInf(a, 0) {
			return nil, fmt.Errorf("nonfinite %s", name)
		}
		v[name] = a
	}
	if v["LTSCALE"].(float64) <= 0 {
		return nil, fmt.Errorf("nonpositive LTSCALE")
	}
	return v, nil
}

// readR2004StyleHeader 读取 R2004 的 AcDb:Header 样式变量并更新文档。
// 其他版本直接返回；段加载或解析失败时记录 STYLE_HEADER_ERROR。
func (d *Document) readR2004StyleHeader(data []byte) {
	if d.Ver != container.VerR2004 {
		return
	}
	sec, err := container.LoadNamedSectionData(data, "AcDb:Header")
	if err == nil {
		var v map[string]any
		v, err = readStylePrefix(sec, d.Codepage, true)
		if err == nil {
			d.HeaderVars = v
			d.Ltscale = v["LTSCALE"].(float64)
			return
		}
	}
	if d.HeaderVars == nil {
		d.HeaderVars = map[string]any{}
	}
	d.HeaderVars["STYLE_HEADER_ERROR"] = fmt.Sprint(err)
}
