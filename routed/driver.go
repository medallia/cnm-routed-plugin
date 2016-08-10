package routed

import (
	"fmt"
	"net"

	log "github.com/Sirupsen/logrus"
	netapi "github.com/docker/go-plugins-helpers/network"
	"github.com/vishvananda/netlink"
)

const (
	defaultNetwork = "10.46.0.0/16"
	defaultGateway = "10.46.0.1/32"
)

type routedEndpoint struct {
	iface         string
	macAddress    net.HardwareAddr
	hostInterface string
	ipv4Address   *net.IPNet
	ipAliases     []*net.IPNet
}

type routedNetwork struct {
	id        string
	endpoints map[string]*routedEndpoint
}

type routedPool struct {
	id           string
	subnet       *net.IPNet
	gateway      *net.IPNet
	allocatedIPs map[string]bool
}

type driver struct {
	version string
}

type Driver struct {
	netapi.Driver
	version string
	network *routedNetwork
	pool    *routedPool
	mtu     int
}

func NewDriver(version string) (*Driver, error) {
	log.Debugf("Initializing routed driver version %+v", version)

	network, _ := netlink.ParseIPNet(defaultNetwork)
	gateway, _ := netlink.ParseIPNet(defaultGateway)

	d := &Driver{
		version: version,
		pool: &routedPool{
			id:           "myPool",
			subnet:       network,
			allocatedIPs: make(map[string]bool),
			gateway:      gateway,
		},
		network: &routedNetwork{
			endpoints: make(map[string]*routedEndpoint),
		},
	}

	d.pool.allocatedIPs[fmt.Sprintf("%s", gateway)] = true

	return d, nil
}

func (t *Driver) GetCapabilities() (*netapi.CapabilitiesResponse, error) {
	res := &netapi.CapabilitiesResponse{Scope: netapi.LocalScope}
	log.Debugf("Get capabilities: responded with %+v", res)
	return res, nil
}

func (d *Driver) Createnetwork(r *netapi.CreateNetworkRequest) error {
	log.Debugf("Create network request: %+v", r)

	d.network = &routedNetwork{id: r.NetworkID, endpoints: make(map[string]*routedEndpoint)}

	log.Infof("Create network %s", r.NetworkID)

	return nil
}

func (d *Driver) Deletenetwork(r *netapi.DeleteNetworkRequest) error {
	log.Debugf("Delete network request: %+v", r)

	d.network = nil

	log.Infof("Destroying network %s", r.NetworkID)

	return nil
}

func (d *Driver) CreateEndpoint(r *netapi.CreateEndpointRequest) (*netapi.CreateEndpointResponse, error) {
	log.Debugf("Create endpoint request: %+v", r)

	//var aliases []*net.IPNet
	endID := r.EndpointID
	reqIface := r.Interface
	log.Debugf("Requested Interface %+v", reqIface)
	//log.Debugf("IP Aliases: %+v", reqIface.IPAliases)

	//for _, ipa := range reqIface.IPAliases {
	//	ip, _ := netlink.ParseIPNet(ipa)
	//	aliases = append(aliases, ip)
	//}
	addr, _ := netlink.ParseIPNet(reqIface.Address)

	ep := &routedEndpoint{
		ipv4Address: addr,
		//ipAliases:   aliases,
	}

	d.network.endpoints[endID] = ep

	log.Infof("Creating endpoint %s %+v", endID, nil)

	return nil, nil
	//return &netapi.CreateEndpointResponse{}, nil
}

func (d *Driver) DeleteEndpoint(r *netapi.DeleteEndpointRequest) error {
	log.Debugf("Delete endpoint request: %+v", r)

	delete(d.network.endpoints, r.EndpointID)

	log.Infof("Deleting endpoint %s", r.EndpointID)

	return nil
}

func (d *Driver) EndpointInfo(r *netapi.InfoRequest) (*netapi.InfoResponse, error) {
	log.Debugf("Endpoint info %s:%s", r.NetworkID, r.EndpointID)

	res := &netapi.InfoResponse{Value: map[string]string{}}

	return res, nil
}

func (d *Driver) Join(r *netapi.JoinRequest) (*netapi.JoinResponse, error) {
	log.Debugf("Join endpoint %s:%s to r.SandboxKey", r.NetworkID, r.EndpointID)

	tempName := r.EndpointID[:4]
	hostName := "vethr" + r.EndpointID[:4]

	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:   hostName,
			TxQLen: 0,
		},
		PeerName: tempName,
	}

	log.Debugf("Adding link %+v", veth)

	if err := netlink.LinkAdd(veth); err != nil {
		log.Errorf("Unable to add link %+v:%+v", veth, err)
		return nil, err
	}

	if err := netlink.LinkSetMTU(veth, 1500); err != nil {
		log.Errorf("Error setting the MTU %s", err)
	}

	log.Debugf("Bringing link up %+v", veth)
	if err := netlink.LinkSetUp(veth); err != nil {
		log.Errorf("Unable to bring up %+v: %+v", veth, err)
		return nil, err
	}

	ep := d.network.endpoints[r.EndpointID]
	ep.iface = hostName

	iface, _ := netlink.LinkByName(hostName)
	routeAdd(ep.ipv4Address, iface)

	for _, ipa := range ep.ipAliases {
		routeAdd(ipa, iface)
	}

	respIface := &netapi.InterfaceName{
		SrcName:   tempName,
		DstPrefix: "eth",
	}

	sandboxRoute := netapi.StaticRoute{
		Destination: "0.0.0.0/0",
		RouteType:   1, // CONNECTED
		NextHop:     "",
	}

	resp := &netapi.JoinResponse{
		InterfaceName:         respIface,
		DisableGatewayService: true,
		StaticRoutes:          []netapi.StaticRoute{sandboxRoute},
	}

	log.Infof("Join Request Response %+v", resp)

	return resp, nil
}

func (d *Driver) Leave(r *netapi.LeaveRequest) error {
	log.Debugf("Leave %s:%s", r.NetworkID, r.EndpointID)

	ep := driver.network.endpoints[r.EndpointID]

	link, err := netlink.LinkByName(ep.iface)

	if err == nil {
		log.Debugf("Deleting host interface %s", ep.iface)
		netlink.LinkDel(link)
	} else {
		log.Debugf("interface %s not found", ep.iface)
	}

	log.Infof("Leaving %s:%s", r.NetworkID, r.EndpointID)
	return nil
}

func routeAdd(ip *net.IPNet, iface netlink.Link) error {

	route := netlink.Route{
		LinkIndex: iface.Attrs().Index,
		Dst:       ip,
	}

	log.Debugf("Adding route %+v", route)

	if err := netlink.RouteAdd(&route); err != nil {
		log.Errorf("Unable to add route %+v: %+v", route, err)
	}

	return nil
}
