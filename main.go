package main

import (
	"flag"
	"log"
	"os"
	"strconv"

	"github.com/urfave/cli/v2"
	"k8s.io/klog/v2"

	"github-actions-exporter/pkg/config"
	"github-actions-exporter/pkg/server"
)

var (
	version = "development"
)

func main() {
	// setup klog
	before := func(c *cli.Context) error {
		fs := flag.NewFlagSet("", flag.ExitOnError)
		klog.InitFlags(fs)
		return fs.Set("v", strconv.Itoa(c.Int("loglevel")))
	}

	app := cli.NewApp()
	app.Name = "github-actions-exporter"
	app.Flags = config.InitConfiguration()
	app.Before = before
	app.Version = version
	app.Action = server.RunServer

	err := app.Run(os.Args)
	if err != nil {
		klog.V(1).Infof("Error: %s", err.Error())
		log.Fatal(err)
	}
}
