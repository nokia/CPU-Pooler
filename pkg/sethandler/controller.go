package sethandler

import (
  "encoding/json"
  "errors"
  "log"
  "reflect"
  "strconv"
  "strings"
  "time"
  origType "github.com/Levovar/CPU-Pooler/internal/types"
  "github.com/Levovar/CPU-Pooler/pkg/types"
  "github.com/intel/multus-cni/checkpoint"
  "k8s.io/api/core/v1"
  "k8s.io/client-go/informers"
  "k8s.io/client-go/kubernetes"
  "k8s.io/client-go/tools/cache"
)

var (
  dedicatedPinnerCoreId = 0
  processConfigKey = "nokia.k8s.io/cpus"
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
    cpuset,err := setHandler.determineCorrectCpuset(pod,container)
    if err!=nil {
      log.Println("ERROR: Cpuset for the containers of Pod:" + string(pod.ObjectMeta.UID) + " could not be re-adjusted, because:" + err.Error())
      continue
    }
    containerId := determineCid(pod.Status,container.Name)
    setHandler.applyCpusetToContainer(containerId,cpuset)
  }
  return
}

func (setHandler *SetHandler) determineCorrectCpuset(pod v1.Pod, container v1.Container) ([]int,error) {
  for resourceName, _ := range container.Resources.Requests {
    resNameAsString := string(resourceName)
    if strings.Contains(resNameAsString, setHandler.poolConfig.DeviceBaseName) && strings.Contains(resNameAsString, "shared") {
      return setHandler.poolConfig.Shared,nil
    } else if strings.Contains(resNameAsString, setHandler.poolConfig.DeviceBaseName) && strings.Contains(resNameAsString, "exclusive") {
      return setHandler.getListOfAllocatedExclusiveCpus(resNameAsString,pod,container)
    }
  }
  return setHandler.poolConfig.Default,nil
}

func (setHandler *SetHandler) getListOfAllocatedExclusiveCpus(exclusivePoolName string, pod v1.Pod, container v1.Container) ([]int,error) {
  checkpoint, err := checkpoint.GetCheckpoint()
  if err != nil {
    return nil, errors.New("Kubelet checkpoint file could not be accessed because:"+err.Error())
  }
  podIdStr := string(pod.ObjectMeta.UID)
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
  var doesSetContainPinnerCore bool
  for _, deviceId := range exclusiveCpus.DeviceIDs {
    idAsInt, err := strconv.Atoi(deviceId)
    if err!=nil {
      return nil, errors.New("Device ID: " + deviceId + " for Container: " + container.Name + " in Pod: " + podIdStr + " is invalid")
    }
    finalCpuSet = append(finalCpuSet, idAsInt)
    if idAsInt == dedicatedPinnerCoreId {
      doesSetContainPinnerCore = true
    }
  }
  if isPinnerUsedByContainer(pod,container.Name) && !doesSetContainPinnerCore {
    finalCpuSet = append(finalCpuSet, dedicatedPinnerCoreId)
  }
  return finalCpuSet,nil
}

func determineCid(podStatus v1.PodStatus, containerName string) string {
  for _,containerStatus := range podStatus.ContainerStatuses {
    if containerStatus.Name == containerName {
      return containerStatus.ContainerID
    } 
  }
  return ""
}

func isPinnerUsedByContainer(pod v1.Pod,containerName string) bool {
  for key, value := range pod.ObjectMeta.Annotations {
    if strings.Contains(key,processConfigKey) {
      var processConfig origType.CPUAnnotation
      err := json.Unmarshal([]byte(value), &processConfig)
      if err != nil {
        return false
      }
      containerConfig := processConfig.ContainerPools(containerName)
      //1: Container is asking exclusive CPUs + 2: Container has a pool configured in the annotation = Pinner will be used
      if len(containerConfig) > 0 {
        return true
      }
    }
  }
  return false
}

func (setHandler *SetHandler) applyCpusetToContainer(containerId string, cpuset []int) {
  if cpuset == nil {
    //Nothing to set. We will leave the container running on the Kubernetes default cpuset
    return
  }
  return
}