package main

import (
	"sync"

	log "github.com/Sirupsen/logrus"
	ipamApi "github.com/docker/go-plugins-helpers/ipam"
	netApi "github.com/docker/go-plugins-helpers/network"
	"github.com/medallia/docker-routed-plugin/routed"
)

const (
	version = "0.1"
)

func main() {
	// TODO: PARSE COMMAND LINE ARGS!
	log.SetLevel(log.DebugLevel)

	messages := make(chan int)
	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		id, err := routed.NewIpamDriver(version)
		if err != nil {
			panic(err)
		}

		log.Debugf("Startig routed ipam driver: %+v", id)
		ih := ipamApi.NewHandler(id)
		ih.ServeUnix("root", "ipam-routed")

		messages <- 1
	}()

	go func() {
		defer wg.Done()

		nd, err := routed.NewNetDriver(version)
		if err != nil {
			panic(err)
		}

		log.Debugf("Starting routed network driver: %+v", nd)
		nh := netApi.NewHandler(nd)
		nh.ServeUnix("root", "net-routed")

		messages <- 2
	}()

	wg.Wait()
}
