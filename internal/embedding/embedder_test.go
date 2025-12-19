package embedding

import (
	"testing"
)

func TestStatus(t *testing.T) {
	s := NewStatus()
	docID := "test-doc"

	if s.IsReady(docID) {
		t.Errorf("expected doc to not be ready initially")
	}

	s.SetReady(docID)
	if !s.IsReady(docID) {
		t.Errorf("expected doc to be ready after SetReady")
	}

	s.Clear(docID)
	if s.IsReady(docID) {
		t.Errorf("expected doc to not be ready after Clear")
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Host != "http://localhost:11434" {
		t.Errorf("unexpected default host: %s", cfg.Host)
	}
	if cfg.Model != "nomic-embed-text" {
		t.Errorf("unexpected default model: %s", cfg.Model)
	}
}
