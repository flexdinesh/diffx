package localserver

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/flexdinesh/diffx/internal/pairing"
	"github.com/flexdinesh/diffx/packages/connection"
)

type NetworkOptions struct {
	Listen    string
	Advertise string
}

type RemoteReady struct {
	Status      Status
	Invitations []string
}

type networkAccess struct {
	listener  net.Listener
	pairing   *pairing.Server
	addresses []string
}

// RunRemote owns the same SQLite/runtime singleton as local sessions. An
// existing process is replaced only by explicit policy, never promoted or
// attached to network access. Closing this foreground run stops its server.
func RunRemote(ctx context.Context, settings Settings, input Input, policy SessionOptions, reset StateReset, network NetworkOptions, ready func(RemoteReady) error, stderr io.Writer) error {
	if policy.Attach {
		return errors.New("remote serving cannot attach to an existing local instance")
	}
	settings.Headless = true
	var err error
	settings, err = effectiveSettings(settings)
	if err != nil {
		return err
	}
	// Reject malformed configuration before an explicitly replaced owner stops.
	address := network.Listen
	if address == "" {
		address = ":7981"
	}
	if _, err := net.ResolveTCPAddr("tcp", address); err != nil {
		return errors.New("invalid personal server listen address")
	}
	if network.Advertise != "" {
		zero := strings.Repeat("A", 43)
		if (connection.Invitation{Version: connection.Version, Address: network.Advertise, RunID: zero, Pin: zero, Secret: zero}).String() == "" {
			return errors.New("invalid personal server address")
		}
	}
	var invitations []string
	return runForeground(ctx, settings, input, func(Status) (bool, error) {
		if !policy.Replace {
			return false, errors.New("local instance already running; use --replace to serve remotely")
		}
		return false, nil
	}, false, reset, func(status Status, _ Published, _ bool) error {
		if ready != nil {
			return ready(RemoteReady{Status: status, Invitations: invitations})
		}
		return nil
	}, stderr, func() (*networkAccess, error) {
		access, err := listenNetwork(network)
		if err != nil {
			return nil, err
		}
		invitations, err = access.rotate("")
		if err != nil {
			_ = access.listener.Close()
			return nil, err
		}
		return access, nil
	})
}

func listenNetwork(options NetworkOptions) (*networkAccess, error) {
	address := options.Listen
	if address == "" {
		address = ":7981"
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, errors.New("personal server listener unavailable")
	}
	identity, err := pairing.New()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	addresses, err := advertisedAddresses(listener.Addr(), options.Advertise)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	for _, address := range addresses {
		if (connection.Invitation{Version: connection.Version, Address: address, RunID: identity.RunID, Pin: identity.Pin, Secret: identity.RunID}).String() == "" {
			_ = listener.Close()
			return nil, errors.New("invalid personal server address")
		}
	}
	return &networkAccess{listener: tls.NewListener(listener, identity.TLSConfig()), pairing: identity, addresses: addresses}, nil
}

func (network *networkAccess) rotate(advertise string) ([]string, error) {
	addresses := network.addresses
	if advertise != "" {
		addresses = []string{advertise}
	}
	return network.pairing.Rotate(addresses)
}

func advertisedAddresses(address net.Addr, explicit string) ([]string, error) {
	if explicit != "" {
		return []string{explicit}, nil
	}
	bound, ok := address.(*net.TCPAddr)
	if !ok {
		return nil, errors.New("unexpected personal server listener")
	}
	port := strconv.Itoa(bound.Port)
	if !bound.IP.IsUnspecified() {
		return []string{net.JoinHostPort(bound.IP.String(), port)}, nil
	}
	// SSH_CONNECTION is local process environment, never an HTTP header. The
	// server address is its third field; no external discovery is performed.
	if fields := strings.Fields(os.Getenv("SSH_CONNECTION")); len(fields) == 4 {
		if ip := net.ParseIP(fields[2]); ip != nil && !ip.IsUnspecified() && !ip.IsLoopback() {
			return []string{net.JoinHostPort(ip.String(), port)}, nil
		}
	}
	interfaces, err := net.InterfaceAddrs()
	if err != nil {
		return nil, errors.New("cannot discover personal server addresses; use --advertise")
	}
	var result []string
	for _, address := range interfaces {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.IsGlobalUnicast() && !ip.IsLoopback() {
			result = append(result, net.JoinHostPort(ip.String(), port))
		}
	}
	slices.Sort(result)
	result = slices.Compact(result)
	if len(result) == 0 {
		return nil, errors.New("no usable personal server address; use --advertise")
	}
	return result, nil
}
