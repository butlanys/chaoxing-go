package captcha

import (
	"os"
	"testing"
)

// TestLocalSolver 确认未配置 ocr_url 时默认走本地内嵌模型，
// 且能正确识别真实的超星风格验证码。
func TestLocalSolver(t *testing.T) {
	s := New(nil, nil)
	if _, ok := s.(*LocalSolver); !ok {
		t.Fatalf("未配置 ocr_url 时应返回 LocalSolver，实得 %T", s)
	}

	// 用 internal/ocr 的测试图片做端到端验证
	cases := map[string]string{
		"../../internal/ocr/testdata/weiban_33tc.png":   "33tc",
		"../../internal/ocr/testdata/amazon_22dadf.png": "22dadf",
		"../../internal/ocr/testdata/amazon_263wb5.png": "263wb5",
	}
	for path, want := range cases {
		img, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", path, err)
		}
		got, err := s.Solve(img)
		if err != nil {
			t.Fatalf("识别 %s 失败: %v", path, err)
		}
		if got != want {
			t.Errorf("%s 识别结果 = %q, 期望 %q", path, got, want)
		}
	}
}

// TestLocalSolverEmptyInput 空输入必须报错而不是 panic
func TestLocalSolverEmptyInput(t *testing.T) {
	s := New(nil, nil)
	if _, err := s.Solve(nil); err == nil {
		t.Error("空输入应当返回错误")
	}
}

// TestHTTPOcrPreferredWhenConfigured 配置了 ocr_url 时应优先用 HTTP 服务
func TestHTTPOcrPreferredWhenConfigured(t *testing.T) {
	s := New(map[string]string{"ocr_url": "http://127.0.0.1:9/ocr"}, nil)
	if _, ok := s.(*HTTPOcrSolver); !ok {
		t.Fatalf("配置 ocr_url 时应返回 HTTPOcrSolver，实得 %T", s)
	}
}
