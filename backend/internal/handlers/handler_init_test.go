package handlers

import (
	"testing"

	"yihe-care/internal/config"
	dbstore "yihe-care/internal/db"
	"yihe-care/internal/risk"
)

func TestInvalidEncryptionKeyCannotBuildDatabaseHandler(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("invalid key silently produced a handler without encryption")
		}
	}()
	New(&dbstore.Database{}, nil, config.Config{DataKey: "invalid-base64-key"})
}

func TestInjectedRiskManagerIsInstanceOwned(t *testing.T) {
	one, two := risk.New(), risk.New()
	h1 := NewWithRisk(nil, nil, config.Config{}, one)
	h2 := NewWithRisk(nil, nil, config.Config{}, two)
	if h1.Risk != one || h2.Risk != two || h1.Risk == h2.Risk {
		t.Fatal("injected risk managers were shared across instances")
	}
}

func TestDefaultHandlersDoNotShareRiskState(t *testing.T) {
	first := New(nil, nil, config.Config{})
	second := New(nil, nil, config.Config{})
	if first.Risk == nil || second.Risk == nil || first.Risk == second.Risk {
		t.Fatal("default constructors shared mutable risk state")
	}
}
