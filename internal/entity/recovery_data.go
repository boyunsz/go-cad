package entity

// EntityLinks exposes stored R13–R2000 sequence links without changing ownership.
// A no-links record implies adjacent handles in these versions.
func (h *CommonEntityHead) EntityLinks() (uint64, uint64) {
	if h == nil {
		return 0, 0
	}
	if h.noLinks && (h.prevNextLinks || h.R13r14) {
		return h.Handle - 1, h.Handle + 1
	}
	return h.prevEntity, h.nextEntity
}

// FitTangents returns the original fit constraints. Zero vectors mean unspecified.
func (e *EntSpline) FitTangents() (Point3, Point3) { return e.begTanVec, e.endTanVec }

// SetFitTangents 保存拟合样条的起点和终点切线向量，供后续几何恢复使用。
func (e *EntSpline) SetFitTangents(a, b Point3) { e.begTanVec, e.endTanVec = a, b }
