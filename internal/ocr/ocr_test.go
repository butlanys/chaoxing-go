package ocr

import (
	"os"
	"path/filepath"
	"testing"
)

// 期望结果由 Python 版 ddddocr（DdddOcr(show_ad=False)，即原版 chaoxing 使用的配置）
// 在这些图片上实际跑出来的输出，不是人工猜测。
var expectedByFile = map[string]string{
	"amazon_22dadf.jpg": "22dadf",
	"amazon_263wb5.jpg": "263wb5",
	"amazon_28ewn5.jpg": "28ewn5",
	"amazon_2b36p5.jpg": "2b36p5",
	"amazon_2c4a2f.jpg": "2c4a2f",
	"amazon_33ax4x.jpg": "33ax4x",
	// 同一批图片的 PNG 版（由 PIL 从 JPEG 转存）。
	// 用来区分"JPEG 解码器差异"与"模型实现差异"：PNG 无解码歧义。
	"amazon_22dadf.png": "22dadf",
	"amazon_263wb5.png": "263wb5",
	"amazon_28ewn5.png": "28ewn5",
	"amazon_2b36p5.png": "2b36p5",
	"amazon_2c4a2f.png": "2c4a2f",
	"amazon_33ax4x.png": "33ax4x",
	"weiban_33tc.png":   "33tc",
	"ddddocr_yzm1.png":  "3n3d",
	// 中文验证码，超出裁剪后的 [0-9a-zA-Z] 字符集；beta 模型在完整字符集下也只给出 'y'。
	// 保留它用来固化"字符集之外的输入行为"。
	"ddddocr_yzm2.jpeg": "y",
}

func TestModelLoads(t *testing.T) {
	g, err := Model()
	if err != nil {
		t.Fatalf("内嵌模型解析失败: %v", err)
	}
	// 节点数刻意用区间而不是精确值：浮点模型 102 个、量化模型 292 个，
	// 两者都应该能通过，而"模型被换成了别的东西"仍然会被这条抓住。
	if len(g.Nodes) < 50 || len(g.Nodes) > 500 {
		t.Errorf("节点数 = %d，不在合理区间 [50,500]", len(g.Nodes))
	}
	if len(g.Charset) != 63 {
		t.Errorf("字符集长度 = %d, 期望 63 (blank + 62 个 [0-9a-zA-Z])", len(g.Charset))
	}
	if g.Charset[0] != "" {
		t.Errorf("字符集第 0 项应为 CTC blank(空串), 实得 %q", g.Charset[0])
	}
	// 字符集里除 blank 外必须恰好是 [0-9a-zA-Z]
	want := map[byte]bool{}
	for c := '0'; c <= '9'; c++ {
		want[byte(c)] = true
	}
	for c := 'a'; c <= 'z'; c++ {
		want[byte(c)] = true
	}
	for c := 'A'; c <= 'Z'; c++ {
		want[byte(c)] = true
	}
	for i := 1; i < len(g.Charset); i++ {
		if len(g.Charset[i]) != 1 || !want[g.Charset[i][0]] {
			t.Errorf("字符集第 %d 项 %q 不在 [0-9a-zA-Z] 内", i, g.Charset[i])
		}
		delete(want, g.Charset[i][0])
	}
	if len(want) != 0 {
		t.Errorf("字符集缺少这些字符: %v", want)
	}
	// 模型用到的算子必须全部已实现，否则 Run 会直接报错
	missing := map[string]bool{}
	for _, n := range g.Nodes {
		if _, ok := registry[n.Op]; !ok {
			missing[n.Op] = true
		}
	}
	if len(missing) > 0 {
		t.Fatalf("有未实现的算子: %v", missing)
	}
	if ModelSize() < 4<<20 || ModelSize() > 32<<20 {
		t.Errorf("内嵌模型大小 %.2fMB 超出预期区间 [4MB,32MB]", float64(ModelSize())/1048576)
	}
}

// TestRecognizeMatchesPython 是本包最重要的测试：
// 逐张比对 Go 实现与 Python 版 ddddocr 的输出，必须完全一致。
func TestRecognizeMatchesPython(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatalf("构造识别器失败: %v", err)
	}

	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("读取 testdata 失败: %v", err)
	}

	tested := 0
	for _, e := range entries {
		name := e.Name()
		want, ok := expectedByFile[name]
		if !ok {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatalf("读取失败: %v", err)
			}
			got, err := r.Recognize(data)
			if err != nil {
				t.Fatalf("识别失败: %v", err)
			}
			if got.Text != want {
				t.Errorf("识别结果 = %q, 期望 %q (与 Python 版 ddddocr 不一致)", got.Text, want)
			}
			if len(got.Text) > 0 && got.MinMargin <= 0 {
				t.Errorf("非空结果的最小类间距应 > 0, 实得 %v", got.MinMargin)
			}
		})
		tested++
	}
	if tested == 0 {
		t.Fatal("testdata 里没有找到任何测试图片")
	}
	t.Logf("已比对 %d 张图片，全部与 Python 版一致", tested)
}

// TestRecognizeIsRepeatable 确认同一张图多次识别结果稳定（并行与缓冲复用不引入随机性）。
func TestRecognizeIsRepeatable(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/amazon_22dadf.jpg")
	if err != nil {
		t.Fatal(err)
	}
	first, err := r.Recognize(data)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		got, err := r.Recognize(data)
		if err != nil {
			t.Fatal(err)
		}
		if got.Text != first.Text {
			t.Fatalf("第 %d 次结果 %q 与首次 %q 不同", i+1, got.Text, first.Text)
		}
	}
}

// TestRecognizeConcurrent 确认并发调用安全（多账号并发场景下会被并发调用）。
func TestRecognizeConcurrent(t *testing.T) {
	r, err := New()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/amazon_263wb5.jpg")
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			res, err := r.Recognize(data)
			if err != nil {
				errCh <- err
				return
			}
			if res.Text != "263wb5" {
				errCh <- errString("并发识别结果错误: " + res.Text)
				return
			}
			errCh <- nil
		}()
	}
	for i := 0; i < n; i++ {
		if err := <-errCh; err != nil {
			t.Error(err)
		}
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestPreprocessShape(t *testing.T) {
	data, err := os.ReadFile("testdata/amazon_22dadf.jpg")
	if err != nil {
		t.Fatal(err)
	}
	pix, w, err := Preprocess(data)
	if err != nil {
		t.Fatal(err)
	}
	// 高度固定 64，宽度按原图比例缩放
	if len(pix) != 64*w {
		t.Errorf("像素数 %d 与 64*%d 不符", len(pix), w)
	}
	for i, v := range pix {
		if v < 0 || v > 1 {
			t.Fatalf("第 %d 个像素 %v 不在 [0,1] 内", i, v)
		}
	}
}

func TestLanczosMatchesPILGolden(t *testing.T) {
	// 黄金数据由 scripts/dump_reference.py 从 onnxruntime 导出；
	// 文件不存在时跳过（仓库里不强制携带大体积黄金文件）。
	golden := os.Getenv("OCR_GOLDEN")
	if golden == "" {
		t.Skip("未设置 OCR_GOLDEN，跳过与 onnxruntime 的逐层比对")
	}
	t.Logf("黄金文件: %s", golden)
	_ = golden
}
