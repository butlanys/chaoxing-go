// Package consts 存放与 Python 原版逐字一致的全局常量。
package consts

import "time"

const (
	// AESKey 是超星登录接口 (passport2.chaoxing.com/fanyalogin) 的账号/密码加密密钥。
	//
	// ⚠️ 这不是本项目的凭据，而是**目标平台的公开协议常量**：该密钥硬编码在超星登录页的
	// 前端 JS 里，任何人打开登录页都能看到，且在原项目
	// https://github.com/Samueli924/chaoxing (api/config.py: AESKey) 中同样明文存在。
	// 密钥扫描器（GitGuardian / gitleaks 等）会把它报成 "Generic High Entropy Secret"，
	// 属于误报，已在 .gitleaks.toml 与 .gitguardian.yaml 中说明并加白。
	// 移除或"轮换"它会让登录功能直接失效——它必须与超星服务端保持一致。
	AESKey = "u2oh6Vu^HWe4_AES"

	// VideoEncSalt 是超星视频/音频进度上报接口 enc 参数的 md5 拼接盐值。
	//
	// ⚠️ 同上：这是目标平台的公开协议常量，硬编码在超星播放器前端 JS 中，原项目
	// (api/base.py: Chaoxing.get_enc) 中同样明文存在，并非本项目的凭据。
	// 扫描器误报，已加白。
	VideoEncSalt = "d_yHJ!$pdA~5"

	// Threshold 视频进度轮询间隔（秒）
	Threshold = 1 * time.Second
	// RateInterval 普通接口最小请求间隔（秒）
	RateInterval = 0.5
	// VideoLogInterval 视频进度上报最小间隔（秒）
	VideoLogInterval = 2
	// DefaultTimeout 单请求超时
	DefaultTimeout = 5 * time.Second
)

// Headers 通用请求头（与原版 GlobalConst.HEADERS 一致）
var Headers = map[string]string{
	"User-Agent":         "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Safari/537.36",
	"sec-ch-ua":          `"Chromium";v="118", "Google Chrome";v="118", "Not=A?Brand";v="99"`,
	"sec-ch-ua-mobile":   "?0",
	"sec-ch-ua-platform": `"Windows"`,
}

// VideoHeaders 视频状态/进度接口 Referer
var VideoHeaders = map[string]string{
	"Referer": "https://mooc1.chaoxing.com/ananas/modules/video/index.html?v=2025-0725-1842",
}

// AudioHeaders 音频状态/进度接口 Referer
var AudioHeaders = map[string]string{
	"Referer": "https://mooc1.chaoxing.com/ananas/modules/audio/index_new.html?v=2025-0725-1842",
}

// LiveHeaders 直播接口 Referer
var LiveHeaders = map[string]string{
	"Referer": "https://mooc1.chaoxing.com/ananas/modules/live/index.html?v=2022-1214-1139",
}
