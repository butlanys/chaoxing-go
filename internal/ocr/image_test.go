package ocr

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestPreprocessMatchesPILGolden 逐像素比对 Go 的预处理与 PIL 的结果。
//
// 黄金数据由 PIL 按 ddddocr 的完全相同步骤生成：
//
//	convert("RGB") -> resize((tw,64), LANCZOS) -> convert("L")
//
// 这个测试能精确定位问题出在预处理还是模型：
// 预处理只要有一个像素不同，后面的识别就可能漂。
func TestPreprocessMatchesPILGolden(t *testing.T) {
	goldenDir := filepath.Join("testdata", "golden")
	entries, err := os.ReadDir(goldenDir)
	if err != nil {
		t.Skipf("没有黄金预处理数据: %v", err)
	}

	for _, e := range entries {
		name := e.Name()
		if filepath.Ext(name) != ".bin" {
			continue
		}
		imgName := name[:len(name)-len(".gray.bin")]

		t.Run(imgName, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(goldenDir, name))
			if err != nil {
				t.Fatal(err)
			}
			if len(raw) < 8 {
				t.Fatal("黄金文件过短")
			}
			wantW := int(binary.LittleEndian.Uint32(raw[0:]))
			wantH := int(binary.LittleEndian.Uint32(raw[4:]))
			want := raw[8:]

			imgData, err := os.ReadFile(filepath.Join("testdata", imgName))
			if err != nil {
				t.Fatal(err)
			}
			pix, gotW, err := Preprocess(imgData)
			if err != nil {
				t.Fatal(err)
			}
			if gotW != wantW {
				t.Fatalf("缩放宽度 = %d, 期望 %d", gotW, wantW)
			}
			if len(pix) != wantW*wantH {
				t.Fatalf("像素数 %d, 期望 %d", len(pix), wantW*wantH)
			}
			if len(want) != len(pix) {
				t.Fatalf("黄金像素数 %d 与实得 %d 不符", len(want), len(pix))
			}

			var diffCount, maxDiff int
			firstAt := -1
			for i := range pix {
				got := int(pix[i]*255 + 0.5)
				d := got - int(want[i])
				if d < 0 {
					d = -d
				}
				if d != 0 {
					diffCount++
					if firstAt < 0 {
						firstAt = i
					}
					if d > maxDiff {
						maxDiff = d
					}
				}
			}
			// 预处理是"基本逐像素对齐"而非位级对齐：PIL 的 LANCZOS 是定点实现
			// （22 位系数精度 + 两次分离卷积中间结果落 uint8），与浮点实现的舍入
			// 路径无法完全一致。实测 PNG 输入下 11648 个像素里只有 1~4 个差 ±1 LSB。
			//
			// PNG 解码是位级精确的，所以对 PNG 可以断言"差异像素数"；
			// JPEG 则叠加了解码器差异（Go 与 libjpeg 不是位级一致），且下采样倍数越大
			// 越明显（344x384 -> 57x64 时约 25% 的像素差 1~4 LSB），
			// 因此对 JPEG 只断言偏差幅度、不断言数量。
			isJPEG := filepath.Ext(imgName) == ".jpeg" || filepath.Ext(imgName) == ".jpg"
			maxLsb := 1
			if isJPEG {
				maxLsb = 4
			}
			if !isJPEG {
				maxAllowed := len(pix) / 100 // 1%
				if maxAllowed < 16 {
					maxAllowed = 16
				}
				if diffCount > maxAllowed {
					t.Errorf("预处理有 %d/%d 个像素与 PIL 不同，超过容差 %d",
						diffCount, len(pix), maxAllowed)
				}
			}
			if maxDiff > maxLsb {
				t.Errorf("预处理像素最大偏差 %d LSB，超过容差 %d (首个不同位置 %d, y=%d,x=%d: got=%d want=%d)",
					maxDiff, maxLsb, firstAt, firstAt/wantW, firstAt%wantW,
					int(pix[firstAt]*255+0.5), want[firstAt])
			}
			t.Logf("与 PIL 不同的像素: %d/%d，最大偏差 %d LSB%s",
				diffCount, len(pix), maxDiff, map[bool]string{true: "（JPEG 解码器差异，仅校验幅度）"}[isJPEG])
		})
	}
}
