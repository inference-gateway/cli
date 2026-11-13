package config

import (
	"fmt"
	"regexp"
	"strings"
)

// GatewayVersionLatest is the gateway.version that follows the newest gateway
// release, the default.
const GatewayVersionLatest = "latest"

const (
	gatewayTagPrefix        = "v"
	gatewayVersionExample   = "0.65.0"
	gatewayVersionPattern   = `^\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`
	invalidGatewayVersionOf = "invalid gateway.version %q: must be %q or a gateway release such as %q"
)

var gatewayVersionShape = regexp.MustCompile(gatewayVersionPattern)

// PinnedRelease returns the tag of the gateway release gateway.version pins,
// such as "v0.65.0". It reports false when the gateway follows the latest
// release, which an unset version does too.
func (c GatewayConfig) PinnedRelease() (tag string, pinned bool) {
	version := strings.TrimSpace(c.Version)
	if version == "" || version == GatewayVersionLatest {
		return "", false
	}
	return gatewayTagPrefix + strings.TrimPrefix(version, gatewayTagPrefix), true
}

func (c GatewayConfig) validateVersion() error {
	tag, pinned := c.PinnedRelease()
	if !pinned || gatewayVersionShape.MatchString(strings.TrimPrefix(tag, gatewayTagPrefix)) {
		return nil
	}
	return fmt.Errorf(invalidGatewayVersionOf, c.Version, GatewayVersionLatest, gatewayVersionExample)
}
