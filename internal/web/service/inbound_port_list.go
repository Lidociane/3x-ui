package service

import (
	"fmt"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

// normalizeInboundPortList validates the optional range and leaves Port alone:
// the panel exposes 端口 and 端口范围 as independent settings, and the inbound
// binds their union (Port is what share links and the TUIC relay read).
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
	// 面板只有一个"端口"输入框（"6409,20000-20010"）：首端口落到 Port，
	// 完整 spec 落到 PortList，xray 监听两者并集。
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
	if inbound.PortList == "" {
		return xray.ExpandPortList("", inbound.Port)
	}
	seen := make(map[int]struct{}, 8)
	if inbound.Port > 0 {
		seen[inbound.Port] = struct{}{}
	}
	for _, port := range xray.ExpandPortList(inbound.PortList, 0) {
		seen[port] = struct{}{}
	}
	ports := make([]int, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}
	return ports
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
