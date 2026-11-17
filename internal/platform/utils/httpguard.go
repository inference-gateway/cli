package utils

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// NewPublicOnlyHTTPClient returns an *http.Client that refuses loopback,
// private, link-local (including cloud metadata), multicast and unspecified
// addresses, for URLs from untrusted sources with no host allow-list. It checks
// the resolved IP at dial time and on every redirect, so DNS rebinding cannot bypass it.
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
