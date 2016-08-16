package main

import (
	"os"
	"sync"

	log "github.com/Sirupsen/logrus"
	ipamApi "github.com/docker/go-plugins-helpers/ipam"
	netApi "github.com/docker/go-plugins-helpers/network"
	"github.com/medallia/docker-routed-plugin/routed"
	"github.com/urfave/cli"
)

const (
	version = "0.1"
)

func main() {

	var debug = cli.BoolFlag{
		Name:  "debug, d",
		Usage: "set debugging",
	}

	var ipamSocket = cli.StringFlag{
		Name:  "ipamsock, s",
		Value: "ipam-routed",
		Usage: "set ipam plugin socket name",
	}

	var netSocket = cli.StringFlag{
		Name:  "netsock, S",
		Value: "net-routed",
		Usage: "set network plugin socket name",
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
	}

	app.Action = DriverRun
	app.Run(os.Args)
}

func DriverRun(c *cli.Context) {

	if c.Bool("debug") {
		log.SetLevel(log.DebugLevel)
	}

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
		ih.ServeUnix("root", c.String("ipamsock"))

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
		nh.ServeUnix("root", c.String("netsock"))

		messages <- 2
	}()

	wg.Wait()
}
