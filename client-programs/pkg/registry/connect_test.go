package registry

import (
	"errors"
	"net/netip"
	"testing"
)

// connectAttempts records the addresses connectWithFallback asks for and
// answers each with the next error in errs.
type connectAttempts struct {
	errs  []error
	addrs []netip.Addr
}

func (c *connectAttempts) connect(addr netip.Addr) error {
	c.addrs = append(c.addrs, addr)
	err := c.errs[0]
	c.errs = c.errs[1:]
	return err
}

func TestConnectWithFallbackConnectsAtTheFixedAddress(t *testing.T) {
	fixed := netip.MustParseAddr("172.18.255.203")
	attempts := &connectAttempts{errs: []error{nil}}

	refused, err := connectWithFallback(attempts.connect, fixed)

	if err != nil || refused != nil {
		t.Fatalf("connectWithFallback() = %v, %v, want nil, nil", refused, err)
	}
	if len(attempts.addrs) != 1 || attempts.addrs[0] != fixed {
		t.Errorf("connected at %v, want only %v", attempts.addrs, fixed)
	}
}

func TestConnectWithFallbackReportsAFixedAddressDockerRefused(t *testing.T) {
	fixed := netip.MustParseAddr("172.18.255.203")
	refusal := errors.New("user specified IP address is supported only when connecting to networks with user configured subnets")
	attempts := &connectAttempts{errs: []error{refusal, nil}}

	refused, err := connectWithFallback(attempts.connect, fixed)

	if err != nil {
		t.Fatalf("connectWithFallback() error = %v, want nil", err)
	}
	if refused != refusal {
		t.Errorf("refused = %v, want the fixed address's error %v", refused, refusal)
	}
	if len(attempts.addrs) != 2 || attempts.addrs[1].IsValid() {
		t.Errorf("connected at %v, want the fixed address then one Docker picks", attempts.addrs)
	}
}

func TestConnectWithFallbackFailsWithoutBlamingTheFixedAddress(t *testing.T) {
	fixed := netip.MustParseAddr("172.18.255.203")
	sandbox := errors.New("network sandbox for container b66ed991957d not found")
	attempts := &connectAttempts{errs: []error{sandbox, sandbox}}

	refused, err := connectWithFallback(attempts.connect, fixed)

	if err != sandbox {
		t.Errorf("error = %v, want the connect error %v", err, sandbox)
	}
	if refused != nil {
		t.Errorf("refused = %v, want nil: a container no address can join says nothing about the fixed one", refused)
	}
}
