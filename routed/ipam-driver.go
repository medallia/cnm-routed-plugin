package routed

import (
	"fmt"
	"net"
	"sync"

	log "github.com/Sirupsen/logrus"
	ipamApi "github.com/docker/go-plugins-helpers/ipam"
	"github.com/docker/libnetwork/netlabel"
	"github.com/vishvananda/netlink"
)

const (
	network = "10.46.0.0/16"
)

type routedPool struct {
	id           string
	subnet       *net.IPNet
	gateway      *net.IPNet
	allocatedIPs map[string]bool
	m            sync.Mutex
}

type IpamDriver struct {
	ipamApi.Ipam
	version string
	pool    *routedPool
}

func NewIpamDriver(version string, gateway string) (*IpamDriver, error) {
	log.Debugf("NewIpamDriver: Initializing ipam routed driver version %+v", version)

	net, _ := netlink.ParseIPNet(network)
	gw, _ := netlink.ParseIPNet(fmt.Sprintf("%s/32", gateway))

	pool := &routedPool{
		id:           "routed",
		subnet:       net,
		allocatedIPs: make(map[string]bool),
		gateway:      gw,
	}

	pool.allocatedIPs[gateway] = true

	d := &IpamDriver{
		version: version,
		pool:    pool,
	}

	return d, nil
}

func (driver *IpamDriver) GetCapabilities() (*ipamApi.CapabilitiesResponse, error) {
	res := &ipamApi.CapabilitiesResponse{
		RequiresMACAddress: false,
	}
	log.Debugf("GetCapabilities: responded with %+v", res)
	return res, nil
}

func (driver *IpamDriver) GetDefaultAddressSpaces() (*ipamApi.AddressSpacesResponse, error) {
	res := &ipamApi.AddressSpacesResponse{
		LocalDefaultAddressSpace:  "Testlocal",
		GlobalDefaultAddressSpace: "TestRemote",
	}
	log.Infof("GetDefaultAddressSpaces: responded with %+v", res)
	return res, nil
}

func (d *IpamDriver) RequestPool(r *ipamApi.RequestPoolRequest) (*ipamApi.RequestPoolResponse, error) {
	log.Debugf("RequestPool: %+v", r)

	ip, _ := netlink.ParseIPNet(r.Pool)

	if ip != nil {
		d.pool.subnet = ip
	}

	cidr := d.pool.subnet.String()
	id := d.pool.id
	gateway := d.pool.gateway.String()

	res := &ipamApi.RequestPoolResponse{
		PoolID: id,
		Pool:   cidr,
		Data:   map[string]string{netlabel.Gateway: gateway},
	}

	log.Infof("RequestPool: responded with %+v", res)
	log.Infof("RequestPool: subnet is %v, gateway is %v", d.pool.subnet.String(),
		d.pool.gateway.String())
	return res, nil
}

func (d *IpamDriver) ReleasePool(r *ipamApi.ReleasePoolRequest) error {
	log.Debugf("ReleasePool: request %+v", r)

	log.Infof("ReleasePool: PoolID %s ", r.PoolID)
	return nil
}

func (d *IpamDriver) RequestAddress(r *ipamApi.RequestAddressRequest) (*ipamApi.RequestAddressResponse, error) {
	log.Debugf("RequestAddress: request %+v", r)

	if r.Options["RequestAddressType"] == netlabel.Gateway {
		return nil, fmt.Errorf("RequestAddress: can't change gateway")
	}

	d.pool.m.Lock()
	defer d.pool.m.Unlock()

	addr := fmt.Sprintf("%s/32", r.Address)

	ip, _ := netlink.ParseIPNet(addr)

	if ip == nil {
		return nil, fmt.Errorf("RequestAddress: invalid IP address %v\n", r.Address)
	}

	if exists := d.pool.allocatedIPs[addr]; exists {
		return nil, fmt.Errorf("RequestAddress: address %s already allocated", addr)
	}

	d.pool.allocatedIPs[addr] = true

	res := &ipamApi.RequestAddressResponse{
		Address: addr,
	}

	log.Infof("RequestAddress: response %+v", res)

	return res, nil
}

func (d *IpamDriver) ReleaseAddress(r *ipamApi.ReleaseAddressRequest) error {
	log.Debugf("ReleaseAddress: request %+v", r)

	d.pool.m.Lock()
	defer d.pool.m.Unlock()

	ip := fmt.Sprintf("%s/32", r.Address)

	delete(d.pool.allocatedIPs, ip)

	log.Infof("ReleaseAddress: %s from %s", r.Address, r.PoolID)
	return nil
}
