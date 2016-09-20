package routed

import (
	"fmt"
	"testing"

	ipamApi "github.com/docker/go-plugins-helpers/ipam"
)

func TestRequestPool(t *testing.T) {
	version := "0.1"
    gateway := "10.100.0.1"
	subnet := "10.1.0.0/16"

	d, err := NewIpamDriver(version, gateway)

	if err != nil {
		t.Fatalf("TestRequestPool failed: could not create driver - %v", err)
	}

	_, err = d.RequestPool(&ipamApi.RequestPoolRequest{
		Pool:         subnet,
		AddressSpace: "Testlocal",
	})

	if err != nil {
		t.Fatalf("TestRequestPool failed: %v", err)
	}

	if d.pool.subnet.String() != subnet {
		t.Fatalf("TestRequestPool failed: wrong subnet %s", d.pool.subnet.String())
	}
}

func TestRequestAddress(t *testing.T) {
	version := "0.1"
    gateway := "10.100.0.1"
	address := "10.1.0.2"

	d, err := NewIpamDriver(version, gateway)

	if err != nil {
		t.Fatalf("TestRequestAddress failed : %v", err)
	}

	res, err := d.RequestAddress(&ipamApi.RequestAddressRequest{
		PoolID:  "routed",
		Address: address,
	})

	if err != nil || (res != nil && res.Address != fmt.Sprintf("%s/32", address)) {
		t.Fatalf("TestRequestAddress failed for address %s: %+v", address, err)
	}

	res, err = d.RequestAddress(&ipamApi.RequestAddressRequest{
		PoolID:  "routed",
		Address: address,
	})

	if err == nil {
		t.Fatalf("TestRequestAddress failed : added same address %s twice", address)
	}
}
