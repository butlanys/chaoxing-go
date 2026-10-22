package htmlx

import "testing"

// 课程列表页面片段（含未开放的 a.not-open-tip / div.not-open-tip 与缺 desc 的课程）
const courseListHTML = `
<html><body>
<div class="course" id="course_1001" info="1001" roleid="3">
  <input class="clazzId" type="hidden" value="2001">
  <input class="courseId" type="hidden" value="1001">
  <a href="/mooc-ans/mycourse/studentcourse?courseid=1001&amp;clazzid=2001&amp;cpi=123456&amp;ut=s" title="高等数学">
    <span class="course-name" title="高等数学">高等数学</span>
  </a>
  <p class="margint10" title="2024-2025学年第一学期">2024-2025学年第一学期</p>
  <p class="color3" title="张三">张三</p>
</div>
<div class="course" id="course_1002" info="1002" roleid="3">
  <a class="not-open-tip" href="javascript:;">课程未开放</a>
  <input class="clazzId" value="2002">
  <input class="courseId" value="1002">
</div>
<div class="course" id="course_1003" info="1003" roleid="1">
  <input class="clazzId" value="2003">
  <input class="courseId" value="1003">
  <a href="/mooc-ans/mycourse/studentcourse?courseid=1003&amp;cpi=654321&amp;ut=s"></a>
  <span class="course-name" title="线性代数">线性代数</span>
  <p class="color3" title="李四">李四</p>
  <div class="not-open-tip">暂未开放</div>
</div>
</body></html>`

func TestDecodeCourseList(t *testing.T) {
	cases := []struct {
		name string
		html string
		want []Course
	}{
		{
			name: "跳过未开放课程并解析字段",
			html: courseListHTML,
			want: []Course{
				{
					ID: "course_1001", Info: "1001", RoleID: "3",
					ClazzID: "2001", CourseID: "1001", CPI: "123456",
					Title: "高等数学", Desc: "2024-2025学年第一学期", Teacher: "张三",
				},
			},
		},
		{
			name: "空字符串",
			html: "",
			want: []Course{},
		},
		{
			name: "纯文本",
			html: "这不是 HTML",
			want: []Course{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DecodeCourseList(c.html)
			if len(got) != len(c.want) {
				t.Fatalf("课程数量 = %d, 期望 %d (%+v)", len(got), len(c.want), got)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("第 %d 个课程 = %+v, 期望 %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}

// 二级课程列表：无 fileid 的 li 应被跳过
const courseFolderHTML = `
<ul class="file-list">
  <li fileid="f1"><input class="rename-input" type="text" value="第一章"></li>
  <li><input class="rename-input" value="没有 fileid 应被跳过"></li>
  <li fileid="f2"><input class="rename-input" value="第二章"></li>
</ul>`

func TestDecodeCourseFolder(t *testing.T) {
	cases := []struct {
		name string
		html string
		want []Folder
	}{
		{
			name: "解析 fileid 与 rename",
			html: courseFolderHTML,
			want: []Folder{{ID: "f1", Rename: "第一章"}, {ID: "f2", Rename: "第二章"}},
		},
		{name: "空字符串", html: "", want: []Folder{}},
		{name: "无 file-list", html: `<ul><li fileid="x"></li></ul>`, want: []Folder{}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DecodeCourseFolder(c.html)
			if len(got) != len(c.want) {
				t.Fatalf("文件夹数量 = %d, 期望 %d (%+v)", len(got), len(c.want), got)
			}
			for i := range c.want {
				if got[i] != c.want[i] {
					t.Errorf("第 %d 个文件夹 = %+v, 期望 %+v", i, got[i], c.want[i])
				}
			}
		})
	}
}

// 章节列表：含已完成 / 解锁 / 缺 id / 无 div 的 li
const coursePointHTML = `
<div class="chapter_unit">
  <ul>
    <li>
      <div id="cur12">
        <a class="clicktitle" href="javascript:;">
1.1 绪论
</a>
        <input class="knowledgeJobCount" type="hidden" value="3">
      </div>
    </li>
    <li>
      <div id="cur13">
        <a class="clicktitle">1.2 极限</a>
        <span class="bntHoverTips">解锁</span>
      </div>
    </li>
    <li>
      <div id="cur14">
        <a class="clicktitle">1.3 连续</a>
        <span class="bntHoverTips">已完成</span>
      </div>
    </li>
    <li>
      <div id="cur15">
        <a class="clicktitle">1.4 同时有任务数与解锁提示</a>
        <input class="knowledgeJobCount" value="2">
        <span class="bntHoverTips">解锁</span>
      </div>
    </li>
    <li>
      <div>
        <a class="clicktitle">没有 id 属性应被跳过</a>
      </div>
    </li>
    <li>
      <a class="clicktitle">没有直接子 div 应被跳过</a>
    </li>
  </ul>
</div>`

func TestDecodeCoursePoint(t *testing.T) {
	got := DecodeCoursePoint(coursePointHTML)

	want := []Point{
		{ID: "12", Title: "1.1 绪论", JobCount: "3", HasFinished: false, NeedUnlock: false},
		{ID: "13", Title: "1.2 极限", JobCount: "1", HasFinished: false, NeedUnlock: true},
		{ID: "14", Title: "1.3 连续", JobCount: "1", HasFinished: true, NeedUnlock: false},
		// knowledgeJobCount 存在时走 if 分支，不再判断"解锁"（Python 的 elif 语义）
		{ID: "15", Title: "1.4 同时有任务数与解锁提示", JobCount: "2", HasFinished: false, NeedUnlock: false},
	}

	if len(got.Points) != len(want) {
		t.Fatalf("章节点数量 = %d, 期望 %d (%+v)", len(got.Points), len(want), got.Points)
	}
	for i := range want {
		if got.Points[i] != want[i] {
			t.Errorf("第 %d 个章节点 = %+v, 期望 %+v", i, got.Points[i], want[i])
		}
	}
	if !got.HasLocked {
		t.Errorf("HasLocked = false, 期望 true")
	}

	// 无锁定内容的章节列表
	unlocked := DecodeCoursePoint(`<div class="chapter_unit"><ul><li><div id="cur1"><a class="clicktitle">只有标题</a></div></li></ul></div>`)
	if len(unlocked.Points) != 1 || unlocked.HasLocked {
		t.Errorf("无锁章节解析错误: %+v", unlocked)
	}

	// 空/纯文本
	if pl := DecodeCoursePoint(""); len(pl.Points) != 0 || pl.HasLocked {
		t.Errorf("空字符串应返回空章节列表, 实际 %+v", pl)
	}
	if pl := DecodeCoursePoint("纯文本"); len(pl.Points) != 0 {
		t.Errorf("纯文本应返回空章节列表, 实际 %+v", pl)
	}
}
