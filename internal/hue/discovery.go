package hue

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

const discoveryConcurrency = 24

// Discover scans the local subnet for Hue Bridges, the same fallback the
// Android app uses. It only connects to port 443 on the container's own subnet.
func Discover(ctx context.Context) []Bridge {
	local, ok := localIPv4()
	if !ok {
		return nil
	}
	prefix := fmt.Sprintf("%d.%d.%d", local[0], local[1], local[2])
	own := int(local[3])

	var (
		mu    sync.Mutex
		found []Bridge
		sem   = make(chan struct{}, discoveryConcurrency)
		wg    sync.WaitGroup
	)
	for host := 1; host <= 254; host++ {
		if host == own {
			continue
		}
		ip := fmt.Sprintf("%s.%d", prefix, host)
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if !portOpen(ctx, ip) {
				return
			}
			api := NewAPI(Bridge{IP: ip, Name: "Hue Bridge"}, "")
			defer api.Close()
			if b, err := api.Identify(ctx); err == nil {
				mu.Lock()
				found = append(found, b)
				mu.Unlock()
			}
		}(ip)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	case <-time.After(20 * time.Second):
	}
	return found
}

func portOpen(ctx context.Context, ip string) bool {
	d := net.Dialer{Timeout: 200 * time.Millisecond}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func localIPv4() (net.IP, bool) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, false
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() {
				continue
			}
			if isSiteLocal(ip4) {
				return ip4, true
			}
		}
	}
	return nil, false
}

func isSiteLocal(ip net.IP) bool {
	return ip[0] == 10 ||
		(ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31) ||
		(ip[0] == 192 && ip[1] == 168)
}
