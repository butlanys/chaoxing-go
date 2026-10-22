package htmlx

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/butlanys/chaoxing-go/internal/logx"
)

// 本文件与 card.go / questions.go / records.go / jsvalue.go 一起实现 types.go 中声明的解析函数。
// 所有 selector / 正则 / 字段映射与 Python 原版 /root/chaoxing-ref/api/decode.py 逐条对齐。

// 预编译正则（对应 decode.py 里的 re.findall / re.search 模式）
var (
	// decode_course_list: re.findall(r"cpi=(.*?)&", href)
	reCourseCPI = regexp.MustCompile(`cpi=(.*?)&`)
	// _extract_points_from_chapter: re.findall(r"^cur(\d{1,20})$", id)
	rePointID = regexp.MustCompile(`^cur(\d{1,20})$`)
	// decode_course_card: re.findall(r"mArg=\{(.*?)\};", html.replace(" ", ""))
	reMArg = regexp.MustCompile(`mArg=\{(.*?)\};`)
)

// Python 正则里 \s 在 str 模式下是 Unicode 感知的，Go 的 \s 只有 ASCII。
// 这里显式列出 Python re 的 \s 集合（用 python3 枚举 chr(0)..chr(0x10FFFF) 得到），保证行为一致。
const pySpaceClass = `[\t\n\v\f\r \x{1c}-\x{1f}\x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]`

// 去掉 \r \t \n（decode.py: replace("\r","").replace("\t","").replace("\n","")）
var reLineBreaks = strings.NewReplacer("\r", "", "\t", "", "\n", "")

// reTagStrip 对应 re.sub(r"<[^>]+>", "", s)
var reTagStrip = regexp.MustCompile(`<[^>]+>`)

// reWS 对应 re.sub(r"\s+", " ", s)
var reWS = regexp.MustCompile(pySpaceClass + `+`)

// log 解析模块共用的日志器（对应 decode.py 里的 logger）
func log() *logx.Logger { return logx.Default() }

// ---------------------------------------------------------------------------
// 通用取值助手：Python 的 dict.get(...) / 真值判断在 Go 里的等价实现
// ---------------------------------------------------------------------------

// anyString 复刻 Python 的 str(value) 语义（用于 JSON 解出来的 any）
func anyString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		// Python: str(True) == "True"
		if x {
			return "True"
		}
		return "False"
	case float64:
		// Python json 的 int 走 str(int)，float 走 str(float)；Go 统一按最短表示
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}

// truthy 复刻 Python 的真值判断（None/False/0/""/[]/{}/() 为假）
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case float32:
		return x != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	case json.Number:
		f, err := x.Float64()
		return err != nil || f != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	default:
		return true
	}
}

// anyInt 把 JSON 解出的值转成 int（playTime / reportTimeInterval 用）
func anyInt(v any) int {
	switch x := v.(type) {
	case nil:
		return 0
	case float64:
		return int(x)
	case float32:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case bool:
		if x {
			return 1
		}
		return 0
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0
		}
		return int(f)
	default:
		f, err := strconv.ParseFloat(anyString(x), 64)
		if err != nil {
			return 0
		}
		return int(f)
	}
}

// getString 等价 card.get(key, "")：键不存在或值为 null 时返回空串
func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return anyString(m[key])
}

// getIntDefault 等价 m.get(key, def) 的整数版本
func getIntDefault(m map[string]any, key string, def int) int {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	return anyInt(v)
}

// getMap 取出值为对象的字段，非对象时返回空 map（等价 Python 里 .get(key, {}) 后再 .get 的用法）
func getMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	if sub, ok := m[key].(map[string]any); ok && sub != nil {
		return sub
	}
	return map[string]any{}
}

// ---------------------------------------------------------------------------
// decode_course_list
// ---------------------------------------------------------------------------

// DecodeCourseList 解析课程列表页面，跳过未开放课程，对应 Python 的 decode_course_list。
func DecodeCourseList(htmlText string) []Course {
	log().Trace("开始解码课程列表...")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		log().Error("课程列表 HTML 解析失败: %s", err.Error())
		return []Course{}
	}

	courses := []Course{}
	doc.Find("div.course").Each(func(_ int, course *goquery.Selection) {
		// 跳过未开放课程
		if course.Find("a.not-open-tip").Length() > 0 || course.Find("div.not-open-tip").Length() > 0 {
			return
		}

		cpi := ""
		if href, ok := course.Find("a").First().Attr("href"); ok {
			if m := reCourseCPI.FindStringSubmatch(href); len(m) > 1 {
				cpi = m[1]
			}
		}

		desc := ""
		if p := course.Find("p.margint10").First(); p.Length() > 0 {
			desc = attrOr(p, "title", "")
		}

		courses = append(courses, Course{
			ID:       attrOr(course, "id", ""),
			Info:     attrOr(course, "info", ""),
			RoleID:   attrOr(course, "roleid", ""),
			ClazzID:  attrOr(course.Find("input.clazzId").First(), "value", ""),
			CourseID: attrOr(course.Find("input.courseId").First(), "value", ""),
			CPI:      cpi,
			Title:    attrOr(course.Find("span.course-name").First(), "title", ""),
			Desc:     desc,
			Teacher:  attrOr(course.Find("p.color3").First(), "title", ""),
		})
	})

	return courses
}

// ---------------------------------------------------------------------------
// decode_course_folder
// ---------------------------------------------------------------------------

// DecodeCourseFolder 解析二级课程列表（文件夹），对应 Python 的 decode_course_folder。
func DecodeCourseFolder(htmlText string) []Folder {
	log().Trace("开始解码二级课程列表...")

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		log().Error("二级课程列表 HTML 解析失败: %s", err.Error())
		return []Folder{}
	}

	folders := []Folder{}
	doc.Find("ul.file-list>li").Each(func(_ int, li *goquery.Selection) {
		fileID, ok := li.Attr("fileid")
		if !ok || fileID == "" {
			// Python: if not course.attrs.get("fileid"): continue
			return
		}
		folders = append(folders, Folder{
			ID:     fileID,
			Rename: attrOr(li.Find("input.rename-input").First(), "value", ""),
		})
	})

	return folders
}

// ---------------------------------------------------------------------------
// decode_course_point
// ---------------------------------------------------------------------------

// DecodeCoursePoint 解析章节列表页面，对应 Python 的 decode_course_point + _extract_points_from_chapter。
func DecodeCoursePoint(htmlText string) PointList {
	log().Trace("开始解码章节列表...")

	result := PointList{HasLocked: false, Points: []Point{}}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlText))
	if err != nil {
		log().Error("章节列表 HTML 解析失败: %s", err.Error())
		return result
	}

	doc.Find("div.chapter_unit").Each(func(_ int, chapterUnit *goquery.Selection) {
		points := extractPointsFromChapter(chapterUnit)
		for _, p := range points {
			if p.NeedUnlock {
				result.HasLocked = true
			}
		}
		result.Points = append(result.Points, points...)
	})

	return result
}

// extractPointsFromChapter 对应 Python 的 _extract_points_from_chapter。
func extractPointsFromChapter(chapterUnit *goquery.Selection) []Point {
	points := []Point{}

	chapterUnit.Find("li").Each(func(_ int, rawPoint *goquery.Selection) {
		// Python: point = raw_point.div —— 取第一个直接子 div；没有则报错，这里容错跳过
		point := rawPoint.ChildrenFiltered("div").First()
		if point.Length() == 0 {
			return
		}
		pointID, ok := point.Attr("id")
		if !ok {
			return
		}
		// Python: re.findall(r"^cur(\d{1,20})$", id)[0] —— 不匹配会 IndexError，这里跳过
		m := rePointID.FindStringSubmatch(pointID)
		if len(m) < 2 {
			return
		}
		id := m[1]

		title := strings.TrimSpace(strings.ReplaceAll(point.Find("a.clicktitle").First().Text(), "\n", ""))

		// 提取任务数量：默认 1；存在 knowledgeJobCount 时取其 value，
		// 否则再看 bntHoverTips 是否含"解锁"（elif 语义必须保留）
		jobCount := "1"
		needUnlock := false
		tips := point.Find("span.bntHoverTips").First()
		if inp := point.Find("input.knowledgeJobCount").First(); inp.Length() > 0 {
			jobCount = attrOr(inp, "value", "")
		} else if tips.Length() > 0 && strings.Contains(tips.Text(), "解锁") {
			needUnlock = true
		}

		hasFinished := false
		if tips.Length() > 0 && strings.Contains(tips.Text(), "已完成") {
			hasFinished = true
		}

		points = append(points, Point{
			ID:          id,
			Title:       title,
			JobCount:    jobCount,
			HasFinished: hasFinished,
			NeedUnlock:  needUnlock,
		})
	})

	return points
}

// attrOr 读取属性，缺失时返回默认值（Python 里 attrs[...] 缺失会抛异常，这里统一容错）
func attrOr(s *goquery.Selection, name, def string) string {
	if s == nil || s.Length() == 0 {
		return def
	}
	if v, ok := s.Attr(name); ok {
		return v
	}
	return def
}
