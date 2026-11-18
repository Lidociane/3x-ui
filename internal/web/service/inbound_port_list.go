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
	// mtproto/tuic/amneziawg listen through a sidecar that binds one port from
	// Port, so a range there would render in the panel and never bind.
	if !protocolSupportsPortList(inbound.Protocol) {
		return fmt.Errorf("protocol %s listens on a single port", inbound.Protocol)
	}
	port, list, err := xray.ParsePortList(inbound.PortList)
	if err != nil {
		return fmt.Errorf("ports: %w", err)
	}
	inbound.Port = port
	inbound.PortList = list
	return nil
}

// protocolSupportsPortList reports whether the range reaches the core: sidecar
// protocols derive their own single listen port from Inbound.Port.
func protocolSupportsPortList(p model.Protocol) bool {
	switch p {
	case model.MTProto, model.TUIC, model.AmneziaWG:
		return false
	}
	return true
}

// inboundPorts expands a row's listen spec; Port stands in when no list is set.
func inboundPorts(inbound *model.Inbound) []int {
	return xray.ExpandPortList(inbound.PortList, inbound.Port)
}

// inboundHasPort reports whether the row binds port (single or inside a range).
func inboundHasPort(inbound *model.Inbound, port int) bool {
	for _, p := range inboundPorts(inbound) {
		if p == port {
			return true
		}
	}
	return false
}

// inboundsPortOverlap reports whether two rows bind at least one shared port.
func inboundsPortOverlap(a, b *model.Inbound) bool {
	for _, port := range inboundPorts(a) {
		if inboundHasPort(b, port) {
			return true
		}
	}
	return false
}
