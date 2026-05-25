//go:build unit

package windsurf

import (
	"testing"
)

func TestEncodeVarint(t *testing.T) {
	tests := []struct {
		input    uint64
		expected []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{300, []byte{0xac, 0x02}},
		{16384, []byte{0x80, 0x80, 0x01}},
	}
	for _, tt := range tests {
		got := EncodeVarint(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("EncodeVarint(%d) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("EncodeVarint(%d) = %v, want %v", tt.input, got, tt.expected)
				break
			}
		}
	}
}

func TestWriteStringField(t *testing.T) {
	got := WriteStringField(3, "abc")
	expected := []byte{0x1a, 0x03, 'a', 'b', 'c'}
	if len(got) != len(expected) {
		t.Fatalf("WriteStringField(3, \"abc\") len = %d, want %d", len(got), len(expected))
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Fatalf("WriteStringField(3, \"abc\") = %v, want %v", got, expected)
		}
	}
}

func TestWriteBoolField(t *testing.T) {
	if got := WriteBoolField(2, false); got != nil {
		t.Errorf("WriteBoolField(2, false) = %v, want nil", got)
	}
	got := WriteBoolField(2, true)
	expected := []byte{0x10, 0x01}
	if len(got) != len(expected) {
		t.Fatalf("WriteBoolField(2, true) len = %d, want %d", len(got), len(expected))
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Fatalf("WriteBoolField(2, true) = %v, want %v", got, expected)
		}
	}
}

func TestParseFieldsRoundTrip(t *testing.T) {
	var buf []byte
	buf = append(buf, WriteStringField(1, "alpha")...)
	buf = append(buf, WriteStringField(1, "beta")...)
	buf = append(buf, WriteVarintField(2, 42)...)

	fields, err := ParseFields(buf)
	if err != nil {
		t.Fatalf("ParseFields: %v", err)
	}

	all := GetAllFields(fields, 1)
	if len(all) != 2 {
		t.Fatalf("GetAllFields(1) count = %d, want 2", len(all))
	}

	s, ok := GetString(fields, 1)
	if !ok || s != "alpha" {
		t.Errorf("GetString(1) = %q, want \"alpha\"", s)
	}

	v, ok := GetVarint(fields, 2)
	if !ok || v != 42 {
		t.Errorf("GetVarint(2) = %d, want 42", v)
	}
}

func TestParseFieldsRejectsTruncated(t *testing.T) {
	_, err := ParseFields([]byte{0x0a, 0x05, 'a'})
	if err == nil {
		t.Fatal("expected error for truncated field, got nil")
	}
}

func TestGRPCFrame(t *testing.T) {
	payload := []byte{0x01, 0x02, 0x03}
	frame := GRPCFrame(payload)
	if frame[0] != 0 {
		t.Errorf("frame[0] = %d, want 0 (no compression)", frame[0])
	}
	if len(frame) != 8 {
		t.Fatalf("frame len = %d, want 8", len(frame))
	}

	frames := ExtractGRPCFrames(frame)
	if len(frames) != 1 {
		t.Fatalf("ExtractGRPCFrames count = %d, want 1", len(frames))
	}
	if len(frames[0]) != 3 {
		t.Fatalf("extracted payload len = %d, want 3", len(frames[0]))
	}
}

func TestWriteMessageField(t *testing.T) {
	if got := WriteMessageField(1, nil); got != nil {
		t.Errorf("WriteMessageField with nil should return nil, got %v", got)
	}
	if got := WriteMessageField(1, []byte{}); got != nil {
		t.Errorf("WriteMessageField with empty should return nil, got %v", got)
	}

	inner := WriteStringField(1, "hi")
	got := WriteMessageField(2, inner)
	fields, err := ParseFields(got)
	if err != nil {
		t.Fatalf("ParseFields: %v", err)
	}
	f := GetField(fields, 2)
	if f == nil {
		t.Fatal("field 2 not found")
	}
	innerFields, err := ParseFields(f.Bytes)
	if err != nil {
		t.Fatalf("ParseFields inner: %v", err)
	}
	s, ok := GetString(innerFields, 1)
	if !ok || s != "hi" {
		t.Errorf("inner string = %q, want \"hi\"", s)
	}
}
