package core

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/adapter/outbound"
	"github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/tunnel"
	"github.com/metacubex/mihomo/tunnel/statistic"
)

func TestProxyOnlyTrafficLeavesOutADirectProxyOfAnyName(t *testing.T) {
	direct := outbound.NewDirectWithOption(outbound.DirectOption{Name: "直连"})
	reject := outbound.NewRejectWithOption(outbound.RejectOption{Name: "HK"})
	tunnel.UpdateProxies(map[string]constant.Proxy{
		"直连": adapter.NewProxy(direct),
		"HK": adapter.NewProxy(reject),
	}, nil)
	t.Cleanup(func() { tunnel.UpdateProxies(nil, nil) })

	for _, test := range []struct {
		outbound constant.ProxyAdapter
		proxied  int64
	}{
		{direct, 0},
		{reject, 5},
	} {
		handleResetTraffic()
		local, remote := net.Pipe()
		go func() { _, _ = io.Copy(io.Discard, remote) }()
		metadata := &constant.Metadata{}
		tracker := statistic.NewTCPTracker(
			outbound.NewConn(local, test.outbound),
			statistic.DefaultManager,
			metadata,
			nil,
			0,
			0,
			true,
		)
		// The accounting tunnel wraps the inbound side; here the tracker
		// stands in for it, so the bytes count as sent back to the client.
		inbound := &accountedConn{Conn: tracker, metadata: metadata}

		if _, err := inbound.Write([]byte("hello")); err != nil {
			t.Fatalf("write through %s: %v", test.outbound.Name(), err)
		}
		_ = tracker.Close()
		_ = remote.Close()

		all := handleGetTotalTraffic(false)
		proxied := handleGetTotalTraffic(true)
		if all.Up != 5 || proxied.Down != test.proxied {
			t.Errorf("%s: total %d, proxied %d; want 5 and %d", test.outbound.Name(), all.Up, proxied.Down, test.proxied)
		}
	}
	handleResetTraffic()
}

func TestStoppingClosesTheConnectionsAlreadyOpen(t *testing.T) {
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = remote.Close() })
	go func() { _, _ = io.Copy(io.Discard, remote) }()
	direct := outbound.NewDirectWithOption(outbound.DirectOption{Name: "DIRECT"})
	tracker := statistic.NewTCPTracker(
		outbound.NewConn(local, direct),
		statistic.DefaultManager,
		&constant.Metadata{},
		nil,
		0,
		0,
		true,
	)
	id := tracker.ID()
	if statistic.DefaultManager.Get(id) == nil {
		t.Fatal("the connection was not tracked")
	}

	handleStopListener()

	if statistic.DefaultManager.Get(id) != nil {
		t.Error("the connection outlived the stop")
	}
	if _, err := local.Write([]byte("x")); err == nil {
		t.Error("the connection still carries traffic after the stop")
	}
}

func TestClosingAGroupLeavesTheOtherConnectionsOpen(t *testing.T) {
	direct := outbound.NewDirectWithOption(outbound.DirectOption{Name: "DIRECT"})
	discord := adapter.NewProxy(
		outbound.NewDirectWithOption(outbound.DirectOption{Name: "Discord"}),
	)
	youtube := adapter.NewProxy(
		outbound.NewDirectWithOption(outbound.DirectOption{Name: "YouTube"}),
	)

	tracked := func(chains ...constant.ProxyAdapter) string {
		local, remote := net.Pipe()
		t.Cleanup(func() { _ = remote.Close() })
		go func() { _, _ = io.Copy(io.Discard, remote) }()
		conn := outbound.NewConn(local, direct)
		for _, chain := range chains {
			conn.AppendToChains(chain)
		}
		tracker := statistic.NewTCPTracker(
			conn,
			statistic.DefaultManager,
			&constant.Metadata{},
			nil,
			0,
			0,
			true,
		)
		t.Cleanup(func() { _ = tracker.Close() })
		return tracker.ID()
	}

	// A connection the changed group routed itself.
	directConnection := tracked(discord)
	// A selector above it chose Discord, so its chain carries both names.
	nestedConnection := tracked(discord, youtube)
	// A connection that never met the changed group stays open.
	otherConnection := tracked(youtube)

	handleCloseConnectionsForGroup("Discord")

	if statistic.DefaultManager.Get(directConnection) != nil {
		t.Error("a connection through the changed group was left open")
	}
	if statistic.DefaultManager.Get(nestedConnection) != nil {
		t.Error("a connection through a group that chose the changed one was left open")
	}
	if statistic.DefaultManager.Get(otherConnection) == nil {
		t.Error("closing a group took an unrelated connection with it")
	}
}

type dialingTunnel struct {
	constant.Tunnel
	entered chan struct{}
}

// Stands in for a connection whose upstream dial is still out: it holds the
// client's side until something closes it.
func (t dialingTunnel) HandleTCPConn(conn net.Conn, _ *constant.Metadata) {
	close(t.entered)
	_, _ = io.Copy(io.Discard, conn)
}

func TestStoppingClosesAConnectionStillDialingOut(t *testing.T) {
	isRunning.Store(true)
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = remote.Close() })
	dialing := dialingTunnel{entered: make(chan struct{})}
	handled := make(chan struct{})
	go func() {
		accountingTunnel{dialing}.HandleTCPConn(local, &constant.Metadata{})
		close(handled)
	}()
	<-dialing.entered

	handleStopListener()

	select {
	case <-handled:
	case <-time.After(5 * time.Second):
		t.Fatal("a connection accepted before the stop is still open")
	}
}

func TestAConnectionArrivingAfterTheStopIsTurnedAway(t *testing.T) {
	handleStopListener()
	local, remote := net.Pipe()
	t.Cleanup(func() { _ = remote.Close() })
	dialing := dialingTunnel{entered: make(chan struct{})}
	returned := make(chan struct{})
	go func() {
		accountingTunnel{dialing}.HandleTCPConn(local, &constant.Metadata{Type: constant.SOCKS5})
		close(returned)
	}()

	select {
	case <-returned:
	case <-dialing.entered:
		t.Fatal("a connection reached the tunnel after the stop")
	case <-time.After(5 * time.Second):
		t.Fatal("handling a connection after the stop never returned")
	}
	if _, err := local.Write([]byte("x")); err == nil {
		t.Error("a connection turned away after the stop is still open")
	}
}
