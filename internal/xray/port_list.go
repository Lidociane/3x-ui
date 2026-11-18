package xray

import (
	"fmt"
	"strconv"
	"strings"
)

// MaxPortListPorts bounds a spec's expansion so a typo like "1-65535" cannot
// turn the bind probe and one listener into a resource blow-up.
const MaxPortListPorts = 256

// ParsePortList validates "443", "443,8443" or "20000-20100" and returns the
// first port plus the normalized list ("" when the spec is a single port).
func ParsePortList(spec string) (int, string, error) {
	spec = strings.ReplaceAll(strings.TrimSpace(spec), " ", "")
	if spec == "" {
		return 0, "", fmt.Errorf("port is required")
	}
	tokens := strings.Split(spec, ",")
	normalized := make([]string, 0, len(tokens))
	first := -1
	total := 0
	for _, token := range tokens {
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
		if first < 0 {
			first = lo
		}
		total += hi - lo + 1
		if total > MaxPortListPorts {
			return 0, "", fmt.Errorf("at most %d ports per inbound", MaxPortListPorts)
		}
		normalized = append(normalized, token)
	}
	if len(normalized) == 1 && !strings.Contains(normalized[0], "-") {
		return first, "", nil
	}
	return first, strings.Join(normalized, ","), nil
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
