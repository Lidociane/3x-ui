package xray

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// MaxPortListPorts bounds a spec's expansion so a typo like "1-65535" cannot
// turn the bind probe and one listener into a resource blow-up.
const MaxPortListPorts = 256

// ParsePortList validates "443", "443,8443" or "20000-20100" and returns the
// smallest port plus the canonical spec ("" when only one port remains).
// Overlapping or repeated entries are merged, so Port is always the lowest
// port the inbound binds and the share link never names a port the listener
// does not own.
func ParsePortList(spec string) (int, string, error) {
	spec = strings.ReplaceAll(strings.TrimSpace(spec), " ", "")
	if spec == "" {
		return 0, "", fmt.Errorf("port is required")
	}
	seen := make(map[int]struct{}, 8)
	for _, token := range strings.Split(spec, ",") {
		lo, hi, ok := splitPortToken(token)
		if !ok {
			return 0, "", fmt.Errorf("invalid port %q: use 443, 443,8443 or 20000-20100", token)
		}
		if lo < 1 || hi > 65535 {
			return 0, "", fmt.Errorf("port %q is outside 1-65535", token)
		}
		if hi < lo {
			return 0, "", fmt.Errorf("invalid port range %q: end is below start", token)
		}
		for port := lo; port <= hi; port++ {
			seen[port] = struct{}{}
			if len(seen) > MaxPortListPorts {
				return 0, "", fmt.Errorf("at most %d ports per inbound", MaxPortListPorts)
			}
		}
	}
	ports := make([]int, 0, len(seen))
	for port := range seen {
		ports = append(ports, port)
	}
	sort.Ints(ports)
	if len(ports) == 1 {
		return ports[0], "", nil
	}
	return ports[0], compressPorts(ports), nil
}

// compressPorts renders a sorted, deduplicated port set back into "a,b-c" form.
func compressPorts(ports []int) string {
	var b strings.Builder
	for i := 0; i < len(ports); {
		j := i
		for j+1 < len(ports) && ports[j+1] == ports[j]+1 {
			j++
		}
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		if i == j {
			b.WriteString(strconv.Itoa(ports[i]))
		} else {
			b.WriteString(strconv.Itoa(ports[i]))
			b.WriteByte('-')
			b.WriteString(strconv.Itoa(ports[j]))
		}
		i = j + 1
	}
	return b.String()
}

// ExpandPortList returns the concrete ports an inbound binds; the single Port
// stands in when no list was configured.
func ExpandPortList(list string, port int) []int {
	if list == "" {
		if port <= 0 {
			return nil
		}
		return []int{port}
	}
	ports := make([]int, 0, len(list))
	for _, token := range strings.Split(list, ",") {
		lo, hi, ok := splitPortToken(token)
		if !ok {
			continue
		}
		for p := lo; p <= hi; p++ {
			ports = append(ports, p)
		}
	}
	return ports
}

func splitPortToken(token string) (int, int, bool) {
	if lo, hi, found := strings.Cut(token, "-"); found {
		loPort, errLo := strconv.Atoi(lo)
		hiPort, errHi := strconv.Atoi(hi)
		if errLo != nil || errHi != nil {
			return 0, 0, false
		}
		return loPort, hiPort, true
	}
	port, err := strconv.Atoi(token)
	if err != nil {
		return 0, 0, false
	}
	return port, port, true
}
