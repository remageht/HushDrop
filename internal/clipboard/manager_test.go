package clipboard

import (
	"strings"
	"testing"
)

func TestClipboardSetAndGet(t *testing.T) {
	mgr := NewManager()

	item, exists := mgr.Get()
	if exists {
		t.Fatalf("expected empty clipboard, got %+v", item)
	}

	err := mgr.Set("Hello HushDrop", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	item, exists = mgr.Get()
	if !exists {
		t.Fatalf("expected item to exist")
	}
	if item.Text != "Hello HushDrop" {
		t.Fatalf("expected 'Hello HushDrop', got '%s'", item.Text)
	}
	if item.BurnAfterRead {
		t.Fatalf("expected BurnAfterRead to be false")
	}

	// Should still exist on second call
	_, exists = mgr.Get()
	if !exists {
		t.Fatalf("expected persistent read for regular clipboard")
	}
}

func TestClipboardBurnAfterRead(t *testing.T) {
	mgr := NewManager()

	err := mgr.Set("Secret 2FA Code: 981240", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	item, exists := mgr.Get()
	if !exists {
		t.Fatalf("expected item to exist on first read")
	}
	if item.Text != "Secret 2FA Code: 981240" {
		t.Fatalf("got '%s'", item.Text)
	}

	// Second read must be empty (burned)
	_, exists = mgr.Get()
	if exists {
		t.Fatalf("expected clipboard to be burned after first read")
	}
}

func TestClipboardSizeLimit(t *testing.T) {
	mgr := NewManager()

	largeText := strings.Repeat("A", MaxClipboardSize+1)
	err := mgr.Set(largeText, false)
	if err == nil {
		t.Fatalf("expected error for text exceeding limit")
	}
}

func TestClipboardClear(t *testing.T) {
	mgr := NewManager()

	_ = mgr.Set("Some text", false)
	mgr.Clear()

	_, exists := mgr.Get()
	if exists {
		t.Fatalf("expected clipboard to be cleared")
	}
}
