// 本文件定义 LARGE_RADIAL_DIMENSION 的专用字段布局。
//
// 布局作为标注解析器的配置使用；其他标注实体的解析见
// entities_dimension.go。

package entity

// DimLayoutLargeRadial LARGE_RADIAL_DIMENSION 的类型专属尾部布局。
// 该类（R2000+ 动态类）共用 COMMON_ENTITY_DIMENSION 公共段与
// EntDimension 载体，仅专属尾部顺序不同（def_pt/chord_pt/jog_angle/
// ovr_center/jog_pt），读取逻辑见 readDimSpecific 的同名 case。
// LibreDWG DECODER 的 flag 合成不给该类补类型位（switch 无此 case），
// 与 computeDimFlag 对未知布局的行为一致。
const DimLayoutLargeRadial DimSpecificLayout = DimLayoutArc + 1
