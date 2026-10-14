package metrics

import (
	"net"
	"sync"
)

// maxRouterLabels caps the distinct values of the "router" label. The flow
// listeners accept UDP from any source, so labelling by the packet's source
// address without a cap would let spoofed traffic create Prometheus series
// without bound. Real deployments have a handful of exporters.
const maxRouterLabels = 64

// RouterLabelOther is the label value used once maxRouterLabels is reached.
const RouterLabelOther = "other"

var (
	routerLabelsMu sync.Mutex
	routerLabels   = make(map[string]struct{})
)

// RouterLabel returns the "router" label value for an exporter address.
func RouterLabel(ip net.IP) string {
	s := ip.String()
	routerLabelsMu.Lock()
	defer routerLabelsMu.Unlock()
	if _, ok := routerLabels[s]; ok {
		return s
	}
	if len(routerLabels) >= maxRouterLabels {
		return RouterLabelOther
	}
	routerLabels[s] = struct{}{}
	return s
}
