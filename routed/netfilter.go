package routed

import (
	"fmt"
	"net"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/libnetwork/iptables"
)

const (
	containersChainName      = "CONTAINERS"
	containerRejectChainName = "CONTAINER-REJECT"
	vethChainPrefix          = "CONTAINER-"
)

type IPRange struct {
	from net.IP
	to   net.IP
}

func ParseIPRange(ipRange string) *IPRange {
	ipStrs := strings.Split(ipRange, "-")
	if len(ipStrs) == 2 {
		var ips [2]net.IP
		for idx, ipStr := range ipStrs {
			ips[idx] = net.ParseIP(strings.TrimSpace(ipStr))
		}
		if ips[0] != nil && ips[1] != nil {
			return &IPRange{from: ips[0], to: ips[1]}
		}
	}
	return nil
}

func (r *IPRange) String() string {
	return r.from.String() + "-" + r.to.String()
}

type netFilterConfig struct {
	allowedNets   []*net.IPNet
	allowedRanges []*IPRange
}

type netFilter struct {
	ifaceName string
	config    *netFilterConfig
}

func ParseIpOrNet(ipStr string) *net.IPNet {
	if !strings.Contains(ipStr, "/") {
		ipStr += "/32"
	}

	if _, ipNet, err := net.ParseCIDR(ipStr); err == nil {
		return ipNet
	} else {
		return nil
	}
}

func NetFilterConfigParse(ingressAllowedString string) (*netFilterConfig, error) {
	if ingressAllowedString != "" {
		config := new(netFilterConfig)
		for _, filterElement := range strings.Split(ingressAllowedString, ",") {
			filterElement = strings.TrimSpace(filterElement)
			ipNet := ParseIpOrNet(filterElement)
			if ipNet == nil {
				if ipRange := ParseIPRange(filterElement); ipRange != nil {
					config.allowedRanges = append(config.allowedRanges, ipRange)
				} else {
					return nil, fmt.Errorf("NetFilter: Could not parse IP, CIDR or IPRange %s", filterElement)
				}
			} else {
				config.allowedNets = append(config.allowedNets, ipNet)
			}
		}
		return config, nil
	} else {
		return nil, nil
	}
}

func NewNetFilter(ifaceName string, epOptions map[string]interface{}) *netFilter {
	log.Debugf("New NetFilter for iface %s and options %s", ifaceName, epOptions)

	// TODO: Fix
	//ingressFiltering := epOptions[netlabel.IngressAllowed].(*netFilterConfig)
	//if ingressFiltering == nil {
	//	log.Info("NetFilter: No network ingress filtering specified")
	//}

	//return &netFilter{ifaceName, ingressFiltering}
	return &netFilter{ifaceName, nil}
}

func chainExists(chainName string) bool {
	return iptables.Exists("", chainName, "-N", chainName)
}

func (n *netFilter) applyFiltering() error {
	if n.config == nil {
		return nil // Net Filtering disabled
	}

	vethChainName := vethChainPrefix + n.ifaceName

	log.Debugf("NetFilter. Allowing ingress: %s %s for %s", n.config.allowedNets, n.config.allowedRanges, n.ifaceName)

	// Verify expected chains "CONTAINERS" and "CONTAINER-REJECT" exist
	for _, chainName := range []string{containersChainName, containerRejectChainName} {
		if !chainExists(chainName) {
			return fmt.Errorf("Expected iptables chain not found: %s", chainName)
		}
	}

	rules := new(iptablesRules)
	rules.addRule("-N", vethChainName) // create veth specific chain

	// Allow specified nets and ranges only
	for _, ipNet := range n.config.allowedNets {
		rules.addRule("-A", vethChainName, "-s", ipNet.String(), "-j", "ACCEPT")
	}
	for _, ipRange := range n.config.allowedRanges {
		rules.addRule("-A", vethChainName, "-m", "iprange", "--src-range", ipRange.String(), "-j", "ACCEPT")
	}

	rules.addRule("-A", vethChainName, "-j", "CONTAINER-REJECT")

	// Add JUMP in CONTAINERS, send all traffic going to the veth interface
	rules.addRule("-I", containersChainName, "1", "-o", n.ifaceName, "-j", vethChainName)

	if err := rules.apply(); err != nil {
		return err
	}

	log.Info("NetFilter: Successfully applied ingress filtering")
	return nil
}

func (n *netFilter) removeFiltering() error {
	if n.config == nil {
		return nil
	}

	log.Debugf("NetFilter. Removing rules for %s", n.ifaceName)

	vethChainName := vethChainPrefix + n.ifaceName

	rules := new(iptablesRules)
	rules.addRule("-D", containersChainName, "-o", n.ifaceName, "-j", vethChainName)
	rules.addRule("-F", vethChainName)
	rules.addRule("-X", vethChainName)
	return rules.apply()
}

type iptablesRules struct {
	rules [][]string
}

func (ipRules *iptablesRules) addRule(args ...string) {
	ipRules.rules = append(ipRules.rules, args)
}

func (ipRules *iptablesRules) apply() error {
	for _, rule := range ipRules.rules {
		if err := applyIpTablesRule(rule...); err != nil {
			return err
		}
	}
	return nil
}

func applyIpTablesRule(args ...string) error {
	log.Debugf("NetFilter. IpTables call %s", args)
	if output, err := iptables.Raw(args...); err != nil {
		return fmt.Errorf("NetFilter. IP tables apply rule failed %s %s %v", args, output, err)
	}
	return nil
}
