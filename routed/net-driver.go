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
	// TODO: should have a list of networks instead of only one network?
	network *routedNetwork
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
	d.network = &routedNetwork{id: r.NetworkID, endpoints: make(map[string]*routedEndpoint)}
	log.Infof("Create network %s", r.NetworkID)
	return nil
}

func (d *NetDriver) DeleteNetwork(r *netApi.DeleteNetworkRequest) error {
	log.Debugf("Delete network request: %+v", r)
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

	hostIface, _ := netlink.LinkByName(hostIfaceName)
	if err != nil {
		log.Errorf("Can't find Host Interface: %s", hostIfaceName)
		return nil, err
	}
	defer func() {
		if err != nil {
			log.Infof("Deleting Host veth %s", hostIfaceName)
			netlink.LinkDel(hostIface)
		}
	}()

	containerIface, _ := netlink.LinkByName(containerIfaceName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			log.Infof("Deleting Container veth %s", containerIfaceName)
			netlink.LinkDel(containerIface)
		}
	}()

	// Down the interface before configuring mac address.
	if err := netlink.LinkSetDown(containerIface); err != nil {
		return nil, fmt.Errorf("could not set link down for container interface %s: %v", containerIfaceName, err)
	}

	var imac net.HardwareAddr
	if opt, ok := options[netlabel.MacAddress]; ok {
		if mac, ok := opt.(net.HardwareAddr); ok {
			log.Debugf("Using Mac Address: %s", mac)
			imac = mac
		}
	}
	// Set the sbox's MAC. If specified, use the one configured by user, otherwise generate one based on IP.
	mac := electMacAddress(imac, ep.ipv4Address.IP)

	err = netlink.LinkSetHardwareAddr(containerIface, mac)
	if err != nil {
		return nil, fmt.Errorf("could not set mac address %s for container interface %s: %v", mac, containerIfaceName, err)
	}

	// Up the host interface after finishing all netlink configuration
	if err := netlink.LinkSetUp(hostIface); err != nil {
		return nil, fmt.Errorf("could not set link up for host interface %s: %v", hostIfaceName, err)
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

func electMacAddress(mac net.HardwareAddr, ip net.IP) net.HardwareAddr {
	if mac != nil {
		return mac
	}
	log.Debugf("Generating MacAddress")
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