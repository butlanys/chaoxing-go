package htmlx

import (
	"regexp"
	"strconv"
)

// 本文件对应 Python api/base.py 的 _parse_work_record_list（作答记录列表）。

var (
	// r'viewNum">第(\d+)次'
	reRecordTimes = regexp.MustCompile(`viewNum">第(\d+)次`)
	// r'viewScore">([\d.]+)分'
	reRecordScore = regexp.MustCompile(`viewScore">([\d.]+)分`)
)

// DecodeWorkRecordList 解析 /work/record-list 页面，返回 (作答序号, 成绩) 列表。
// 次数与成绩数量不一致时成绩一律按 0 处理（放弃分数捷径，仅做逐题比对）。
func DecodeWorkRecordList(htmlText string) []RecordItem {
	records := []RecordItem{}

	timesList := reRecordTimes.FindAllStringSubmatch(htmlText, -1)
	scores := reRecordScore.FindAllStringSubmatch(htmlText, -1)

	matched := len(timesList) == len(scores)
	if !matched {
		log().Warn("作答记录次数/成绩数量不匹配(%d/%d), 成绩按0处理", len(timesList), len(scores))
	}

	for i, t := range timesList {
		if len(t) < 2 {
			continue
		}
		times, err := strconv.Atoi(t[1])
		if err != nil {
			// Python: int(t) 抛 ValueError -> continue
			continue
		}

		score := 0.0
		if matched {
			score, err = strconv.ParseFloat(scores[i][1], 64)
			if err != nil {
				continue
			}
		}
		records = append(records, RecordItem{Times: times, Score: score})
	}

	return records
}
