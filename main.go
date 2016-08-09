package main

import (
  "os"
  "fmt" 
	log "github.com/Sirupsen/logrus"
  netapi "github.com/docker/go-plugins-helpers/network"
	"github.com/medallia/docker-routed-plugin/routed"
)

const (
	version = "0.1"
)

func main() {

  f, err := os.OpenFile("test.log", os.O_APPEND | os.O_CREATE | os.O_RDWR, 0666)
  if err != nil {
      fmt.Printf("error opening file: %v", err)
  }

  defer f.Close()

  log.SetOutput(f)

  log.SetLevel(log.DebugLevel)

	d, err := routed.NewDriver(version)
	if err != nil {
		panic(err)
	}

  h := netapi.NewHandler(d)
  h.ServeUnix("root", "routed")
}
