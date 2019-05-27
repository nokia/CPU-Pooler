package main

import (
	"flag"
	"github.com/nokia/CPU-Pooler/pkg/sethandler"
	"github.com/nokia/CPU-Pooler/pkg/types"
	"log"
	"os"
	"os/signal"
	"syscall"
)

var (
	kubeConfig     string
	poolConfigPath string
	cpusetRoot     string
)

func main() {
	flag.Parse()
	if poolConfigPath == "" || cpusetRoot == "" {
		log.Fatal("ERROR: Mandatory command-line arguments poolconfigs and cpusetroot were not provided!")
	}
	poolConf, _, err := types.DeterminePoolConfig()
	if err != nil {
		log.Fatal("ERROR: Could not read CPU pool configuration files because: " + err.Error() + ", exiting!")
	}
	setHandler, err := sethandler.New(kubeConfig, poolConf, cpusetRoot)
	if err != nil {
		log.Fatal("ERROR: Could not initalize K8s client because of error: " + err.Error() + ", exiting!")
	}
	controller := setHandler.CreateController()
	stopChannel := make(chan struct{})
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)
	log.Println("CPUSetter's Controller initalized successfully! Warm-up starts now!")
	go controller.Run(stopChannel)
	// Wait until Controller pushes a signal on the stop channel
	select {
	case <-stopChannel:
		log.Fatal("CPUSetter's Controller stopped abruptly, exiting!")
	case <-signalChannel:
		log.Println("Orchestrator initiated graceful shutdown. See you soon!")
	}
}

func init() {
	flag.StringVar(&poolConfigPath, "poolconfigs", "", "Path to the pool configuration files. Mandatory parameter.")
	flag.StringVar(&cpusetRoot, "cpusetroot", "", "The root of the cgroupfs where Kubernetes creates the cpusets for the Pods . Mandatory parameter.")
	flag.StringVar(&kubeConfig, "kubeconfig", "", "Path to a kubeconfig. Optional parameter, only required if out-of-cluster.")
}
