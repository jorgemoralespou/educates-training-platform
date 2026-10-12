package registry

import (
	"net/netip"
	"testing"
)

func testClusterNetwork(subnet, gateway string) clusterNetwork {
	nw := clusterNetwork{holders: map[netip.Addr]string{}}

	if subnet != "" {
		nw.subnet = netip.MustParsePrefix(subnet)
	}

	if gateway != "" {
		nw.gateway = netip.MustParseAddr(gateway)
	}

	return nw
}

func TestRegistryFixedAddress(t *testing.T) {
	cases := []struct {
		name    string
		subnet  string
		gateway string
		want    string
		wantErr bool
	}{
		{name: "kind default /16", subnet: "172.18.0.0/16", gateway: "172.18.0.1", want: "172.18.255.254"},
		{name: "/20 from the 192.168 pool", subnet: "192.168.16.0/20", gateway: "192.168.16.1", want: "192.168.31.254"},
		{name: "custom /24", subnet: "10.10.0.0/24", gateway: "10.10.0.1", want: "10.10.0.254"},
		{name: "unmasked subnet", subnet: "172.18.4.7/16", gateway: "172.18.0.1", want: "172.18.255.254"},
		{name: "gateway at the top", subnet: "10.10.0.0/24", gateway: "10.10.0.254", want: "10.10.0.253"},
		{name: "no gateway reported", subnet: "172.18.0.0/16", want: "172.18.255.254"},
		{name: "smallest usable /30", subnet: "10.10.0.0/30", gateway: "10.10.0.1", want: "10.10.0.2"},
		{name: "/30 with gateway at the top", subnet: "10.10.0.0/30", gateway: "10.10.0.2", want: "10.10.0.1"},
		{name: "/31 too small", subnet: "10.10.0.0/31", wantErr: true},
		{name: "IPv6 only", subnet: "fc00:f853:ccd:e793::/64", wantErr: true},
		{name: "no subnet", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := registryFixedAddress(testClusterNetwork(tc.subnet, tc.gateway))

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %s", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.String() != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestMirrorFixedAddressWindow(t *testing.T) {
	cases := []struct {
		name    string
		subnet  string
		gateway string
		lowest  string
		highest string
	}{
		{name: "kind default /16", subnet: "172.18.0.0/16", gateway: "172.18.0.1", lowest: "172.18.255.54", highest: "172.18.255.253"},
		{name: "/20 from the 192.168 pool", subnet: "192.168.16.0/20", gateway: "192.168.16.1", lowest: "192.168.31.54", highest: "192.168.31.253"},
		{name: "custom /24 shrinks the window", subnet: "10.10.0.0/24", gateway: "10.10.0.1", lowest: "10.10.0.191", highest: "10.10.0.253"},
		{name: "gateway at the top shifts the window", subnet: "10.10.0.0/24", gateway: "10.10.0.254", lowest: "10.10.0.190", highest: "10.10.0.252"},
	}

	mirrors := []string{
		"educates-registry-mirror-docker.io",
		"educates-registry-mirror-ghcr.io",
		"educates-registry-mirror-registry.k8s.io",
		"educates-registry-mirror-reg.kyverno.io",
		"educates-registry-mirror-quay.io",
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nw := testClusterNetwork(tc.subnet, tc.gateway)
			lowest := netip.MustParseAddr(tc.lowest)
			highest := netip.MustParseAddr(tc.highest)

			for _, mirror := range mirrors {
				got, err := mirrorFixedAddress(nw, mirror)

				if err != nil {
					t.Fatalf("%s: unexpected error: %v", mirror, err)
				}

				if got.Less(lowest) || highest.Less(got) {
					t.Errorf("%s: got %s, want within %s to %s", mirror, got, lowest, highest)
				}

				again, _ := mirrorFixedAddress(nw, mirror)

				if again != got {
					t.Errorf("%s: not stable, got %s then %s", mirror, got, again)
				}
			}
		})
	}
}

func TestMirrorFixedAddressProbesPastHeldAddresses(t *testing.T) {
	nw := testClusterNetwork("172.18.0.0/16", "172.18.0.1")
	mirror := "educates-registry-mirror-docker.io"

	first, err := mirrorFixedAddress(nw, mirror)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nw.holders[first] = "educates-registry-mirror-ghcr.io"

	second, err := mirrorFixedAddress(nw, mirror)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := first.Prev()
	if first == netip.MustParseAddr("172.18.255.54") {
		want = netip.MustParseAddr("172.18.255.253")
	}

	if second != want {
		t.Errorf("got %s, want %s after %s was taken", second, want, first)
	}
}

func TestMirrorFixedAddressSkipsGateway(t *testing.T) {
	mirror := "educates-registry-mirror-docker.io"

	hashed, err := mirrorFixedAddress(testClusterNetwork("172.18.0.0/16", ""), mirror)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := mirrorFixedAddress(testClusterNetwork("172.18.0.0/16", hashed.String()), mirror)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got == hashed {
		t.Errorf("got the gateway address %s", got)
	}
}

func TestMirrorFixedAddressKeepsHeldWindowAddress(t *testing.T) {
	nw := testClusterNetwork("172.18.0.0/16", "172.18.0.1")
	mirror := "educates-registry-mirror-docker.io"

	hashed, err := mirrorFixedAddress(nw, mirror)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	held := netip.MustParseAddr("172.18.255.100")
	if held == hashed {
		held = held.Next()
	}

	nw.holders[held] = mirror

	got, err := mirrorFixedAddress(nw, mirror)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != held {
		t.Errorf("got %s, want the held %s", got, held)
	}
}

func TestMirrorFixedAddressIgnoresHeldAddressOutsideWindow(t *testing.T) {
	nw := testClusterNetwork("172.18.0.0/16", "172.18.0.1")
	mirror := "educates-registry-mirror-docker.io"

	hashed, err := mirrorFixedAddress(nw, mirror)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	nw.holders[netip.MustParseAddr("172.18.0.6")] = mirror

	got, err := mirrorFixedAddress(nw, mirror)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got != hashed {
		t.Errorf("got %s, want the hashed %s", got, hashed)
	}
}

func TestMirrorFixedAddressFullWindow(t *testing.T) {
	// A /29 has six usable addresses: the registry takes .6 and the mirror
	// window is the single address below it.
	nw := testClusterNetwork("10.10.0.0/29", "10.10.0.1")
	mirror := "educates-registry-mirror-docker.io"

	got, err := mirrorFixedAddress(nw, mirror)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if want := netip.MustParseAddr("10.10.0.5"); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}

	nw.holders[got] = "educates-registry-mirror-ghcr.io"

	if got, err := mirrorFixedAddress(nw, mirror); err == nil {
		t.Errorf("expected error for a full window, got %s", got)
	}
}

func TestMirrorFixedAddressTooSmallSubnet(t *testing.T) {
	nw := testClusterNetwork("10.10.0.0/30", "10.10.0.1")

	if got, err := mirrorFixedAddress(nw, "educates-registry-mirror-docker.io"); err == nil {
		t.Errorf("expected error for a /30, got %s", got)
	}
}
