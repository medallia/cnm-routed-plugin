package routed

import (
	"testing"

	netApi "github.com/docker/go-plugins-helpers/network"
)

func TestNetwork(t *testing.T) {
	version := "0.1"
    gateway := "10.100.0.1"
    mtu := 1500
	netId := "c56656e6066544b3c0a42058fad46872fb55eb85bfcfb2217349cf4a1d847f4c"

	d, err := NewNetDriver(version, gateway, mtu)

	if err != nil {
		t.Fatalf("TestCreateNetwork failed: could not create driver - %v", err)
	}

	err = d.CreateNetwork(&netApi.CreateNetworkRequest{
		NetworkID: netId,
	})

	if err != nil {
		t.Fatalf("TestCreateNetwork failed: %v", err)
	}

	if d.network.id != netId {
		t.Fatalf("TestCreateNetwork failed: wrong netId %s", d.network.id)
	}

	err = d.DeleteNetwork(&netApi.DeleteNetworkRequest{
		NetworkID: netId,
	})

	if err != nil {
		t.Fatalf("TestCreateNetwork failed: %v", err)
	}

	if d.network != nil {
		t.Fatalf("TestCreateNetwork failed: d.network not null")
	}

}

func TestEndpoint(t *testing.T) {
	version := "0.1"
    gateway := "10.100.0.1"
    mtu := 1500
	netId := "c56656e6066544b3c0a42058fad46872fb55eb85bfcfb2217349cf4a1d847f4c"
	eId := "4b50fb7f12adb0da3e6662148e9b1bc43b507ad2fd8a0f187ff297cbc88aee05"
	address := "10.1.0.2/32"
	sandBoxKey := "/var/run/docker/netns/68b0caca5d0c"

	d, err := NewNetDriver(version, gateway, mtu)

	if err != nil {
		t.Fatalf("TestCreateSandbox failed: could not create driver - %v", err)
	}

	err = d.CreateNetwork(&netApi.CreateNetworkRequest{
		NetworkID: netId,
	})

	if err != nil {
		t.Fatalf("TestCreateSandbox failed: %v", err)
	}

	_, err = d.CreateEndpoint(&netApi.CreateEndpointRequest{
		NetworkID:  netId,
		EndpointID: eId,
		Interface:  &netApi.EndpointInterface{Address: address},
	})

	if err != nil {
		t.Fatalf("TestCreateSandbox failed: %v", err)
	}

	ep := d.network.endpoints[eId]

	if ep == nil || ep.ipv4Address.String() != address {
		t.Fatalf("TestCreateSandbox failed: wrong Endpoint %v", ep)
	}

	res, err := d.Join(&netApi.JoinRequest{
		NetworkID:  netId,
		EndpointID: eId,
		SandboxKey: sandBoxKey,
	})

	if err != nil {
		t.Fatalf("TestCreateSandbox failed: %v", err)
	}

	if res == nil || res.InterfaceName.DstPrefix != "eth" {
		t.Fatalf("TestCreateSandbox failed: wrong join response %+v", res)
	}

	err = d.Leave(&netApi.LeaveRequest{
		NetworkID:  netId,
		EndpointID: eId,
	})

	if err != nil {
		t.Fatalf("TestCreateSandbox failed: %v", err)
	}
}
