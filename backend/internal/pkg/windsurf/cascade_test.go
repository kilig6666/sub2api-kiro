//go:build unit

package windsurf

import "testing"

func TestBuildMetadata(t *testing.T) {
	meta := BuildMetadata("test-key", "session-123")
	fields, err := ParseFields(meta)
	if err != nil {
		t.Fatalf("ParseFields: %v", err)
	}
	if s, ok := GetString(fields, 1); !ok || s != "windsurf" {
		t.Errorf("field 1 = %q, want windsurf", s)
	}
	if s, ok := GetString(fields, 3); !ok || s != "test-key" {
		t.Errorf("field 3 = %q, want test-key", s)
	}
	if s, ok := GetString(fields, 10); !ok || s != "session-123" {
		t.Errorf("field 10 = %q, want session-123", s)
	}
}

func TestBuildStartCascadeRequest(t *testing.T) {
	req := BuildStartCascadeRequest("key", "sess")
	fields, err := ParseFields(req)
	if err != nil {
		t.Fatalf("ParseFields: %v", err)
	}
	// field 1 = metadata message
	if GetField(fields, 1) == nil {
		t.Error("missing metadata field 1")
	}
	// field 4 = 1, field 5 = 1
	if v, ok := GetVarint(fields, 4); !ok || v != 1 {
		t.Errorf("field 4 = %d, want 1", v)
	}
	if v, ok := GetVarint(fields, 5); !ok || v != 1 {
		t.Errorf("field 5 = %d, want 1", v)
	}
}

func TestParseStartCascadeResponse(t *testing.T) {
	resp := WriteStringField(1, "cascade-abc-123")
	id, err := ParseStartCascadeResponse(resp)
	if err != nil {
		t.Fatalf("ParseStartCascadeResponse: %v", err)
	}
	if id != "cascade-abc-123" {
		t.Errorf("cascade_id = %q, want cascade-abc-123", id)
	}
}

func TestParseTrajectoryStatus(t *testing.T) {
	var buf []byte
	buf = append(buf, WriteStringField(1, "cascade-id")...)
	buf = append(buf, WriteVarintField(2, 1)...)
	status := ParseTrajectoryStatus(buf)
	if status != 1 {
		t.Errorf("status = %d, want 1", status)
	}
}

func TestBuildSendCascadeMessageRequest(t *testing.T) {
	req, err := BuildSendCascadeMessageRequest("key", "cascade-1", "hello", 281, "MODEL_CLAUDE_4_SONNET", "sess", nil)
	if err != nil {
		t.Fatalf("BuildSendCascadeMessageRequest: %v", err)
	}
	if len(req) == 0 {
		t.Fatal("request is empty")
	}
	fields, err := ParseFields(req)
	if err != nil {
		t.Fatalf("ParseFields: %v", err)
	}
	// field 1 = cascade_id
	if s, ok := GetString(fields, 1); !ok || s != "cascade-1" {
		t.Errorf("cascade_id = %q, want cascade-1", s)
	}
}

func TestBuildSendCascadeMessageRequestNoModel(t *testing.T) {
	_, err := BuildSendCascadeMessageRequest("key", "cascade-1", "hello", 0, "", "sess", nil)
	if err == nil {
		t.Fatal("expected error for missing model, got nil")
	}
}

func TestBuildNativeCascadeToolConfig(t *testing.T) {
	config := BuildNativeCascadeToolConfig(nil)
	if len(config) == 0 {
		t.Fatal("default tool config should not be empty")
	}
}

func TestParseTrajectoryStepsEmpty(t *testing.T) {
	steps, err := ParseTrajectorySteps([]byte{})
	if err != nil {
		t.Fatalf("ParseTrajectorySteps empty: %v", err)
	}
	if len(steps) != 0 {
		t.Errorf("expected 0 steps, got %d", len(steps))
	}
}
