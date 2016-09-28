package routed

import (
	"fmt"
	"net"
	"strconv"
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
	vethPrefix     = "vethr"
	ethPrefix      = "eth"
	defaultGwIface = "eth0"
	routedPrefix   = "com.medallia.routed.network"
	ipAliases      = routedPrefix + ".ipAliases"
	mtuValue       = routedPrefix + ".mtu"
	ingressAllowed = routedPrefix + ".ingressAllowed"
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
	ipAliases          []*net.IPNet
}

type NetDriver struct {
	netApi.Driver
	version string
	gateway string
	mtu     int
	network *routedNetwork
}

func NewNetDriver(version string, gateway string, mtu int) (*NetDriver, error) {
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
		mtu:     mtu,
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
	opts := r.Options

	network := d.network
	network.m.Lock()
	defer network.m.Unlock()

	log.Debugf("CreateEndpoint: Requested Interface %+v", ifInfo)
	addr, _ := netlink.ParseIPNet(ifInfo.Address)

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
	log.Debugf("CreateEndpoint: Adding link %+v", veth)
	if err := netlink.LinkAdd(veth); err != nil {
		log.Errorf("CreateEndpoint: Unable to add link %+v:%+v", veth, err)
		return nil, err
	}

	hostIface, _ := netlink.LinkByName(hostIfaceName)
	if err != nil {
		log.Errorf("CreateEndpoint: Can't find host interface %s", hostIfaceName)
		return nil, err
	}
	defer func() {
		if err != nil {
			log.Infof("CreateEndpoint: Deleting host interface %s", hostIfaceName)
			netlink.LinkDel(hostIface)
		}
	}()

	containerIface, _ := netlink.LinkByName(containerIfaceName)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			log.Infof("CreateEndpoint: Deleting container interface %s", containerIfaceName)
			netlink.LinkDel(containerIface)
		}
	}()

	// Set MTU for interfaces
	mtu := d.mtu
	if val, ok := opts[mtuValue]; ok {
		mtu, err = strconv.Atoi(val.(string))

		if err != nil {
			log.Errorf("CreateEndpoint: Error setting the MTU value %+v, %s", val, err)
			return nil, err
		}
	}

	if mtu != 0 {
		log.Debugf("CreateEndpoint: Setting MTU %+v on %+v", mtu, veth)

		if err := netlink.LinkSetMTU(hostIface, mtu); err != nil {
			log.Errorf("CreateEndpoint: Error setting the MTU %s", err)
			return nil, err
		}

		if err := netlink.LinkSetMTU(containerIface, mtu); err != nil {
			log.Errorf("CreateEndpoint: Error setting the MTU %s", err)
			return nil, err
		}
	}

	// Put down the interface before configuring mac address.
	if err := netlink.LinkSetDown(containerIface); err != nil {
		log.Errorf("CreateEndpoint: could not set link down for container interface %s, %v", containerIfaceName, err)
		return nil, err
	}

	var imac net.HardwareAddr
	if opt, ok := opts[netlabel.MacAddress]; ok {
		if mac, ok := opt.(net.HardwareAddr); ok {
			log.Debugf("CreateEndpoint: Using Mac Address %s", mac)
			imac = mac
		}
	}
	// Set the sbox's MAC. If specified, use the one configured by user, otherwise generate one based on IP.
	mac := electMacAddress(imac, addr.IP)

	err = netlink.LinkSetHardwareAddr(containerIface, mac)
	if err != nil {
		log.Errorf("CreateEndpoint: could not set mac address %s for container interface %s, %v", mac, containerIfaceName, err)
		return nil, err
	}

	log.Debugf("CreateEndpoint: Bringing link up %+v", veth)
	// Up the host interface after finishing all netlink configuration
	if err := netlink.LinkSetUp(hostIface); err != nil {
		log.Errorf("CreateEndpoint: could not set link up for host interface %s, %v", hostIfaceName, err)
		return nil, err
	}

	if err := netlink.LinkSetUp(containerIface); err != nil {
		log.Errorf("CreateEndpoint: could not set link up for host interface %s, %v", containerIfaceName, err)
		return nil, err
	}

	ep := &routedEndpoint{
		hostInterfaceName:  hostIfaceName,
		containerIfaceName: containerIfaceName,
		macAddress:         mac,
		ipv4Address:        addr,
	}

	// Add IP aliases
	if aliasesString, ok := opts[ipAliases]; ok {

		aliases := strings.Split(aliasesString.(string), ",")
		ep.ipAliases = make([]*net.IPNet, 0, len(aliases))

		for _, ips := range aliases {

			ifInfo.IPAliases = append(ifInfo.IPAliases, ips)
			alias, err := types.ParseCIDR(ips)

			if err != nil {
				log.Errorf("CreateEndpoint: wrong IP alias %s for interface", ips)
				return nil, err
			}
			ep.ipAliases = append(ep.ipAliases, alias)
		}
	}

	// Configure firewall rules
	if configString, ok := opts[ingressAllowed]; ok {

		config, err := NetFilterConfigParse(configString.(string))
		if err != nil {
			log.Errorf("CreateEndpoint: could not parse net filtering %v", err)
			return nil, err
		}

		ep.netFilter = NewNetFilter(hostIfaceName, config)
		if err := ep.netFilter.applyFiltering(); err != nil {
			log.Errorf("CreateEndpoint: could not add net filtering %v", err)
			return nil, err
		}
	}

	d.network.endpoints[eid] = ep

	log.Infof("CreateEndpoint: created endpoint %s", eid)

	ifInfo.Address = ""
	res := &netApi.CreateEndpointResponse{
		Interface: ifInfo,
	}

	return res, nil
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
	log.Debugf("EndpointInfo: request %+v:", r)
	res := &netApi.InfoResponse{Value: map[string]string{}}
	return res, nil
}

func (d *NetDriver) Join(r *netApi.JoinRequest) (*netApi.JoinResponse, error) {
	log.Debugf("Join: request %+v", r)

	eid := r.EndpointID
	network := d.network

	network.m.Lock()
	defer network.m.Unlock()

	ep := d.network.endpoints[eid]

	hostIface, err := netlink.LinkByName(ep.hostInterfaceName)
	if err != nil {
		log.Errorf("CreateEndpoint: Can't find host interface %s", ep.hostInterfaceName)
		return nil, err
	}
	defer func() {
		if err != nil {
			log.Infof("CreateEndpoint: Deleting host interface %s", ep.hostInterfaceName)
			netlink.LinkDel(hostIface)
		}
	}()

	// Configure routes on host side
	routeAdd(ep.ipv4Address, hostIface)
	for _, ipa := range ep.ipAliases {
		routeAdd(ipa, hostIface)
	}

	// Configure routes on container side
	respIface := netApi.InterfaceName{
		SrcName:   ep.containerIfaceName,
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
