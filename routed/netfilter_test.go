package routed

import (
	"fmt"
	"net"
	"testing"
)

// Verify IPRange parsing works as expected
func TestParseIPRange(t *testing.T) {
	var testRangeStr string

	testRangeStr = "1.1.1.1-2.2.2.2"
	if ipRange := ParseIPRange(testRangeStr); ipRange == nil {
		t.Errorf("IPRange %s could not be parsed", testRangeStr)
	} else {
		if !ipRange.from.Equal(net.ParseIP("1.1.1.1")) || !ipRange.to.Equal(net.ParseIP("2.2.2.2")) {
			t.Errorf("Wrong IPRange from/to %s %s", ipRange.from, ipRange.to)
		}
		if toString := ipRange.String(); toString != testRangeStr {
			t.Errorf("Wrong String() result %s", toString)
		}
	}

	testRangeStr = "1.1.1.1-2"
	if ipRange := ParseIPRange(testRangeStr); ipRange != nil {
		t.Errorf("IPRange %s should be invalid", testRangeStr)
	}

	testRangeStr = "1.1.1.1-2.2.2.2/24"
	if ipRange := ParseIPRange(testRangeStr); ipRange != nil {
		t.Errorf("IPRange %s should be invalid", testRangeStr)
	}
}

func TestParseIPOrNet(t *testing.T) {
	verifyString(t, "1.1.1.1/32", ParseIpOrNet("1.1.1.1"))
	verifyString(t, "1.1.0.0/16", ParseIpOrNet("1.1.1.0/16"))

	invalidIps := []string{"1.1.1.1.1", "1.1.1.1/24/24", "257.1.1.1", "1.1.1.1/33"}
	for _, testIP := range invalidIps {
		if ipOrNet := ParseIpOrNet(testIP); ipOrNet != nil {
			t.Errorf("IP or NET should not be valid %v", testIP)
		}
	}
}

func TestNetFilterConfigParse(t *testing.T) {
	verifyNetFilterConfig(t, "1.1.1.1,3.3.3.3-4.4.4.4,2.2.2.2/16")
	verifyNetFilterConfig(t, "1.1.1.1, 3.3.3.3 -	4.4.4.4,  2.2.2.25/16  ")
}

func verifyNetFilterConfig(t *testing.T, configString string) {
	if config, error := NetFilterConfigParse(configString); error != nil {
		t.Errorf("Config '%s' should valid %v", configString, error)
	} else {
		verifyString(t, "1.1.1.1/32", config.allowedNets[0])
		verifyString(t, "2.2.0.0/16", config.allowedNets[1])
		verifyString(t, "3.3.3.3-4.4.4.4", config.allowedRanges[0])
	}
}

func verifyString(t *testing.T, expected string, actual fmt.Stringer) {
	if ipNet := actual.String(); ipNet != expected {
		t.Errorf("Expected %v, got %v", expected, ipNet)
	}
}