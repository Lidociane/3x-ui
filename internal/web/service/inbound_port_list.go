package service

import (
	"fmt"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// normalizeInboundPortList folds an operator's listen spec into Port+PortList:
// Port stays the first port (share links, the TUIC relay and bind probes read
// it) and PortList keeps the full range for the config generator.
func normalizeInboundPortList(inbound *model.Inbound) error {
	if inbound.PortList == "" {
		return nil
	}
	port, list, err := xray.ParsePortList(inbound.PortList)
	if err != nil {
		return fmt.Errorf("ports: %w", err)
	}
	inbound.Port = port
	inbound.PortList = list
	return nil
}
