# 第三方组件与许可

本项目自身以 **GPL-3.0** 发布（见 `LICENSE`）。以下是随本项目一起分发的第三方成果。

---

## ddddocr（验证码识别模型与字符集）

- 项目：https://github.com/sml2h3/ddddocr
- 版本：1.6.1
- 许可：MIT

本项目的验证码识别能力基于 ddddocr 的识别模型。`internal/ocr/data/ddddocr.bin`
是 ddddocr 的 `common.onnx`（`DdddOcr(beta=True)` 使用的浮点模型）经过以下
**无损或受控**变换后的产物：

1. 输出层按超星验证码字符集裁剪：8210 类 → 63 类（CTC blank + `[0-9a-zA-Z]`）。
   只保留这些类别对应的权重行，保留类别的取值完全不变，属无损裁剪。
2. 序列化为本项目自定义的二进制格式（见 `internal/ocr/format.go`），含图结构、
   权重、字符集与节点属性。
3. 权重以 **float32 原样存储**（不使用 float16 压缩 —— 实测 f16 会让深层卷积的
   数值误差累积，准确率显著下降）。

推理引擎（`internal/ocr/*.go`）为本项目用纯 Go 重新实现，未使用 ddddocr 的任何代码。

### MIT License

```
The MIT License (MIT)

Copyright © 2022 ddddocr contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
```

---

## 上游项目 Samueli924/chaoxing

- 项目：https://github.com/Samueli924/chaoxing
- 许可：GPL-3.0

本项目是该项目的 Go 语言重写二创，协议行为与之逐字对齐，因此继承 GPL-3.0。

---

## Go 依赖

| 模块 | 许可 |
| --- | --- |
| `github.com/PuerkitoBio/goquery` | BSD-3-Clause |
| `gopkg.in/ini.v1` | Apache-2.0 |
| `github.com/wailsapp/wails/v2` | MIT |
| `golang.org/x/net` | BSD-3-Clause |

完整依赖树见 `go.mod` / `go.sum`。
