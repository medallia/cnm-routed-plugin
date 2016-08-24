package routed

import (
	"fmt"
	"net"
	"strings"
	"sync"

	log "github.com/Sirupsen/logrus"
	netApi "github.com/docker/go-plugins-helpers/network"
	"github.com/docker/libnetwork/netlabel"
	"github.com/docker/libnetwork/netutils"
	"github.com/docker/libnetwork/types"
	"github.com/vishvananda/netlink"
)

const (
	ifaceID        = 1
	defaultMtu     = 1500
	vethPrefix     = "vethr"
	ethPrefix      = "eth"
	defaultGwIface = "eth0"
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
	gateway string
	mtu     int
	network *routedNetwork
}

func NewNetDriver(version string, gateway string) (*NetDriver, error) {
	log.Debugf("NewNetDriver: Initializing routed driver version %+v", version)

	links, err := netlink.LinkList()
	if err != nil {
		log.Errorf("NewNetDriver: Can't get list of net devices: %s", err)
		return nil, err
	}
	// clean up old interfaces
	for _, lnk := range links {
		if strings.HasPrefix(lnk.Attrs().Name, vethPrefix) {
			if err := netlink.LinkDel(lnk); err != nil {
				log.Errorf("NewNetDriver: veth couldn't be deleted: %s", lnk.Attrs().Name)
			} else {
				log.Infof("NewNetDriver: veth cleaned up: %s", lnk.Attrs().Name)
			}
		}
	}

	d := &NetDriver{
		version: version,
		mtu:     defaultMtu,
		gateway: gateway,
	}

	return d, nil
}

func (d *NetDriver) GetCapabilities() (*netApi.CapabilitiesResponse, error) {
	res := &netApi.CapabilitiesResponse{Scope: netApi.LocalScope}
	log.Debugf("GetCapabilities: responded with %+v", res)
	return res, nil
}

func (d *NetDriver) CreateNetwork(r *netApi.CreateNetworkRequest) error {
	log.Debugf("CreateNetwork: request %+v", r)
	d.network = &routedNetwork{id: r.NetworkID, endpoints: make(map[string]*routedEndpoint)}
	log.Infof("CreateNetwork: NetworkID %s", r.NetworkID)
	return nil
}

func (d *NetDriver) DeleteNetwork(r *netApi.DeleteNetworkRequest) error {
	log.Debugf("DeleteNetwork: request %+v", r)
	d.network = nil
	log.Infof("DeleteNetwork: NetworkID %s", r.NetworkID)
	return nil
}

func (d *NetDriver) CreateEndpoint(r *netApi.CreateEndpointRequest) (*netApi.CreateEndpointResponse, error) {
	log.Debugf("CreateEndpoint: request %+v", r)

	eid := r.EndpointID
	ifInfo := r.Interface

	network := d.network
	network.m.Lock()
	defer network.m.Unlock()

	log.Debugf("CreateEndpoint: Requested Interface %+v", ifInfo)
	addr, _ := netlink.ParseIPNet(ifInfo.Address)
	ep := &routedEndpoint{
		ipv4Address: addr,
	}
	d.network.endpoints[eid] = ep
	log.Infof("CreateEndpoint: created endpoint %s", eid)

	return nil, nil
}

func (d *NetDriver) DeleteEndpoint(r *netApi.DeleteEndpointRequest) error {
	log.Debugf("DeleteEndpoint: request %+v", r)

	eid := r.EndpointID
	network := d.network
	ep := network.endpoints[eid]

	network.m.Lock()
	defer network.m.Unlock()

	delete(network.endpoints, eid)
	log.Infof("DeleteEndpoint: deleted endpoint %s", eid)

	// Try removal of link. Discard error: link pair might have
	// already been deleted by sandbox delete.
	link, err := netlink.LinkByName(ep.hostInterfaceName)
	if err == nil {
		log.Debugf("DeleteEndpoint: Deleting host interface %s", ep.hostInterfaceName)
		netlink.LinkDel(link)
	} else {
		log.Debugf("DeleteEndpoint: Can't find host interface: %s, %v ", ep.hostInterfaceName, err)
	}

	if ep.netFilter != nil {
		if err := ep.netFilter.removeFiltering(); err != nil {
			log.Warnf("DeleteEndpoint: Couldn't remove net filter rules for iface %s, %v", ep.hostInterfaceName, err)
		}
	}

	return nil
}

func (d *NetDriver) EndpointInfo(r *netApi.InfoRequest) (*netApi.InfoResponse, error) {
	log.Debugf("EndpointInfo: reuqest %+v:", r)
	res := &netApi.InfoResponse{Value: map[string]string{}}
	return res, nil
}

func (d *NetDriver) Join(r *netApi.JoinRequest) (*netApi.JoinResponse, error) {
	log.Debugf("Join: request %+v", r)

	eid := r.EndpointID
	network := d.network
	options := r.Options

	network.m.Lock()
	defer network.m.Unlock()

	ep := d.network.endpoints[eid]

	// Generate host-side veth name
	hostIfaceName, err := generateIfaceName(vethPrefix + string(eid)[:4])
	if err != nil {
		return nil, err
	}

	// Generate container-side veth name
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

	// create veth
	log.Debugf("Join: Adding link %+v", veth)
	if err := netlink.LinkAdd(veth); err != nil {
		log.Errorf("Join: Unable to add link %+v:%+v", veth, err)
		return nil, err
	}

	log.Debugf("Join: Setting mtu %+v on %+v", d.mtu, veth)
	if err := netlink.LinkSetMTU(veth, d.mtu); err != nil {
		log.Errorf("Join: Error setting the MTU %s", err)
	}

	log.Debugf("Join: Bringing link up %+v", veth)
	if err := netlink.LinkSetUp(veth); err != nil {
		log.Errorf("Join: Unable to bring up %+v: %+v", veth, err)
		return nil, err
	}

	hostIface, _ := netlink.LinkByName(hostIfaceName)
	if err != nil {
		log.Errorf("Join: Can't find host interface %s", hostIfaceName)
		return nil, err
	}
	defer func() {
		if err != nil {
			log.Infof("Join: Deleting host interface %s", hostIfaceName)
			netlink.LinkDel(hostIface)
		}
	}()

	containerIface, _ := netlink.LinkByName(containerIfaceName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			log.Infof("Join: Deleting container interface %s", containerIfaceName)
			netlink.LinkDel(containerIface)
		}
	}()

	// Down the interface before configuring mac address.
	if err := netlink.LinkSetDown(containerIface); err != nil {
		return nil, fmt.Errorf("Join: could not set link down for container interface %s, %v", containerIfaceName, err)
	}

	var imac net.HardwareAddr
	if opt, ok := options[netlabel.MacAddress]; ok {
		if mac, ok := opt.(net.HardwareAddr); ok {
			log.Debugf("Join: Using Mac Address %s", mac)
			imac = mac
		}
	}
	// Set the sbox's MAC. If specified, use the one configured by user, otherwise generate one based on IP.
	mac := electMacAddress(imac, ep.ipv4Address.IP)

	err = netlink.LinkSetHardwareAddr(containerIface, mac)
	if err != nil {
		return nil, fmt.Errorf("Join: could not set mac address %s for container interface %s, %v", mac, containerIfaceName, err)
	}

	// Up the host interface after finishing all netlink configuration
	if err := netlink.LinkSetUp(hostIface); err != nil {
		return nil, fmt.Errorf("Join: could not set link up for host interface %s, %v", hostIfaceName, err)
	}

	// Configure routes
	routeAdd(ep.ipv4Address, hostIface)

	//for _, ipa := range ep.ipAliases {
	//	routeAdd(ipa, iface)
	//}

	ep.hostInterfaceName = hostIfaceName
	ep.containerIfaceName = containerIfaceName

	// Configure firewall rules
	ep.netFilter = NewNetFilter(hostIfaceName, options)
	if err := ep.netFilter.applyFiltering(); err != nil {
		return nil, fmt.Errorf("Join: could not add net filtering %v", err)
	}

	respIface := netApi.InterfaceName{
		SrcName:   containerIfaceName,
		DstPrefix: ethPrefix,
	}

	gwRoute := &netApi.StaticRoute{
		Destination: fmt.Sprintf("%s/32", d.gateway),
		RouteType:   types.CONNECTED,
		NextHop:     "",
	}

	defaultRoute := &netApi.StaticRoute{
		Destination: "0.0.0.0/0",
		RouteType:   types.NEXTHOP,
		NextHop:     d.gateway,
	}

	res := &netApi.JoinResponse{
		InterfaceName:         respIface,
		DisableGatewayService: true,
		StaticRoutes:          []*netApi.StaticRoute{gwRoute, defaultRoute},
	}

	log.Infof("Join: response %+v", res)

	return res, nil
}

func (d *NetDriver) Leave(r *netApi.LeaveRequest) error {
	log.Debugf("Leave: request %+v", r)
	return nil
}

func electMacAddress(mac net.HardwareAddr, ip net.IP) net.HardwareAddr {
	if mac != nil {
		return mac
	}
	log.Debugf("electMacAddress: Generating MacAddress")
	return generateMacAddr(ip)
}

// Generate a IEEE802 compliant MAC address from the given IP address.
// The generator is guaranteed to be consistent: the same IP will always yield the same
// MAC address. This is to avoid ARP cache issues.
func generateMacAddr(ip net.IP) net.HardwareAddr {
	hw := make(net.HardwareAddr, 6)
	hw[0] = 0x02
	hw[1] = 0x42
	copy(hw[2:], ip.To4())
	return hw
}

func routeAdd(ip *net.IPNet, iface netlink.Link) error {
	route := netlink.Route{
		LinkIndex: iface.Attrs().Index,
		Dst:       ip,
	}
	log.Debugf("routeAdd: Adding route %+v", route)
	if err := netlink.RouteAdd(&route); err != nil {
		log.Errorf("routeAdd: Unable to add route %+v: %+v", route, err)
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
