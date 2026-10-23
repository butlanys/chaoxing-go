package ocr

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// 模型文件格式（由 scripts/convert_ddddocr.py 生成，小端序）：
//
//	magic[8] = "CXOCR\0\0\0"
//	u32 version
//	u32 nInitializers
//	  每个权重: name(u8len+bytes), ndim(u8), dims(int64 * ndim),
//	           dtype(u8), nbytes(u64), data
//	u32 nNodes
//	  每个节点: op(u8len+bytes), nIn(u8), in*(u8len+bytes),
//	           nOut(u8), out*(u8len+bytes), nAttr(u8),
//	           属性: name(u8len+bytes), type(u8), payload
//	u32 nGraphInputs, name*
//	u32 nGraphOutputs, name*
//	u32 nCharset, (u8len+bytes)*
//
// 属性 payload 按类型：
//
//	1 INT    -> int64
//	2 FLOAT  -> float32
//	3 STRING -> u8len+bytes
//	4 INTS   -> u32 n, int64*n
//	5 FLOATS -> u32 n, float32*n
//	6 TENSOR -> ndim(u8), dims(int64*ndim), dtype(u8), nbytes(u64), data
// DType 是模型文件里记录的权重类型编码。
type DType uint8

const (
	// F32 单精度浮点
	F32 DType = 0
	// F16 半精度浮点（存储用，加载时展开为 F32）
	F16 DType = 1
	// I64 64 位整数（形状计算用）
	I64 DType = 2
	// U8 无符号 8 位（量化后的激活与权重）
	U8 DType = 3
	// I8 有符号 8 位（量化后的 LSTM 权重）
	I8 DType = 4
)

const modelMagic = "CXOCR\x00\x00\x00"

// 属性类型。
const (
	attrInt    = 1
	attrFloat  = 2
	attrString = 3
	attrInts   = 4
	attrFloats = 5
	attrTensor = 6
)

// Attr 是节点的一个属性值。
type Attr struct {
	Type   uint8
	I      int64
	F      float32
	S      string
	Ints   []int64
	Floats []float32
	T      *Tensor
}

// Int 取整型属性，缺省返回 def。
func (a Attr) Int(def int64) int64 {
	if a.Type == attrInt {
		return a.I
	}
	return def
}

// IntsOr 取整型数组属性。
func (a Attr) IntsOr(def []int64) []int64 {
	if a.Type == attrInts {
		return a.Ints
	}
	return def
}

// StringOr 取字符串属性。
func (a Attr) StringOr(def string) string {
	if a.Type == attrString {
		return a.S
	}
	return def
}

// Node 是图中的一个算子节点。
type Node struct {
	Op      string
	Inputs  []string
	Outputs []string
	Attrs   map[string]Attr
}

// Attr 取属性，不存在时返回零值。
func (n *Node) Attr(name string) Attr { return n.Attrs[name] }

// Graph 是一张可执行的推理图。
type Graph struct {
	Initializers map[string]*Tensor
	Nodes        []*Node
	Inputs       []string
	Outputs      []string
	// Charset 是输出类对应的字符表，索引 0 为 CTC blank。
	Charset []string
}

type reader struct {
	b   []byte
	pos int
}

var errTruncated = errors.New("模型文件被截断")

func (r *reader) need(n int) error {
	if r.pos+n > len(r.b) {
		return errTruncated
	}
	return nil
}

func (r *reader) u8() (uint8, error) {
	if err := r.need(1); err != nil {
		return 0, err
	}
	v := r.b[r.pos]
	r.pos++
	return v, nil
}

func (r *reader) u32() (uint32, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	v := binary.LittleEndian.Uint32(r.b[r.pos:])
	r.pos += 4
	return v, nil
}

func (r *reader) u64() (uint64, error) {
	if err := r.need(8); err != nil {
		return 0, err
	}
	v := binary.LittleEndian.Uint64(r.b[r.pos:])
	r.pos += 8
	return v, nil
}

func (r *reader) i64() (int64, error) {
	v, err := r.u64()
	return int64(v), err
}

func (r *reader) f32() (float32, error) {
	v, err := r.u32()
	return math.Float32frombits(v), err
}

func (r *reader) str() (string, error) {
	n, err := r.u8()
	if err != nil {
		return "", err
	}
	if err := r.need(int(n)); err != nil {
		return "", err
	}
	s := string(r.b[r.pos : r.pos+int(n)])
	r.pos += int(n)
	return s, nil
}

func (r *reader) bytes(n int) ([]byte, error) {
	if err := r.need(n); err != nil {
		return nil, err
	}
	b := r.b[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

func (r *reader) count() (int, error) {
	n, err := r.u32()
	if err != nil {
		return 0, err
	}
	// 防御：长度字段异常大时直接失败，避免 make 出巨大切片
	if n > 1<<24 {
		return 0, fmt.Errorf("模型文件长度字段异常: %d", n)
	}
	return int(n), nil
}

// ParseModel 解析内嵌的模型字节流。
func ParseModel(data []byte) (*Graph, error) {
	r := &reader{b: data}
	if len(data) < 8 || string(data[:8]) != modelMagic {
		return nil, errors.New("不是 chaoxing-go OCR 模型文件（magic 不匹配）")
	}
	r.pos = 8

	ver, err := r.u32()
	if err != nil {
		return nil, err
	}
	if ver != 1 {
		return nil, fmt.Errorf("不支持的模型文件版本: %d", ver)
	}

	g := &Graph{Initializers: make(map[string]*Tensor)}

	nInit, err := r.count()
	if err != nil {
		return nil, err
	}
	for i := 0; i < nInit; i++ {
		name, err := r.str()
		if err != nil {
			return nil, err
		}
		nd, err := r.u8()
		if err != nil {
			return nil, err
		}
		shape := make([]int, nd)
		for d := range shape {
			v, err := r.i64()
			if err != nil {
				return nil, err
			}
			shape[d] = int(v)
		}
		dt, err := r.u8()
		if err != nil {
			return nil, err
		}
		nb, err := r.u64()
		if err != nil {
			return nil, err
		}
		raw, err := r.bytes(int(nb))
		if err != nil {
			return nil, err
		}
		t, err := decodeTensor(shape, DType(dt), raw)
		if err != nil {
			return nil, fmt.Errorf("权重 %s: %w", name, err)
		}
		g.Initializers[name] = t
	}

	nNode, err := r.count()
	if err != nil {
		return nil, err
	}
	g.Nodes = make([]*Node, 0, nNode)
	for i := 0; i < nNode; i++ {
		op, err := r.str()
		if err != nil {
			return nil, err
		}
		nIn, err := r.u8()
		if err != nil {
			return nil, err
		}
		ins := make([]string, nIn)
		for k := range ins {
			if ins[k], err = r.str(); err != nil {
				return nil, err
			}
		}
		nOut, err := r.u8()
		if err != nil {
			return nil, err
		}
		outs := make([]string, nOut)
		for k := range outs {
			if outs[k], err = r.str(); err != nil {
				return nil, err
			}
		}
		nAttr, err := r.u8()
		if err != nil {
			return nil, err
		}
		attrs := make(map[string]Attr, nAttr)
		for k := 0; k < int(nAttr); k++ {
			an, err := r.str()
			if err != nil {
				return nil, err
			}
			at, err := r.u8()
			if err != nil {
				return nil, err
			}
			var a Attr
			a.Type = at
			switch at {
			case attrInt:
				if a.I, err = r.i64(); err != nil {
					return nil, err
				}
			case attrFloat:
				if a.F, err = r.f32(); err != nil {
					return nil, err
				}
			case attrString:
				if a.S, err = r.str(); err != nil {
					return nil, err
				}
			case attrInts:
				n, err := r.count()
				if err != nil {
					return nil, err
				}
				a.Ints = make([]int64, n)
				for j := range a.Ints {
					if a.Ints[j], err = r.i64(); err != nil {
						return nil, err
					}
				}
			case attrFloats:
				n, err := r.count()
				if err != nil {
					return nil, err
				}
				a.Floats = make([]float32, n)
				for j := range a.Floats {
					if a.Floats[j], err = r.f32(); err != nil {
						return nil, err
					}
				}
			case attrTensor:
				nd, err := r.u8()
				if err != nil {
					return nil, err
				}
				shape := make([]int, nd)
				for d := range shape {
					v, err := r.i64()
					if err != nil {
						return nil, err
					}
					shape[d] = int(v)
				}
				dt, err := r.u8()
				if err != nil {
					return nil, err
				}
				nb, err := r.u64()
				if err != nil {
					return nil, err
				}
				raw, err := r.bytes(int(nb))
				if err != nil {
					return nil, err
				}
				if a.T, err = decodeTensor(shape, DType(dt), raw); err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("不支持的属性类型: %d", at)
			}
			attrs[an] = a
		}
		g.Nodes = append(g.Nodes, &Node{Op: op, Inputs: ins, Outputs: outs, Attrs: attrs})
	}

	if g.Inputs, err = readStrings(r); err != nil {
		return nil, err
	}
	if g.Outputs, err = readStrings(r); err != nil {
		return nil, err
	}
	if g.Charset, err = readStrings(r); err != nil {
		return nil, err
	}
	return g, nil
}

func readStrings(r *reader) ([]string, error) {
	n, err := r.count()
	if err != nil {
		return nil, err
	}
	out := make([]string, n)
	for i := range out {
		if out[i], err = r.str(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func decodeTensor(shape []int, dt DType, raw []byte) (*Tensor, error) {
	want := NumElem(shape)
	switch dt {
	case F32:
		if len(raw) != want*4 {
			return nil, fmt.Errorf("float32 数据长度不符: 期望 %d 实得 %d", want*4, len(raw))
		}
		f := make([]float32, want)
		for i := range f {
			f[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
		return NewF32(shape, f), nil
	case F16:
		if len(raw) != want*2 {
			return nil, fmt.Errorf("float16 数据长度不符: 期望 %d 实得 %d", want*2, len(raw))
		}
		f := make([]float32, want)
		for i := range f {
			f[i] = float16ToFloat32(binary.LittleEndian.Uint16(raw[i*2:]))
		}
		return NewF32(shape, f), nil
	case I64:
		if len(raw) != want*8 {
			return nil, fmt.Errorf("int64 数据长度不符: 期望 %d 实得 %d", want*8, len(raw))
		}
		v := make([]int64, want)
		for i := range v {
			v[i] = int64(binary.LittleEndian.Uint64(raw[i*8:]))
		}
		return NewI64(shape, v), nil
	case U8:
		if len(raw) != want {
			return nil, fmt.Errorf("uint8 数据长度不符: 期望 %d 实得 %d", want, len(raw))
		}
		v := make([]uint8, want)
		copy(v, raw)
		return NewU8(shape, v), nil
	case I8:
		if len(raw) != want {
			return nil, fmt.Errorf("int8 数据长度不符: 期望 %d 实得 %d", want, len(raw))
		}
		v := make([]int8, want)
		for i := range v {
			v[i] = int8(raw[i])
		}
		return &Tensor{Shape: append([]int(nil), shape...), Kind: KindI8, I8: v}, nil
	}
	return nil, fmt.Errorf("未知的数据类型: %d", dt)
}

// float16ToFloat32 把 IEEE 754 半精度位模式展开成单精度。
func float16ToFloat32(h uint16) float32 {
	sign := uint32(h>>15) & 1
	exp := uint32(h>>10) & 0x1f
	frac := uint32(h) & 0x3ff

	switch {
	case exp == 0:
		if frac == 0 {
			// ±0
			return math.Float32frombits(sign << 31)
		}
		// 次正规数：规格化后指数从 -14 起算
		e := uint32(127 - 15 + 1)
		for frac&0x400 == 0 {
			frac <<= 1
			e--
		}
		frac &= 0x3ff
		return math.Float32frombits(sign<<31 | e<<23 | frac<<13)
	case exp == 0x1f:
		// Inf / NaN
		return math.Float32frombits(sign<<31 | 0xff<<23 | frac<<13)
	default:
		return math.Float32frombits(sign<<31 | (exp+127-15)<<23 | frac<<13)
	}
}
