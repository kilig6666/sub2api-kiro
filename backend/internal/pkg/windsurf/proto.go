package windsurf

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// WireType 表示 protobuf wire type
type WireType uint8

const (
	WireTypeVarint  WireType = 0
	WireTypeFixed64 WireType = 1
	WireTypeLen     WireType = 2
	WireTypeFixed32 WireType = 5
)

// Field 表示一个解析后的 protobuf 字段
type Field struct {
	Number   uint32
	WireType WireType
	Varint   uint64
	Bytes    []byte
}

// EncodeVarint 编码一个 varint
func EncodeVarint(value uint64) []byte {
	var out []byte
	for {
		b := byte(value & 0x7f)
		value >>= 7
		if value != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if value == 0 {
			break
		}
	}
	return out
}

func tag(field uint32, wt WireType) []byte {
	return EncodeVarint((uint64(field) << 3) | uint64(wt))
}

// WriteVarintField 写入一个 varint 字段
func WriteVarintField(field uint32, value uint64) []byte {
	out := tag(field, WireTypeVarint)
	out = append(out, EncodeVarint(value)...)
	return out
}

// WriteStringField 写入一个 string 字段（length-delimited）
func WriteStringField(field uint32, value string) []byte {
	b := []byte(value)
	out := tag(field, WireTypeLen)
	out = append(out, EncodeVarint(uint64(len(b)))...)
	out = append(out, b...)
	return out
}

// WriteMessageField 写入一个嵌套 message 字段（length-delimited）
func WriteMessageField(field uint32, value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	out := tag(field, WireTypeLen)
	out = append(out, EncodeVarint(uint64(len(value)))...)
	out = append(out, value...)
	return out
}

// WriteBoolField 写入一个 bool 字段（value=false 时不写入）
func WriteBoolField(field uint32, value bool) []byte {
	if !value {
		return nil
	}
	return WriteVarintField(field, 1)
}

// WriteLenFieldAllowEmpty 写入一个 length-delimited 字段（允许空值）
func WriteLenFieldAllowEmpty(field uint32, value []byte) []byte {
	out := tag(field, WireTypeLen)
	out = append(out, EncodeVarint(uint64(len(value)))...)
	out = append(out, value...)
	return out
}

// ParseFields 解析 protobuf 二进制数据为字段列表
func ParseFields(buf []byte) ([]Field, error) {
	var fields []Field
	pos := 0
	for pos < len(buf) {
		tagVal, tagLen, err := decodeVarint(buf, pos)
		if err != nil {
			return nil, fmt.Errorf("parse tag at offset %d: %w", pos, err)
		}
		pos += tagLen
		number := uint32(tagVal >> 3)
		wt := WireType(tagVal & 0x07)

		var f Field
		f.Number = number
		f.WireType = wt

		switch wt {
		case WireTypeVarint:
			val, valLen, err := decodeVarint(buf, pos)
			if err != nil {
				return nil, fmt.Errorf("parse varint field %d: %w", number, err)
			}
			pos += valLen
			f.Varint = val
		case WireTypeLen:
			length, lenLen, err := decodeVarint(buf, pos)
			if err != nil {
				return nil, fmt.Errorf("parse len field %d length: %w", number, err)
			}
			pos += lenLen
			end := pos + int(length)
			if end > len(buf) {
				return nil, fmt.Errorf("truncated len-delimited field %d at offset %d", number, pos)
			}
			f.Bytes = buf[pos:end]
			pos = end
		case WireTypeFixed64:
			if pos+8 > len(buf) {
				return nil, fmt.Errorf("truncated fixed64 field %d", number)
			}
			f.Bytes = buf[pos : pos+8]
			pos += 8
		case WireTypeFixed32:
			if pos+4 > len(buf) {
				return nil, fmt.Errorf("truncated fixed32 field %d", number)
			}
			f.Bytes = buf[pos : pos+4]
			pos += 4
		default:
			return nil, fmt.Errorf("unknown wire type %d at offset %d", wt, pos)
		}
		fields = append(fields, f)
	}
	return fields, nil
}

// GetField 获取第一个匹配的字段
func GetField(fields []Field, number uint32) *Field {
	for i := range fields {
		if fields[i].Number == number {
			return &fields[i]
		}
	}
	return nil
}

// GetAllFields 获取所有匹配的字段
func GetAllFields(fields []Field, number uint32) []Field {
	var result []Field
	for _, f := range fields {
		if f.Number == number {
			result = append(result, f)
		}
	}
	return result
}

// GetVarint 获取 varint 字段值
func GetVarint(fields []Field, number uint32) (uint64, bool) {
	f := GetField(fields, number)
	if f == nil || f.WireType != WireTypeVarint {
		return 0, false
	}
	return f.Varint, true
}

// GetString 获取 string 字段值
func GetString(fields []Field, number uint32) (string, bool) {
	f := GetField(fields, number)
	if f == nil || f.WireType != WireTypeLen {
		return "", false
	}
	return string(f.Bytes), true
}

// GetBytes 获取 bytes 字段值
func GetBytes(fields []Field, number uint32) ([]byte, bool) {
	f := GetField(fields, number)
	if f == nil || f.WireType != WireTypeLen {
		return nil, false
	}
	return f.Bytes, true
}

// GRPCFrame 将 payload 包装为 gRPC frame（5 字节头 + payload）
func GRPCFrame(payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	frame[0] = 0 // no compression
	binary.BigEndian.PutUint32(frame[1:5], uint32(len(payload)))
	copy(frame[5:], payload)
	return frame
}

// ExtractGRPCFrames 从 gRPC 响应中提取所有 frame payload
func ExtractGRPCFrames(buf []byte) [][]byte {
	var frames [][]byte
	pos := 0
	for pos+5 <= len(buf) {
		length := int(binary.BigEndian.Uint32(buf[pos+1 : pos+5]))
		pos += 5
		if pos+length > len(buf) {
			break
		}
		frames = append(frames, buf[pos:pos+length])
		pos += length
	}
	return frames
}

func decodeVarint(buf []byte, offset int) (uint64, int, error) {
	var value uint64
	var shift uint32
	pos := offset
	for pos < len(buf) {
		b := buf[pos]
		pos++
		value |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return value, pos - offset, nil
		}
		shift += 7
		if shift >= 64 {
			return 0, 0, errors.New("varint overflow")
		}
	}
	return 0, 0, errors.New("truncated varint")
}
