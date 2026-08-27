package utils

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// NewPublicOnlyHTTPClient returns an *http.Client that refuses to connect to
// loopback, private, link-local (including the 169.254.169.254 cloud metadata
// endpoint), multicast, or unspecified addresses. Use it whenever the URL
// comes from an untrusted source (an LLM tool argument) and no explicit host
// allow-list applies. The check runs at dial time on the resolved IP, so DNS
// rebinding cannot bypass it, and it applies to every redirect hop too.
func NewPublicOnlyHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil {
				return fmt.Errorf("failed to parse dial address %q: %w", host, err)
			}
			if !isPublicIP(ip) {
				return fmt.Errorf("refusing to connect to non-public address %s", ip)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:       http.ProxyFromEnvironment,
			DialContext: dialer.DialContext,
		},
	}
}

// isPublicIP reports whether ip is a globally routable unicast address.
func isPublicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}
