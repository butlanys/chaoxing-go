# 纯 Go 复刻 ddddocr 验证码识别：实现与验证记录

**结论：跑通了。** 纯 Go、零外部依赖、模型内嵌进二进制。

## 最终形态

- `internal/ocr` —— 完整推理引擎：二进制模型格式 + 数据驱动执行器 + 全部算子 +
  与 PIL 逐像素对齐的预处理 + CTC 解码
- 模型内嵌（`//go:embed`），**不依赖 onnxruntime 动态库、不依赖 Python、不需要 HTTP OCR 服务**
- 模型：ddddocr 的 `common.onnx`（即 `DdddOcr(beta=True)` 用的浮点模型），
  输出层按超星字符集裁剪到 63 类（`[0-9a-zA-Z]` + CTC blank），20.7MB f32

## 为什么选浮点模型而不是量化模型

Python 原版 chaoxing 用的是 `DdddOcr(show_ad=False)`，对应 `common_old.onnx`
（ORT 动态量化版）。但量化版**不适合纯 Go 复刻**：

```
scale = (max(x) - min(x)) / 255     # max/min 差 1 ULP，整个 scale 偏移
q     = round(x / scale + zp)       # round() 不连续，一批量化值同时翻转
```

量化 scale 取自张量极值，1 ULP 的浮点噪声不是翻转一个值，而是通过 scale 翻转一批值。
实测一个 `181_quantized` 元素翻转 → `429_output_quantized` 有 382/35328 个元素偏差达 91
→ 逐层放大。浮点模型没有这一环，1 ULP 差不会级联。

另外量化版在同一批 394 张图上准确率也更低（49.0% vs 60.4%），所以浮点版在两方面都更优。

## 两个把准确率打到 26% 的真 bug

修之前：纯 Go 绝对准确率 **26.1%**，Python **60.4%**。两个都是实现 bug，不是数值敏感 ——
判据是：给 ORT 的输入加 1e-4 扰动，它的 logits 只变 0.0061、argmax 完全不变，
而我的实现偏差 19.96，是它的 3000 倍。网络根本不敏感。

### bug 1：`--f16` 压缩权重

模型文件一开始用 float16 存权重（体积减半）。float16 只有 10 位尾数，
权重精度掉到 ~5e-4 —— 用**完全相同的输入**喂 `opConv`，仍偏离 ORT 1.5e-3，
而 ORT 自身偏离 float64 精确值只有 1e-5。改回 f32 后偏差降到 6.7e-06。

**教训**：float16 存推理权重对深层网络是危险的，误差会被逐层放大。
量化模型（权重本就是 int8）上做 f16 没问题，浮点模型上不行。

### bug 2：双向 LSTM 的反向判断

```go
// 错：双向时 dir == "bidirectional"，这个条件永远不成立
if dir == "reverse" { t = seq - 1 - s }

// 对
if dir == "reverse" || (dir == "bidirectional" && d == 1) { t = seq - 1 - s }
```

第 2 个方向一直在**正向**计算而不是反向。表现很隐蔽：LSTM 输出前几个值与 ORT
差 2.4e-8（看着完全正常），但约一半元素（d=1 那一半）错，最大偏差 1.98。
改精度（float32→float64）偏差**一位都不变**，这个"数值修复无效"的现象正是
结构性 bug 的信号。

## 验证方法（可复跑）

逐层比对是关键工具：把 ORT 的每个中间张量导出来做黄金参考，Go 侧按**执行顺序**
比对，第一个偏离的算子就是凶手。

```bash
# 1. 导出可逐层取数的参考模型
python3 scripts/export_intermediates.py <model.onnx> /tmp/ref.onnx

# 2. 导出某张图的全部中间张量与预处理后的输入
OCR_REF_MODEL=/tmp/ref.onnx python3 scripts/dump_layers.py <图片> /tmp/layers.bin

# 3. Go 侧逐层比对（第一个偏离的算子）
OCR_LAYERS=/tmp/layers.bin go test -run TestFindFirstDivergence -v ./internal/ocr/

# 4. 单独隔离某个算子（喂黄金输入，排除上游误差传导）
OCR_LAYERS=/tmp/layers.bin go test -run 'TestConvAgainstGoldenInput|TestLSTMAgainstGoldenInput' -v ./internal/ocr/

# 5. 端到端一致率
OCR_BATCH_DIR=<图片目录> OCR_BATCH_OUT=/tmp/go.json go test -timeout 40m -run TestBatchAgreement ./internal/ocr/
```

注意：比对中间张量必须关掉图优化（`ORT_DISABLE_ALL`），否则中间值被折叠。

## 预处理对齐

复刻 ddddocr 的 `convert("RGB") -> resize((tw,64), LANCZOS) -> convert("L")`。
两处细节不对齐结果就会漂：

- **RGB→L 用 16 位定点式** `(r*19595 + g*38470 + b*7471 + 0x8000) >> 16`，
  不是教科书的 `(r*299+g*587+b*114)/1000`（实测部分像素差 1）
- **PIL 的 LANCZOS 是定点实现**：系数用 22 位精度量化、两次分离卷积的中间结果
  落到 uint8 并截断，不能用浮点版 Lanczos 直接替代

实测 PNG 输入下 11648 个像素里只有 1~4 个差 ±1 LSB。
JPEG 输入会叠加解码器差异（Go 与 libjpeg 不是位级一致），下采样倍数越大越明显，
测试里对 JPEG 只断言偏差幅度、不断言差异像素数。

## 模型与字符集是严格配套的

用错字符集会得到"能跑但全是乱码"的结果，看起来像模型坏了，其实是配置错了：

```python
DdddOcr(show_ad=False)   # -> common_old.onnx + CHARSET_OLD
DdddOcr(beta=True)       # -> common.onnx     + CHARSET_BETA
```

这两个模型在同一批 394 张图上输出 **0 张一致**，是完全不同的模型，不是同一模型的
不同精度版本。`scripts/convert_ddddocr.py` 用 `--model float|quant` 选择，各自断言
对应的算子分布。

## 量化模型的算子语义（已逐算子反推验证，代码保留在 ops_quant.go / ops_lstm.go）

如果以后要支持量化模型，这几点是实验排除出来的：

- **门顺序是 ONNX 的 iofc**，不是常见的 ifco（用错偏差从 1e-7 涨到 2.0）
- **W/R 布局是 `[dirs, input|hidden, 4H]`**，相对于 ONNX 浮点 LSTM 的 W 是转置的
  （浮点模型是 `[dirs, 4H, input]`，两者不能混用）
- **循环路径也必须量化 h**：只用反量化权重做浮点乘，偏差 1.4e-3；量化后 1.8e-7
- **X 是整段一次量化**而非逐时间步：逐时间步会让偏差涨到 0.19

## 工具坑记录

- onnxruntime 默认不让取中间张量（`Invalid output name:390`），
  `onnx.utils.extract_model` 又要求 value_info 带 shape。正解：先
  `shape_inference.infer_shapes(m, strict_mode=False)` 拿到全部类型，
  再把中间张量直接追加为 `graph.output`（见 `scripts/export_intermediates.py`）
- `v.dtype not in {np.float32: 0}` 恒为真 ——
  `hash(np.dtype('float32')) != hash(np.float32)`，要用 `str(dtype)` 做键
- `go test -timeout 1800` 报 parse error，必须带单位（`40m`）
- Go 里 `for t := 0; ...` 会遮蔽 `*testing.T` 参数，导致 `t.Logf` 编译错误

## 参考：ORT 的 sigmoid/tanh 无法逐位复刻（但不影响最终结果）

ORT 的激活走 Eigen 的向量化实现。用 ORT 自己的 Sigmoid 真值表验证，标准写法都对不上：
`1/(1+exp(-x))` float64 差 60.6%、float32 差 61.5%、
`0.5*(1+tanh(x/2))` float32 tanh 差 32.8%、Eigen `pexp_float` 移植差 61.6%。

（`0.5*(1+tanh(x/2))` 这个形式是对的 —— x=-30 时 ORT 输出精确的 0，而 exp 形式给
9.36e-14，说明 ORT 走 tanh 路径。）

但对**浮点模型**这不影响结果：网络对 1 ULP 级扰动不敏感（输入加 1e-4 扰动，
logits 只变 0.0061、argmax 不变），所以 sigmoid 的 1 ULP 差异不会改变识别结果。
真正致命的是上面那两个 bug。
