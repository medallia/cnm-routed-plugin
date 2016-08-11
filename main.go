package main

import (
	log "github.com/Sirupsen/logrus"
	netapi "github.com/docker/go-plugins-helpers/network"
	"github.com/medallia/docker-routed-plugin/routed"
)

const (
	version = "0.1"
)

func main() {
	log.SetLevel(log.DebugLevel)

	d, err := routed.NewDriver(version)
	if err != nil {
		panic(err)
	}

	log.Debugf("Driver created %+v", d)
	h := netapi.NewHandler(d)
	h.ServeUnix("root", "routed")
}
