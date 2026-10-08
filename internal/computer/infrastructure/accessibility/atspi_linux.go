//go:build linux

package accessibility

import (
	"context"
	"os"
	"time"

	dbus "github.com/godbus/dbus/v5"
	xgbutil "github.com/jezek/xgbutil"
	xprop "github.com/jezek/xgbutil/xprop"
)

const (
	callTimeout     = time.Second
	accessibleIface = "org.a11y.atspi.Accessible"
	coordTypeScreen = uint32(0)
)

var (
	registryRoot = accessible{Bus: "org.a11y.atspi.Registry", Path: "/org/a11y/atspi/accessible/root"}
	busDaemon    = accessible{Bus: "org.freedesktop.DBus", Path: "/org/freedesktop/DBus"}
)

// dbusBus is the AT-SPI2 accessibility bus, reached with godbus.
type dbusBus struct {
	conn *dbus.Conn
}

// dialA11yBus connects to the accessibility bus. ctx bounds the connection and every
// call made on it.
func dialA11yBus(ctx context.Context) (*dbusBus, error) {
	address, err := a11yBusAddress(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := dbus.Connect(address, dbus.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	return &dbusBus{conn: conn}, nil
}

// a11yBusAddress finds the accessibility bus in libatspi's order: AT_SPI_BUS_ADDRESS, the
// X root window property AT_SPI_BUS, then org.a11y.Bus on the session bus. It never
// autolaunches a session bus.
func a11yBusAddress(ctx context.Context) (string, error) {
	if address := os.Getenv("AT_SPI_BUS_ADDRESS"); address != "" {
		return address, nil
	}
	if address := rootWindowBusAddress(); address != "" {
		return address, nil
	}
	return sessionA11yBusAddress(ctx)
}

func rootWindowBusAddress() string {
	x, err := xgbutil.NewConn()
	if err != nil {
		return ""
	}
	defer x.Conn().Close()
	address, _ := xprop.PropValStr(xprop.GetProperty(x, x.RootWin(), "AT_SPI_BUS"))
	return address
}

func sessionA11yBusAddress(ctx context.Context) (string, error) {
	session, err := dbus.SessionBusPrivateNoAutoStartup(dbus.WithContext(ctx))
	if err != nil {
		return "", err
	}
	defer func() { _ = session.Close() }()
	if err := session.Auth(nil); err != nil {
		return "", err
	}
	if err := session.Hello(); err != nil {
		return "", err
	}
	var address string
	err = session.Object("org.a11y.Bus", "/org/a11y/bus").CallWithContext(ctx, "org.a11y.Bus.GetAddress", 0).Store(&address)
	return address, err
}

func (b *dbusBus) close() {
	_ = b.conn.Close()
}

func (b *dbusBus) call(ctx context.Context, node accessible, method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	return b.conn.Object(node.Bus, dbus.ObjectPath(node.Path)).CallWithContext(ctx, method, 0, args...)
}

func (b *dbusBus) property(ctx context.Context, node accessible, iface, name string, value any) error {
	return b.call(ctx, node, "org.freedesktop.DBus.Properties.Get", iface, name).Store(value)
}

func (b *dbusBus) applications(ctx context.Context) ([]accessible, error) {
	return b.children(ctx, registryRoot)
}

func (b *dbusBus) processID(ctx context.Context, bus string) (uint32, error) {
	var pid uint32
	err := b.call(ctx, busDaemon, "org.freedesktop.DBus.GetConnectionUnixProcessID", bus).Store(&pid)
	return pid, err
}

func (b *dbusBus) children(ctx context.Context, node accessible) ([]accessible, error) {
	var children []accessible
	err := b.call(ctx, node, accessibleIface+".GetChildren").Store(&children)
	return children, err
}

// roleName falls back to the localized role name, which is the only one WebKit fills in
// for most web content, a push button included.
func (b *dbusBus) roleName(ctx context.Context, node accessible) (string, error) {
	var role string
	if err := b.call(ctx, node, accessibleIface+".GetRoleName").Store(&role); err != nil || role != "" {
		return role, err
	}
	err := b.call(ctx, node, accessibleIface+".GetLocalizedRoleName").Store(&role)
	return role, err
}

func (b *dbusBus) interfaces(ctx context.Context, node accessible) ([]string, error) {
	var interfaces []string
	err := b.call(ctx, node, accessibleIface+".GetInterfaces").Store(&interfaces)
	return interfaces, err
}

func (b *dbusBus) name(ctx context.Context, node accessible) (string, error) {
	var name string
	err := b.property(ctx, node, accessibleIface, "Name", &name)
	return name, err
}

func (b *dbusBus) states(ctx context.Context, node accessible) (uint64, error) {
	var words []uint32
	if err := b.call(ctx, node, accessibleIface+".GetState").Store(&words); err != nil {
		return 0, err
	}
	var states uint64
	for i, word := range words[:min(len(words), 2)] {
		states |= uint64(word) << (32 * i)
	}
	return states, nil
}

func (b *dbusBus) extents(ctx context.Context, node accessible) (rect, error) {
	var r rect
	err := b.call(ctx, node, componentInterface+".GetExtents", coordTypeScreen).Store(&r)
	return r, err
}

// actionNames reads each action name with NActions and GetName. It never calls
// GetActions: WebKit declares it but never replies to it.
func (b *dbusBus) actionNames(ctx context.Context, node accessible) ([]string, error) {
	var count int32
	if err := b.property(ctx, node, actionInterface, "NActions", &count); err != nil {
		return nil, err
	}
	var names []string
	for i := range count {
		var name string
		if err := b.call(ctx, node, actionInterface+".GetName", i).Store(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
}

func (b *dbusBus) doAction(ctx context.Context, node accessible, index int) (bool, error) {
	var done bool
	err := b.call(ctx, node, actionInterface+".DoAction", int32(index)).Store(&done)
	return done, err
}
