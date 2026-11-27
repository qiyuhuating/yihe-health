package main

import (
	"testing"
	"yihe-care/internal/config"
)

func TestDesktopBootstrapToken(t *testing.T) {
	t.Setenv("DESKTOP_TOKEN", "")
	t.Setenv("YIHE_DESKTOP_TOKEN", "review-desktop-bootstrap-token")
	cfg := config.Load()
	applyDesktopEnv(cfg.Addr)
	if cfg.DesktopToken != "review-desktop-bootstrap-token" {
		t.Fatalf("desktop handler configuration lost bootstrap token: %q", cfg.DesktopToken)
	}
}
