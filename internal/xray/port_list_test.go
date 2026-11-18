package xray

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/mhsanaei/3x-ui/v3/internal/util/json_util"
)

func TestParsePortList(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		port    int
		list    string
		wantErr bool
	}{
		{name: "single", spec: "443", port: 443},
		{name: "list", spec: "443,8443", port: 443, list: "443,8443"},
		{name: "range with spaces", spec: " 20000-20010 ", port: 20000, list: "20000-20010"},
		{name: "mixed", spec: "6408,20000-20002", port: 6408, list: "6408,20000-20002"},
		{name: "empty entry", spec: "443,", wantErr: true},
		{name: "not a port", spec: "abc", wantErr: true},
		{name: "above 65535", spec: "70000", wantErr: true},
		{name: "zero", spec: "0", wantErr: true},
		{name: "descending range", spec: "20010-20000", wantErr: true},
		{name: "too many ports", spec: "1-65535", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			port, list, err := ParsePortList(tt.spec)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParsePortList(%q) accepted an invalid spec (port=%d list=%q)", tt.spec, port, list)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePortList(%q): %v", tt.spec, err)
			}
			if port != tt.port || list != tt.list {
				t.Fatalf("ParsePortList(%q) = (%d, %q), want (%d, %q)", tt.spec, port, list, tt.port, tt.list)
			}
		})
	}
}

func TestExpandPortList(t *testing.T) {
	if got := ExpandPortList("", 443); !reflect.DeepEqual(got, []int{443}) {
		t.Fatalf("single port expansion = %v, want [443]", got)
	}
	if got := ExpandPortList("20000-20002,443", 20000); !reflect.DeepEqual(got, []int{20000, 20001, 20002, 443}) {
		t.Fatalf("range expansion = %v", got)
	}
	if got := ExpandPortList("", 0); got != nil {
		t.Fatalf("unset expansion = %v, want nil", got)
	}
}

// xray-core's PortList accepts a number or a string and nothing else, so the
// shape of this field decides whether the core starts at all.
func TestInboundConfigPortMarshal(t *testing.T) {
	single, err := json.Marshal(InboundConfig{
		Listen:   json_util.RawMessage(`"0.0.0.0"`),
		Port:     443,
		Protocol: "vless",
		Settings: json_util.RawMessage(`{}`),
		Tag:      "in-443",
	})
	if err != nil {
		t.Fatalf("marshal single: %v", err)
	}
	if want := `"port":443`; !strings.Contains(string(single), want) {
		t.Fatalf("single port config = %s, want %s", single, want)
	}

	ranged, err := json.Marshal(InboundConfig{
		Listen:   json_util.RawMessage(`"0.0.0.0"`),
		Port:     20000,
		PortList: "20000-20010",
		Protocol: "vless",
		Settings: json_util.RawMessage(`{}`),
		Tag:      "in-range",
	})
	if err != nil {
		t.Fatalf("marshal range: %v", err)
	}
	if want := `"port":"20000-20010"`; !strings.Contains(string(ranged), want) {
		t.Fatalf("ranged config = %s, want %s", ranged, want)
	}
}

func TestInboundConfigPortUnmarshal(t *testing.T) {
	var single InboundConfig
	if err := json.Unmarshal([]byte(`{"port":443,"protocol":"vless","tag":"a"}`), &single); err != nil {
		t.Fatalf("unmarshal single: %v", err)
	}
	if single.Port != 443 || single.PortList != "" {
		t.Fatalf("single unmarshal = (%d, %q), want (443, \"\")", single.Port, single.PortList)
	}

	var ranged InboundConfig
	if err := json.Unmarshal([]byte(`{"port":"20000-20010","protocol":"vless","tag":"b"}`), &ranged); err != nil {
		t.Fatalf("unmarshal range: %v", err)
	}
	if ranged.Port != 20000 || ranged.PortList != "20000-20010" {
		t.Fatalf("range unmarshal = (%d, %q), want (20000, \"20000-20010\")", ranged.Port, ranged.PortList)
	}
	if got := ranged.Ports(); !reflect.DeepEqual(got, []int{20000, 20001, 20002, 20003, 20004, 20005, 20006, 20007, 20008, 20009, 20010}) {
		t.Fatalf("Ports() = %v", got)
	}
}

// A change that only widens the port range must read as a changed inbound, or
// the hot diff would leave the old listener in place.
func TestInboundConfigEqualsSeesPortList(t *testing.T) {
	base := InboundConfig{Port: 20000, PortList: "20000-20005", Protocol: "vless"}
	wider := base
	wider.PortList = "20000-20010"
	if base.Equals(&wider) {
		t.Fatal("Equals reported equal for two different port lists")
	}
	same := base
	if !base.Equals(&same) {
		t.Fatal("Equals reported different for identical configs")
	}
}
