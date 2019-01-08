package main

import (
  "flag"
  "log"
  "os"
  "syscall"
  "github.com/Levovar/CPU-Pooler/pkg/sethandler"
  "github.com/Levovar/CPU-Pooler/pkg/types"
  "os/signal"
  "k8s.io/client-go/tools/clientcmd"
  "k8s.io/client-go/kubernetes"
)

var (
  kubeConfig string
  poolConfigPath string
  cpusetRoot string
)

func main() {
  flag.Parse()
  if poolConfigPath == "" || cpusetRoot == "" {
    log.Fatal("ERROR: Mandatory command-line arguments poolconfigs and cpusetroot were not provided!")    
  }
  poolConfigs, err := parsePoolConfigFiles()
  if err != nil {
    log.Fatal("ERROR: Could not read CPU pool configuration files, exiting!")
  }
  cfg, err := clientcmd.BuildConfigFromFlags("", kubeConfig)
  if err != nil {
    log.Fatal("ERROR: Could not read cluster kubeconfig, exiting!")
  }
  kubeClient, err := kubernetes.NewForConfig(cfg)
  if err != nil {
    log.Fatal("ERROR: Could not initalize K8s client because of error: "+ err.Error() +", exiting!")
  }
  setHandler := sethandler.New(kubeClient,poolConfigs,cpusetRoot)
  controller := setHandler.CreateController()
  stopChannel := make(chan struct{})
  signalChannel := make(chan os.Signal, 1)
  signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)
  go controller.Run(stopChannel)
  // Wait until Controller pushes a signal on the stop channel
  select {
    case <-stopChannel:
      log.Fatal("CCPUSetter's Controller stopped abruptly, exiting!")
    case <-signalChannel:
      log.Println("Orchestrator initiated graceful shutdown. See you soon!")
      os.Exit(0)
  }
}

func init() {
  flag.StringVar(&poolConfigPath, "poolconfigs", "", "Path to the pool configuration files. Mandatory parameter.")
  flag.StringVar(&cpusetRoot, "cpusetroot", "", "The root of the cgroupfs where Kubernetes creates the cpusets for the Pods . Mandatory parameter.")
  flag.StringVar(&kubeConfig, "kubeconfig", "", "Path to a kubeconfig. Optional parameter, only required if out-of-cluster.")
}

//TODO
func parsePoolConfigFiles() (types.PoolConfig,error) {
  poolConfig := types.PoolConfig{}
  return poolConfig,nil
}