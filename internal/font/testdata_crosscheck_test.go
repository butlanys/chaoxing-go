package font

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestCrossCheckAgainstFontTools 用「外部参考实现」验证 Go 的 TTF 解析与字形哈希。
//
// 参考数据由 scripts/gen_font_fixture.py 生成：该脚本用 fontTools 合成一份 TTF，
// 再用原版 Python 项目 (/root/chaoxing-ref/api/cxsecret_font.py) 的 hash_glyph
// 逐字形算出 md5。本测试要求 Go 实现给出完全相同的哈希表，
// 从而证明「TTF 表解析 + 坐标增量还原 + flags repeat 展开 + hash 输入串拼接」四步都与原版一致。
//
// 重新生成参考数据：python3 scripts/gen_font_fixture.py
func TestCrossCheckAgainstFontTools(t *testing.T) {
	ttfPath := filepath.Join("testdata", "sample.ttf")
	jsonPath := filepath.Join("testdata", "sample_hashes.json")

	ttf, err := os.ReadFile(ttfPath)
	if err != nil {
		t.Fatalf("读取合成 TTF 失败: %v (请先运行 python3 scripts/gen_font_fixture.py)", err)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("读取参考哈希失败: %v (请先运行 python3 scripts/gen_font_fixture.py)", err)
	}

	var want map[string]string
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("参考哈希 JSON 解析失败: %v", err)
	}
	if len(want) == 0 {
		t.Fatal("参考哈希为空")
	}

	got, err := ParseTTFGlyphs(ttf)
	if err != nil {
		t.Fatalf("ParseTTFGlyphs 失败: %v", err)
	}

	// 1) 参考中的每个字形都必须命中且哈希一致
	for name, wantHash := range want {
		gotHash, ok := got[name]
		if !ok {
			t.Errorf("缺少字形 %s (参考哈希 %s)", name, wantHash)
			continue
		}
		if gotHash != wantHash {
			t.Errorf("字形 %s 哈希不一致:\n  go   = %s\n  参考 = %s", name, gotHash, wantHash)
		}
	}

	// 2) Go 不应多出参考之外的 uni* 字形
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("Go 多出参考中不存在的字形: %s", name)
		}
	}

	t.Logf("交叉验证通过: %d 个字形哈希与 fontTools + 原版 hash_glyph 完全一致", len(want))
}
