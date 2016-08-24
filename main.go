package main

import (
	"fmt"
	"os"
	"sync"

	log "github.com/Sirupsen/logrus"
	ipamApi "github.com/docker/go-plugins-helpers/ipam"
	netApi "github.com/docker/go-plugins-helpers/network"
	"github.com/medallia/docker-routed-plugin/routed"
	"github.com/urfave/cli"
	"github.com/vishvananda/netlink"
)

const (
	version = "0.1"
)

func main() {

	debug := cli.BoolFlag{
		Name:  "debug, d",
		Usage: "set debugging",
	}

	ipamSocket := cli.StringFlag{
		Name:  "ipamsock, s",
		Value: "ipam-routed",
		Usage: "set ipam plugin socket name",
	}

	netSocket := cli.StringFlag{
		Name:  "netsock, S",
		Value: "net-routed",
		Usage: "set network plugin socket name",
	}

	gateway := cli.StringFlag{
		Name:  "gateway, g",
		Value: "gateway",
		Usage: "IP to configure as default gateway for containers",
	}

	app := cli.NewApp()
	app.Name = "routed"
	app.Usage = "Docker routed network driver"
	app.UsageText = "docker run -ti --privileged --net=host --rm -v /run/docker/plugins:/run/docker/plugins ${IMAGETAG} --debug"
	app.Version = version

	app.Flags = []cli.Flag{
		debug,
		ipamSocket,
		netSocket,
		gateway,
	}

	app.Action = driverRun
	app.Run(os.Args)
}

func driverRun(c *cli.Context) {

	if c.Bool("debug") {
		log.SetLevel(log.DebugLevel)
	}

	gateway := c.String("gateway")
	_, err := netlink.ParseAddr(fmt.Sprintf("%s/32", gateway))
	if err != nil {
		panic(err)
	}

	messages := make(chan int)
	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		id, err := routed.NewIpamDriver(version, gateway)
		if err != nil {
			panic(err)
		}

		log.Debugf("Startig routed ipam driver: %+v", id)
		ih := ipamApi.NewHandler(id)
		ih.ServeUnix("root", c.String("ipamsock"))

		messages <- 1
	}()

	go func() {
		defer wg.Done()

		nd, err := routed.NewNetDriver(version, gateway)
		if err != nil {
			panic(err)
		}

		log.Debugf("Starting routed network driver: %+v", nd)
		nh := netApi.NewHandler(nd)
		nh.ServeUnix("root", c.String("netsock"))

		messages <- 2
	}()

	wg.Wait()
}
