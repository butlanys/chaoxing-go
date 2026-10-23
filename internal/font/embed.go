package font

import _ "embed"

// embeddedTableJSON 内嵌的字体映射表（原版 resource/font_map_table.json，约 1.5MB）。
//
// 内嵌的意义：发布出去的单个可执行文件即可解密超星加密字体，用户不需要额外附带
// 数据文件。磁盘上的 resource/font_map_table.json 仍然优先（便于用户自行更新映射表），
// 找不到时才回退到这份内嵌数据。
//
//go:embed data/font_map_table.json
var embeddedTableJSON []byte

// embeddedTableSource 内嵌数据在错误信息中的来源标识
const embeddedTableSource = "<embedded:data/font_map_table.json>"
