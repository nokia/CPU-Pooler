package main

import (
  "flag"
  "log"
  "os"
  "syscall"
  "github.com/Levovar/CPU-Pooler/pkg/sethandler"
  "os/signal"
  "k8s.io/client-go/tools/clientcmd"
  "k8s.io/client-go/kubernetes"
)

var (
  kubeconfig string
)

func main() {
  flag.Parse()
  cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
  if err != nil {
    log.Fatal("ERROR: Could not read cluster kubeconfig, exiting!")
  }
  kubeClient, err := kubernetes.NewForConfig(cfg)
  if err != nil {
    log.Fatal("ERROR: Could not initalize K8s client because of error: "+ err.Error() +", exiting!")
  }
  controller := sethandler.NewController(kubeClient)
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