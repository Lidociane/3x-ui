package xray

import (
	"bytes"
	"encoding/json"
	"strconv"

	"github.com/mhsanaei/3x-ui/v3/internal/util/json_util"
)

// InboundConfig represents an Xray inbound configuration.
// It defines how Xray accepts incoming connections including protocol, port, and settings.
type InboundConfig struct {
	// TuicRelay owns client revocation outside Xray; its user edits must not restart the core.
	TuicRelay     bool                 `json:"-"`
	TuicInboundID int                  `json:"-"`
	Listen        json_util.RawMessage `json:"listen"` // listen cannot be an empty string
	Port          int                  `json:"-"`
	// PortList is Xray's multi-port form ("443,8443,20000-20100"). When set it
	// replaces Port, so one inbound answers on the whole range.
	PortList       string               `json:"-"`
	Protocol       string               `json:"protocol"`
	Settings       json_util.RawMessage `json:"settings"`
	StreamSettings json_util.RawMessage `json:"streamSettings,omitempty"`
	Tag            string               `json:"tag"`
	Sniffing       json_util.RawMessage `json:"sniffing,omitempty"`
}

// MarshalJSON emits "port" as a number, or as Xray's port-list string when the
// inbound listens on a range: xray-core accepts exactly those two shapes.
func (c InboundConfig) MarshalJSON() ([]byte, error) {
	port, err := c.portJSON()
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Listen         json_util.RawMessage `json:"listen"`
		Port           json.RawMessage      `json:"port"`
		Protocol       string               `json:"protocol"`
		Settings       json_util.RawMessage `json:"settings"`
		StreamSettings json_util.RawMessage `json:"streamSettings,omitempty"`
		Tag            string               `json:"tag"`
		Sniffing       json_util.RawMessage `json:"sniffing,omitempty"`
	}{c.Listen, port, c.Protocol, c.Settings, c.StreamSettings, c.Tag, c.Sniffing})
}

// UnmarshalJSON reads either shape back, so a config produced by this panel and
// one written by hand both load.
func (c *InboundConfig) UnmarshalJSON(data []byte) error {
	var raw struct {
		Listen         json_util.RawMessage `json:"listen"`
		Port           json.RawMessage      `json:"port"`
		Protocol       string               `json:"protocol"`
		Settings       json_util.RawMessage `json:"settings"`
		StreamSettings json_util.RawMessage `json:"streamSettings,omitempty"`
		Tag            string               `json:"tag"`
		Sniffing       json_util.RawMessage `json:"sniffing,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.Listen = raw.Listen
	c.Protocol = raw.Protocol
	c.Settings = raw.Settings
	c.StreamSettings = raw.StreamSettings
	c.Tag = raw.Tag
	c.Sniffing = raw.Sniffing
	c.Port, c.PortList = 0, ""
	if len(raw.Port) == 0 || string(raw.Port) == "null" {
		return nil
	}
	if raw.Port[0] == '"' {
		var spec string
		if err := json.Unmarshal(raw.Port, &spec); err != nil {
			return err
		}
		c.PortList = spec
		if port, _, err := ParsePortList(spec); err == nil {
			c.Port = port
		}
		return nil
	}
	return json.Unmarshal(raw.Port, &c.Port)
}

// Ports expands this inbound's listen spec into the ports it binds.
func (c *InboundConfig) Ports() []int {
	return ExpandPortList(c.PortList, c.Port)
}

func (c InboundConfig) portJSON() (json.RawMessage, error) {
	if c.PortList != "" {
		return json.Marshal(c.PortList)
	}
	return json.RawMessage(strconv.Itoa(c.Port)), nil
}

// Equals compares two InboundConfig instances for deep equality.
func (c *InboundConfig) Equals(other *InboundConfig) bool {
	if !bytes.Equal(c.Listen, other.Listen) {
		return false
	}
	if c.Port != other.Port || c.PortList != other.PortList {
		return false
	}
	if c.Protocol != other.Protocol {
		return false
	}
	if !bytes.Equal(c.Settings, other.Settings) {
		return false
	}
	if !bytes.Equal(c.StreamSettings, other.StreamSettings) {
		return false
	}
	if c.Tag != other.Tag {
		return false
	}
	if !bytes.Equal(c.Sniffing, other.Sniffing) {
		return false
	}
	return true
}
