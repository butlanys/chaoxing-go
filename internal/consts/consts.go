// Package consts 存放与 Python 原版逐字一致的全局常量。
package consts

import "time"

const (
	// AESKey 超星登录密码/账号 AES 加密密钥（与原版 api/config.py 一致）
	AESKey = "u2oh6Vu^HWe4_AES"
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
