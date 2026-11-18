package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// The generated config must carry the operator's range; a single-port inbound
// must keep emitting a bare number so existing configs are untouched.
func TestInboundGenXrayInboundConfigPortList(t *testing.T) {
	t.Run("range", func(t *testing.T) {
		ib := Inbound{
			Port:           20000,
			PortList:       "20000-20010",
			Protocol:       VLESS,
			Settings:       "{}",
			StreamSettings: "{}",
			Tag:            "in-range",
		}
		cfg := ib.GenXrayInboundConfig()
		if len(cfg.Ports()) != 11 {
			t.Fatalf("generated config expands to %d ports, want 11", len(cfg.Ports()))
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("marshal generated config: %v", err)
		}
		if !strings.Contains(string(raw), `"port":"20000-20010"`) {
			t.Fatalf("generated config = %s, want a string port range", raw)
		}
	})
	t.Run("single", func(t *testing.T) {
		ib := Inbound{Port: 443, Protocol: VLESS, Settings: "{}", StreamSettings: "{}", Tag: "in-443"}
		raw, err := json.Marshal(ib.GenXrayInboundConfig())
		if err != nil {
			t.Fatalf("marshal generated config: %v", err)
		}
		if !strings.Contains(string(raw), `"port":443`) || strings.Contains(string(raw), `"port":"`) {
			t.Fatalf("generated config = %s, want a bare numeric port", raw)
		}
	})
}
