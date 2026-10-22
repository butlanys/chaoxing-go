// 本文件由脚本从 Python 原版 api/cxsecret_font.py 的 KX_RADICALS_TAB 逐字生成，请勿手工修改。

package font

import "strings"

// kxRadicalsFrom 康熙部首（原版 KX_RADICALS_TAB 的源字符串，223 个字符）
const kxRadicalsFrom = "⼀⼁⼂⼃⼄⼅⼆⼇⼈⼉⼊⼋⼌⼍⼎⼏⼐⼑⼒⼓⼔⼕⼖⼗⼘⼙⼚⼛⼜⼝⼞⼟⼠⼡⼢⼣⼤⼥⼦⼧⼨⼩⼪⼫⼬⼭⼮⼯⼰⼱⼲⼳⼴⼵⼶⼷⼸⼹⼺⼻⼼⼽⼾⼿⽀⽁⽂⽃⽄⽅⽆⽇⽈⽉⽊⽋⽌⽍⽎⽏⽐⽑⽒⽓⽔⽕⽖⽗⽘⽙⽚⽛⽜⽝⽞⽟⽠⽡⽢⽣⽤⽥⽦⽧⽨⽩⽪⽫⽬⽭⽮⽯⽰⽱⽲⽳⽴⽵⽶⽷⽸⽹⽺⽻⽼⽽⽾⽿⾀⾁⾂⾃⾄⾅⾆⾇⾈⾉⾊⾋⾌⾍⾎⾏⾐⾑⾒⾓⾔⾕⾖⾗⾘⾙⾚⾛⾜⾝⾞⾟⾠⾡⾢⾣⾤⾥⾦⾧⾨⾩⾪⾫⾬⾭⾮⾯⾰⾱⾲⾳⾴⾵⾶⾷⾸⾹⾺⾻⾼髙⾽⾾⾿⿀⿁⿂⿃⿄⿅⿆⿇⿈⿉⿊⿋⿌⿍⿎⿏⿐⿑⿒⿓⿔⿕⺠⻬⻩⻢⻜⻅⺟⻓"

// kxRadicalsTo 与 kxRadicalsFrom 一一对应的常用汉字
const kxRadicalsTo = "一丨丶丿乙亅二亠人儿入八冂冖冫几凵刀力勹匕匚匸十卜卩厂厶又口囗土士夂夊夕大女子宀寸小尢尸屮山巛工己巾干幺广廴廾弋弓彐彡彳心戈戶手支攴文斗斤方无日曰月木欠止歹殳毋比毛氏气水火爪父爻爿片牙牛犬玄玉瓜瓦甘生用田疋疒癶白皮皿目矛矢石示禸禾穴立竹米糸缶网羊羽老而耒耳聿肉臣自至臼舌舛舟艮色艸虍虫血行衣襾見角言谷豆豕豸貝赤走足身車辛辰辵邑酉采里金長門阜隶隹雨青非面革韋韭音頁風飛食首香馬骨高高髟鬥鬯鬲鬼魚鳥鹵鹿麥麻黃黍黑黹黽鼎鼓鼠鼻齊齒龍龜龠民齐黄马飞见母长"

// kxRadicalMap 康熙部首 -> 对应汉字 的映射表
var kxRadicalMap = func() map[rune]rune {
	from := []rune(kxRadicalsFrom)
	to := []rune(kxRadicalsTo)
	m := make(map[rune]rune, len(from))
	for i := range from {
		m[from[i]] = to[i]
	}
	return m
}()

// KXRadicals 康熙部首替换器（与 Python KX_RADICALS_TAB 完全一致）
var KXRadicals = func() *strings.Replacer {
	from := []rune(kxRadicalsFrom)
	to := []rune(kxRadicalsTo)
	pairs := make([]string, 0, len(from)*2)
	for i := range from {
		pairs = append(pairs, string(from[i]), string(to[i]))
	}
	return strings.NewReplacer(pairs...)
}()

// KXFix 把康熙部首替换为对应汉字，等价于 Python 的 str.translate(KX_RADICALS_TAB)
func KXFix(s string) string {
	if len(s) == 0 {
		return s
	}
	hasRadical := false
	for _, r := range s {
		if _, ok := kxRadicalMap[r]; ok {
			hasRadical = true
			break
		}
	}
	if !hasRadical {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if c, ok := kxRadicalMap[r]; ok {
			b.WriteRune(c)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
