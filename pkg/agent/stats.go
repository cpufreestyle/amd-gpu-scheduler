package agent

import (
	"net"
	"runtime"
)

// getLocalIPs returns all non-loopback local IPs
func getLocalIPs() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				ips = append(ips, ipnet.IP.String())
			}
		}
	}
	return ips
}

// getCPUUsage returns current CPU usage percentage (rough estimate)
func getCPUUsage() float64 {
	// Simple heuristic: number of goroutines as proxy
	// Real implementation would use syscall or external tool
	n := runtime.NumGoroutine()
	// Rough: 0-100 based on goroutine count (very rough)
	if n > 200 {
		return 80.0
	}
	return float64(n) * 0.3
}

func init() {
	// Ensure runtime stats are collected
	runtime.GOMAXPROCS(runtime.NumCPU())
}
