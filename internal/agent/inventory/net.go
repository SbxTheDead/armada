package inventory

import "net"

// ifaceInfo is a flattened, string-only view of a network interface so the
// collector code stays free of net.* types (easier to test and serialize).
type ifaceInfo struct {
	Name         string
	HardwareAddr string
	Addrs        []string
}

// netInterfaces enumerates host interfaces using the standard library. It reads
// only public addressing facts (name, MAC, assigned IPs). Loopback and
// down interfaces are included; the control plane decides what to display.
func netInterfaces() ([]ifaceInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]ifaceInfo, 0, len(ifaces))
	for _, ifc := range ifaces {
		info := ifaceInfo{
			Name:         ifc.Name,
			HardwareAddr: ifc.HardwareAddr.String(),
		}
		if addrs, err := ifc.Addrs(); err == nil {
			for _, a := range addrs {
				info.Addrs = append(info.Addrs, a.String())
			}
		}
		out = append(out, info)
	}
	return out, nil
}
