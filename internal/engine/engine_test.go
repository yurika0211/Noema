package engine

import (
	"errors"
	"testing"
)

func TestUnsupportedError(t *testing.T) {
	err := Unsupported("host_context")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("want ErrUnsupported, got %v", err)
	}
	if err.Error() == "unsupported" {
		t.Fatal("feature name should be in the error")
	}
}
