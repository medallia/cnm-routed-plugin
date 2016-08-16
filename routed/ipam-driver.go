package routed

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	ipamApi "github.com/docker/go-plugins-helpers/ipam"
	"github.com/vishvananda/netlink"
)

const (
	network = "10.46.0.0/16"
	gateway = "10.46.0.1/32"
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

func NewIpamDriver(version string) (*IpamDriver, error) {
	log.Debugf("Initializing ipam routed driver version %+v", version)

	net, _ := netlink.ParseIPNet(network)
	gw, _ := netlink.ParseIPNet(gateway)

	pool := &routedPool{
		id:           "myPool",
		subnet:       net,
		allocatedIPs: make(map[string]bool),
		gateway:      gw,
	}

	pool.allocatedIPs[fmt.Sprintf("%s", gateway)] = true

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
	log.Debugf("Get capabilities: responded with %+v", res)
	return res, nil
}

func (driver *IpamDriver) GetDefaultAddressSpaces() (*ipamApi.AddressSpacesResponse, error) {
	res := &ipamApi.AddressSpacesResponse{
		LocalDefaultAddressSpace:  "Testlocal",
		GlobalDefaultAddressSpace: "TestRemote",
	}
	log.Infof("Get default addresse spaces: responded with %+v", res)
	return res, nil
}

func (d *IpamDriver) RequestPool(r *ipamApi.RequestPoolRequest) (*ipamApi.RequestPoolResponse, error) {
	log.Debugf("Pool Request request: %+v", r)

	cidr := fmt.Sprintf("%s", d.pool.subnet)
	id := d.pool.id
	gateway := fmt.Sprintf("%s", d.pool.gateway)

	res := &ipamApi.RequestPoolResponse{
		PoolID: id,
		Pool:   cidr,
		Data:   map[string]string{"com.docker.network.gateway": gateway},
	}

	log.Infof("Pool Request: responded with %+v", res)
	return res, nil
}

func (d *IpamDriver) ReleasePool(r *ipamApi.ReleasePoolRequest) error {
	log.Debugf("Pool Release request: %+v", r)

	log.Infof("Pool release %s ", r.PoolID)
	return nil
}

func (d *IpamDriver) RequestAddress(r *ipamApi.RequestAddressRequest) (*ipamApi.RequestAddressResponse, error) {
	log.Debugf("Address Request request: %+v", r)

	d.pool.m.Lock()
	defer d.pool.m.Unlock()

	if len(r.Address) > 0 {
		addr := fmt.Sprintf("%s/32", r.Address)
		if _, ok := d.pool.allocatedIPs[addr]; ok {
			return nil, fmt.Errorf("%s already allocated", addr)
		}

		res := &ipamApi.RequestAddressResponse{
			Address: addr,
		}
		log.Infof("Addresse request response: %+v", res)
		return res, nil
	}
again:
	// just generate a random address
	rand.Seed(time.Now().UnixNano())
	ip := d.pool.subnet.IP.To4()
	ip[3] = byte(rand.Intn(254))
	netIP := fmt.Sprintf("%s/32", ip)
	log.Infof("ip:%s", netIP)

	_, ok := d.pool.allocatedIPs[netIP]

	if ok {
		goto again
	}
	d.pool.allocatedIPs[netIP] = true
	res := &ipamApi.RequestAddressResponse{
		Address: fmt.Sprintf("%s", netIP),
	}

	log.Infof("Addresse request response: %+v", res)
	return res, nil
}

func (d *IpamDriver) ReleaseAddress(r *ipamApi.ReleaseAddressRequest) error {
	log.Debugf("Address Release request: %+v", r)

	d.pool.m.Lock()
	defer d.pool.m.Unlock()

	ip := fmt.Sprintf("%s/32", r.Address)

	delete(d.pool.allocatedIPs, ip)

	log.Infof("Addresse release %s from %s", r.Address, r.PoolID)
	return nil
}
