package sethandler

import (
  "errors"
  "log"
  "reflect"
  "strconv"
  "strings"
  "time"
  "github.com/Levovar/CPU-Pooler/pkg/types"
  "github.com/intel/multus-cni/checkpoint"
  "k8s.io/api/core/v1"
  k8stypes "k8s.io/apimachinery/pkg/types"
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
    cpuset,err := setHandler.determineCorrectCpuset(pod.ObjectMeta.UID,container)
    if err!=nil {
      log.Println("ERROR: Cpuset for the containers of Pod:" + string(pod.ObjectMeta.UID) + " could not be re-adjusted, because:" + err.Error())
      return
    }
    applyCpuset(cpuset)      
  }
  return
}

func (setHandler *SetHandler) determineCorrectCpuset(podUuid k8stypes.UID, container v1.Container) ([]int,error) {
  for resourceName, _ := range container.Resources.Requests {
    resNameAsString := string(resourceName)
    if strings.Contains(resNameAsString, setHandler.poolConfig.DeviceBaseName) && strings.Contains(resNameAsString, "shared") {
      return setHandler.poolConfig.Shared,nil
    } else if strings.Contains(resNameAsString, setHandler.poolConfig.DeviceBaseName) && strings.Contains(resNameAsString, "exclusive") {
      return setHandler.getListOfAllocatedExclusiveCpus(resNameAsString,podUuid,container)
    }
  }
  return setHandler.poolConfig.Default,nil
}

func (setHandler *SetHandler) getListOfAllocatedExclusiveCpus(exclusivePoolName string, podUuid k8stypes.UID, container v1.Container) ([]int,error) {
  checkpoint, err := checkpoint.GetCheckpoint()
  if err != nil {
    return nil, errors.New("Kubelet checkpoint file could not be accessed because:"+err.Error())
  }
  podIdStr := string(podUuid)
  devices, err := checkpoint.GetComputeDeviceMap(podIdStr)
  if err != nil {
    return nil, errors.New("List of assigned devices could not be read from checkpoint file for Pod: "+ podIdStr +" because:"+err.Error())
  }
  exclusiveCpus, exist := devices[exclusivePoolName]
  if !exist {
    log.Println("WARNING: Container: " + container.Name + " in Pod: " + podIdStr + " asked for exclusive CPUs, but were not allocated any! Cannot adjust its default cpuset")
    return nil,nil
  }
  var finalCpuSet []int
  for _, deviceId := range exclusiveCpus.DeviceIDs {
    idAsInt, err := strconv.Atoi(deviceId)
    if err!=nil {
      return nil, errors.New("Device ID: " + deviceId + " for Container: " + container.Name + " in Pod: " + podIdStr + " is invalid")
    }
    finalCpuSet = append(finalCpuSet, idAsInt)
  } 
  return finalCpuSet,nil
}

func applyCpuset(cpuset []int) {
  if cpuset == nil {
    //Nothing to set. We will leave the container running on the Kubernetes default cpuset
    return
  }
  return
}