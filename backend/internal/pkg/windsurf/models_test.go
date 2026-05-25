//go:build unit

package windsurf

import "testing"

func TestResolveWindsurfModelCanonical(t *testing.T) {
	m, ok := ResolveWindsurfModel("claude-sonnet-4.6")
	if !ok {
		t.Fatal("expected to resolve claude-sonnet-4.6")
	}
	if m.CanonicalName != "claude-sonnet-4.6" {
		t.Errorf("CanonicalName = %q, want claude-sonnet-4.6", m.CanonicalName)
	}
	if m.ModelUID != "claude-sonnet-4-6" {
		t.Errorf("ModelUID = %q, want claude-sonnet-4-6", m.ModelUID)
	}
}

func TestResolveWindsurfModelAlias(t *testing.T) {
	m, ok := ResolveWindsurfModel("claude-opus-4-7")
	if !ok {
		t.Fatal("expected to resolve claude-opus-4-7 alias")
	}
	if m.CanonicalName != "claude-opus-4-7-medium" {
		t.Errorf("CanonicalName = %q, want claude-opus-4-7-medium", m.CanonicalName)
	}
}

func TestResolveWindsurfModelByUID(t *testing.T) {
	m, ok := ResolveWindsurfModel("MODEL_GPT_5_2_LOW")
	if !ok {
		t.Fatal("expected to resolve by model UID")
	}
	if m.CanonicalName != "gpt-5.2-low" {
		t.Errorf("CanonicalName = %q, want gpt-5.2-low", m.CanonicalName)
	}
	if m.EnumValue != 400 {
		t.Errorf("EnumValue = %d, want 400", m.EnumValue)
	}
}

func TestResolveWindsurfModelCaseInsensitive(t *testing.T) {
	m, ok := ResolveWindsurfModel("GPT-5.5-LOW")
	if !ok {
		t.Fatal("expected case-insensitive resolution")
	}
	if m.CanonicalName != "gpt-5.5-low" {
		t.Errorf("CanonicalName = %q, want gpt-5.5-low", m.CanonicalName)
	}
}

func TestResolveWindsurfModelUnknown(t *testing.T) {
	_, ok := ResolveWindsurfModel("nonexistent-model")
	if ok {
		t.Error("expected unknown model to not resolve")
	}
}

func TestResolveWindsurfModelEmpty(t *testing.T) {
	_, ok := ResolveWindsurfModel("")
	if ok {
		t.Error("expected empty string to not resolve")
	}
}

func TestWindsurfModelsNotEmpty(t *testing.T) {
	all := WindsurfModels()
	if len(all) < 50 {
		t.Errorf("WindsurfModels() returned %d models, expected at least 50", len(all))
	}
}
