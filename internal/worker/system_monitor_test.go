package worker

import (
	"testing"

	"github.com/WithAutonomi/indelible/internal/config"
	"github.com/WithAutonomi/indelible/internal/dbtest"
)

// TestSetAntdUnavailable_PersistsOnTransition verifies V2-486: the monitor
// persists the antd_unavailable flag only on a real state change, and writes
// the "true"/"false" values CreateUpload reads to fast-fail uploads.
func TestSetAntdUnavailable_PersistsOnTransition(t *testing.T) {
	db := dbtest.OpenDB(t)
	cfg := &config.Config{
		WalletEncryptionKey: "1111111111111111111111111111111111111111111111111111111111111111",
	}
	m := NewSystemMonitor(db, cfg)

	// Not persisted until a real transition.
	if v, _ := m.settingsWriter.Get(AntdUnavailableSetting); v != "" {
		t.Errorf("expected unset flag initially, got %q", v)
	}

	m.setAntdUnavailable(true)
	if v, _ := m.settingsWriter.Get(AntdUnavailableSetting); v != "true" {
		t.Errorf("after set true: flag = %q, want true", v)
	}

	// Idempotent: setting the same value again keeps it true.
	m.setAntdUnavailable(true)
	if v, _ := m.settingsWriter.Get(AntdUnavailableSetting); v != "true" {
		t.Errorf("idempotent set: flag = %q, want true", v)
	}

	m.setAntdUnavailable(false)
	if v, _ := m.settingsWriter.Get(AntdUnavailableSetting); v != "false" {
		t.Errorf("after set false: flag = %q, want false", v)
	}
}
