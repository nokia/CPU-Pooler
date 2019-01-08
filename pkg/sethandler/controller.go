package sethandler

import (
  "reflect"
  "strings"
  "time"
  "github.com/Levovar/CPU-Pooler/pkg/types"
  "k8s.io/api/core/v1"
  "k8s.io/client-go/informers"
  "k8s.io/client-go/kubernetes"
  "k8s.io/client-go/tools/cache"
)

type SetHandler struct {
  poolConfig types.PoolConfig
  cpusetRoot string
  k8sClient kubernetes.Interface
}

func New(k8sClient kubernetes.Interface, poolConfig types.PoolConfig, cpusetRoot string) *SetHandler {
  setHandler := SetHandler {
    poolConfig: poolConfig,
    cpusetRoot: cpusetRoot,
    k8sClient: k8sClient,
  }
  return &setHandler
}

func (setHandler *SetHandler) CreateController() cache.Controller {
  kubeInformerFactory := informers.NewSharedInformerFactory(setHandler.k8sClient, time.Second*30)
  controller := kubeInformerFactory.Core().V1().Pods().Informer()
  controller.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc:  func(obj interface{}) {setHandler.podAdded(*(reflect.ValueOf(obj).Interface().(*v1.Pod)))},
    DeleteFunc: func(obj interface{}) {},
    UpdateFunc: func(oldObj, newObj interface{}) {},
  })
  return controller
}

func (setHandler *SetHandler) podAdded(pod v1.Pod) {
  for _, container := range pod.Spec.Containers {
    cpuset := determineCorrectCpuset(setHandler.poolConfig,container)
    if cpuset != nil {
      applyCpuset(cpuset)
    }        
  }
  return
}

func determineCorrectCpuset(poolConfig types.PoolConfig, container v1.Container) []int {
  for resourceName, _ := range container.Resources.Requests {
    resNameAsString := string(resourceName)
    if strings.Contains(resNameAsString, poolConfig.DeviceBaseName) && strings.Contains(resNameAsString, "shared") {
      return poolConfig.Shared
    } else if strings.Contains(resNameAsString, poolConfig.DeviceBaseName) && strings.Contains(resNameAsString, "exclusive") {
      return getListOfAllocatedExclusiveCpus(container)
    }
  }
  return poolConfig.Default
}

func getListOfAllocatedExclusiveCpus(container v1.Container) []int {
  return nil
}

func applyCpuset(cpuset []int) {
  return
}