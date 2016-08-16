// TODO:
// - error management
// - return empty {} response on success

package routed

import (
	"fmt"
	"net"
	"strings"
	"sync"

	log "github.com/Sirupsen/logrus"
	netApi "github.com/docker/go-plugins-helpers/network"
	"github.com/docker/libnetwork/netutils"
	"github.com/vishvananda/netlink"
)

const (
	ifaceID                 = 1
	defaultMtu              = 1500
	sandboxLinkLocalAddress = "169.254.0.2/30"
	defaultGw               = "169.254.0.1/30"
	vethPrefix              = "vethr"
	ethPrefix               = "eth"
)

type routedNetwork struct {
	id        string
	endpoints map[string]*routedEndpoint
	m         sync.Mutex
}

type routedEndpoint struct {
	hostInterfaceName  string
	containerIfaceName string
	macAddress         net.HardwareAddr
	ipv4Address        *net.IPNet
	netFilter          *netFilter
}

type NetDriver struct {
	netApi.Driver
	version string
	mtu     int
	// TODO: should this be a list of networks?
	network *routedNetwork
	m       sync.Mutex
}

func NewNetDriver(version string) (*NetDriver, error) {
	log.Debugf("Initializing routed driver version %+v", version)

	links, err := netlink.LinkList()
	if err != nil {
		log.Errorf("Can't get list of net devices: %s", err)
		return nil, err
	}
	// clean up old interfaces
	for _, lnk := range links {
		if strings.HasPrefix(lnk.Attrs().Name, vethPrefix) {
			if err := netlink.LinkDel(lnk); err != nil {
				log.Errorf("veth couldn't be deleted: %s", lnk.Attrs().Name)
			} else {
				log.Infof("veth cleaned up: %s", lnk.Attrs().Name)
			}
		}
	}

	d := &NetDriver{
		version: version,
		mtu:     defaultMtu,
	}

	return d, nil
}

func (d *NetDriver) GetCapabilities() (*netApi.CapabilitiesResponse, error) {
	res := &netApi.CapabilitiesResponse{Scope: netApi.LocalScope}
	log.Debugf("Get capabilities: responded with %+v", res)
	return res, nil
}

func (d *NetDriver) CreateNetwork(r *netApi.CreateNetworkRequest) error {
	log.Debugf("Create network request: %+v", r)
	d.m.Lock()
	defer d.m.Unlock()

	// TODO: return if network exists?
	d.network = &routedNetwork{id: r.NetworkID, endpoints: make(map[string]*routedEndpoint)}
	log.Infof("Create network %s", r.NetworkID)
	return nil
}

func (d *NetDriver) DeleteNetwork(r *netApi.DeleteNetworkRequest) error {
	log.Debugf("Delete network request: %+v", r)
	d.m.Lock()
	defer d.m.Unlock()

	d.network = nil
	log.Infof("Destroying network %s", r.NetworkID)
	return nil
}

func (d *NetDriver) CreateEndpoint(r *netApi.CreateEndpointRequest) (*netApi.CreateEndpointResponse, error) {
	log.Debugf("Create endpoint request: %+v", r)

	eid := r.EndpointID
	ifInfo := r.Interface

	network := d.network
	network.m.Lock()
	defer network.m.Unlock()

	log.Debugf("Requested Interface %+v", ifInfo)
	addr, _ := netlink.ParseIPNet(ifInfo.Address)
	ep := &routedEndpoint{
		ipv4Address: addr,
	}
	d.network.endpoints[eid] = ep
	log.Infof("Creating endpoint %s %+v", eid, nil)

	return nil, nil
}

func (d *NetDriver) DeleteEndpoint(r *netApi.DeleteEndpointRequest) error {
	log.Debugf("Delete endpoint request: %+v", r)

	eid := r.EndpointID
	network := d.network
	ep := network.endpoints[eid]

	network.m.Lock()
	defer network.m.Unlock()

	delete(network.endpoints, eid)
	log.Infof("Deleting endpoint %s", eid)

	// Try removal of link. Discard error: link pair might have
	// already been deleted by sandbox delete.
	link, err := netlink.LinkByName(ep.hostInterfaceName)
	if err == nil {
		log.Debugf("Deleting host interface %s", ep.hostInterfaceName)
		netlink.LinkDel(link)
	} else {
		log.Debugf("Can't find host interface: $s, %v ", ep.hostInterfaceName, err)
	}

	if ep.netFilter != nil {
		if err := ep.netFilter.removeFiltering(); err != nil {
			log.Warnf("Couldn't remove net filter rules for iface %s,%v", ep.hostInterfaceName, err)
		}
	}

	return nil
}

func (d *NetDriver) EndpointInfo(r *netApi.InfoRequest) (*netApi.InfoResponse, error) {
	log.Debugf("Endpoint info %s:%s", r.NetworkID, r.EndpointID)
	res := &netApi.InfoResponse{Value: map[string]string{}}
	return res, nil
}

func (d *NetDriver) Join(r *netApi.JoinRequest) (*netApi.JoinResponse, error) {
	log.Debugf("Join endpoint %s:%s to r.SandboxKey", r.NetworkID, r.EndpointID)

	eid := r.EndpointID
	network := d.network
	options := r.Options

	network.m.Lock()
	defer network.m.Unlock()

	// Generate host veth
	hostIfaceName, err := generateIfaceName(vethPrefix + string(eid)[:4])
	if err != nil {
		return nil, err
	}

	// Generate host veth
	containerIfaceName, err := generateIfaceName(vethPrefix + string(eid)[:4])
	if err != nil {
		return nil, err
	}

	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name:   hostIfaceName,
			TxQLen: 0,
		},
		PeerName: containerIfaceName,
	}

	log.Debugf("Adding link %+v", veth)
	if err := netlink.LinkAdd(veth); err != nil {
		log.Errorf("Unable to add link %+v:%+v", veth, err)
		return nil, err
	}

	log.Debugf("Setting mtu %+v on %+v", d.mtu, veth)
	if err := netlink.LinkSetMTU(veth, d.mtu); err != nil {
		log.Errorf("Error setting the MTU %s", err)
	}

	log.Debugf("Bringing link up %+v", veth)
	if err := netlink.LinkSetUp(veth); err != nil {
		log.Errorf("Unable to bring up %+v: %+v", veth, err)
		return nil, err
	}

	ep := d.network.endpoints[eid]
	ep.hostInterfaceName = hostIfaceName
	ep.containerIfaceName = containerIfaceName

	iface, _ := netlink.LinkByName(hostIfaceName)
	routeAdd(ep.ipv4Address, iface)

	//for _, ipa := range ep.ipAliases {
	//	routeAdd(ipa, iface)
	//}

	ep.netFilter = NewNetFilter(hostIfaceName, options)
	if err := ep.netFilter.applyFiltering(); err != nil {
		return nil, fmt.Errorf("could not add net filtering %v", err)
	}

	respIface := netApi.InterfaceName{
		SrcName:   containerIfaceName,
		DstPrefix: ethPrefix,
	}

	sandboxRoute := &netApi.StaticRoute{
		Destination: "0.0.0.0/0",
		RouteType:   1, // CONNECTED
		NextHop:     "",
	}

	res := &netApi.JoinResponse{
		InterfaceName:         respIface,
		DisableGatewayService: true,
		StaticRoutes:          []*netApi.StaticRoute{sandboxRoute},
	}

	log.Infof("Join Request Response %+v", res)

	return res, nil
}

func (d *NetDriver) Leave(r *netApi.LeaveRequest) error {
	log.Debugf("Leave %s:%s", r.NetworkID, r.EndpointID)
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

// ErrIfaceName error is returned when a new name could not be generated.
type ErrIfaceName struct{}

func (ein *ErrIfaceName) Error() string {
	return "failed to find name for new interface"
}

func generateIfaceName(vethPrefix string) (string, error) {
	vethLen := 12 - len(vethPrefix)
	for i := 0; i < 3; i++ {
		name, err := netutils.GenerateRandomName(vethPrefix, vethLen)
		if err != nil {
			continue
		}
		if _, err := net.InterfaceByName(name); err != nil {
			if strings.Contains(err.Error(), "no such") {
				return name, nil
			}
			return "", err
		}
	}
	return "", &ErrIfaceName{}
}
