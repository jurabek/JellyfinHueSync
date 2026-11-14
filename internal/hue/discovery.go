package hue

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const discoveryConcurrency = 24

// Discover finds Hue Bridges. Priority order:
//
//  1. HUE_SCAN_SUBNET (e.g. "192.168.188.0/24") — explicit, for containers on
//     a bridged network whose own subnet differs from the LAN.
//  2. https://discovery.meethue.com — works from any network with internet and
//     a cloud-linked bridge.
//  3. The container's own subnet — works with host networking or offline LANs.
func Discover(ctx context.Context) []Bridge {
	if subnet := os.Getenv("HUE_SCAN_SUBNET"); subnet != "" {
		if bridges := discoverSubnet(ctx, subnet); len(bridges) > 0 {
			return bridges
		}
	}
	if bridges := verifyCandidates(ctx, discoverCloud(ctx)); len(bridges) > 0 {
		return bridges
	}
	return discoverLocal(ctx)
}

// discoverCloud queries https://discovery.meethue.com for registered bridges.
func discoverCloud(ctx context.Context) []Bridge {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "https://discovery.meethue.com/", nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var results []struct {
		ID                string `json:"id"`
		InternalIPAddress string `json:"internalipaddress"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil
	}

	bridges := make([]Bridge, 0, len(results))
	for _, r := range results {
		id := strings.ToLower(r.ID)
		if !bridgeIDRe.MatchString(id) || r.InternalIPAddress == "" {
			continue
		}
		bridges = append(bridges, Bridge{IP: r.InternalIPAddress, ID: id, Name: "Hue Bridge"})
	}
	return bridges
}

// discoverSubnet scans an explicit CIDR (home LANs are typically /24).
func discoverSubnet(ctx context.Context, cidr string) []Bridge {
	return scanHosts(ctx, subnetHosts(cidr))
}

// discoverLocal scans the container's own subnet.
func discoverLocal(ctx context.Context) []Bridge {
	local, ok := localIPv4()
	if !ok {
		return nil
	}
	prefix := fmt.Sprintf("%d.%d.%d.0/24", local[0], local[1], local[2])
	return scanHosts(ctx, subnetHosts(prefix))
}

// verifyCandidates confirms each candidate is a reachable Hue Bridge and
// resolves its name.
func verifyCandidates(ctx context.Context, candidates []Bridge) []Bridge {
	var (
		mu    sync.Mutex
		found []Bridge
		wg    sync.WaitGroup
	)
	for _, c := range candidates {
		wg.Add(1)
		go func(c Bridge) {
			defer wg.Done()
			api := NewAPI(c, "")
			defer api.Close()
			if b, err := api.Identify(ctx); err == nil {
				mu.Lock()
				found = append(found, b)
				mu.Unlock()
			}
		}(c)
	}
	wg.Wait()
	return found
}

// scanHosts connects to port 443 on each host and identifies Hue Bridges.
func scanHosts(ctx context.Context, hosts []string) []Bridge {
	var (
		mu    sync.Mutex
		found []Bridge
		sem   = make(chan struct{}, discoveryConcurrency)
		wg    sync.WaitGroup
	)
	for _, ip := range hosts {
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

// subnetHosts returns every usable host address in an IPv4 CIDR (excluding the
// network and broadcast addresses). Returns nil for large or invalid networks.
func subnetHosts(cidr string) []string {
	_, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil
	}
	ip4 := ipNet.IP.To4()
	if ip4 == nil {
		return nil
	}
	ones, bits := ipNet.Mask.Size()
	if bits != 32 {
		return nil
	}
	total := 1 << uint(32-ones)
	if total < 4 || total > 2048 {
		return nil
	}

	start := binary.BigEndian.Uint32(ip4)
	hosts := make([]string, 0, total-2)
	for i := 1; i < total-1; i++ {
		a := start + uint32(i)
		hosts = append(hosts, fmt.Sprintf("%d.%d.%d.%d", byte(a>>24), byte(a>>16), byte(a>>8), byte(a)))
	}
	return hosts
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
