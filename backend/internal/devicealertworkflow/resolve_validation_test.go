package devicealertworkflow

import (
	"context"
	"errors"
	"testing"
)

func TestResolveRejectsUnknownKindWithoutOpeningTransaction(t *testing.T) {
	_, err := Resolve(context.Background(), nil, 11, 1, "invalid", nil, nil)
	if !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("unexpected invalid alert kind error: %v", err)
	}
}
