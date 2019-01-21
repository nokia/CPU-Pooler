package sethandler

import (
  "encoding/json"
  "errors"
  "log"
  "os"
  "reflect"
  "strings"
  "time"
  "github.com/nokia/CPU-Pooler/pkg/types"
  "github.com/intel/multus-cni/checkpoint"
  multusTypes "github.com/intel/multus-cni/types"
  "k8s.io/api/core/v1"
  "k8s.io/client-go/tools/clientcmd"
  "k8s.io/client-go/informers"
  "k8s.io/client-go/kubernetes"
  "k8s.io/client-go/tools/cache"
  "path/filepath"
)

var (
  dedicatedPinnerCoreId = "0"
  resourceBaseName = "nokia.k8s.io"
  processConfigKey = resourceBaseName + "/cpus"
)

type SetHandler struct {
  poolConfig types.PoolConfig
  cpusetRoot string
  k8sClient kubernetes.Interface
}

func New(kubeConf string, poolConfig types.PoolConfig, cpusetRoot string) (*SetHandler,error) {
  cfg, err := clientcmd.BuildConfigFromFlags("", kubeConf)
  if err != nil {
    return nil, err
  }
  kubeClient, err := kubernetes.NewForConfig(cfg)
  if err != nil {
    return nil, err
  }
  setHandler := SetHandler {
    poolConfig: poolConfig,
    cpusetRoot: cpusetRoot,
    k8sClient: kubeClient,
  }
  return &setHandler, nil
}

func (setHandler *SetHandler) CreateController() cache.Controller {
  kubeInformerFactory := informers.NewSharedInformerFactory(setHandler.k8sClient, time.Second*30)
  controller := kubeInformerFactory.Core().V1().Pods().Informer()
  controller.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc:  func(obj interface{}) {setHandler.podAdded(*(reflect.ValueOf(obj).Interface().(*v1.Pod)))},
    DeleteFunc: func(obj interface{}) {},
    UpdateFunc: func(oldObj, newObj interface{}) {setHandler.podChanged(*(reflect.ValueOf(oldObj).Interface().(*v1.Pod)),*(reflect.ValueOf(newObj).Interface().(*v1.Pod)))},
  })
  return controller
}

func (setHandler *SetHandler) podAdded(pod v1.Pod) {
  //The maze wasn't meant for you
  if !shouldPodBeHandled(pod) {
    return
  }
  for _, container := range pod.Spec.Containers {
    cpuset,err := setHandler.determineCorrectCpuset(pod,container)
    if err!=nil {
      log.Println("ERROR: Cpuset for the containers of Pod:" + string(pod.ObjectMeta.UID) + " could not be re-adjusted, because:" + err.Error())
      continue
    }
    containerId := determineCid(pod.Status,container.Name)
    err = setHandler.applyCpusetToContainer(containerId,cpuset)
    if err!=nil {
      log.Println("ERROR: Cpuset for the containers of Pod:" + string(pod.ObjectMeta.UID) + " could not be re-adjusted, because:" + err.Error())
      continue
    }
  }
  return
}

func (setHandler *SetHandler) podChanged(oldPod, newPod v1.Pod) {
  //The maze wasn't meant for you either
  if shouldPodBeHandled(oldPod) || !shouldPodBeHandled(newPod)  {
    return
  }
  for _, container := range newPod.Spec.Containers {
    cpuset,err := setHandler.determineCorrectCpuset(newPod,container)
    if err!=nil {
      log.Println("ERROR: Cpuset for the containers of Pod:" + string(newPod.ObjectMeta.UID) + " could not be re-adjusted, because:" + err.Error())
      continue
    }
    containerId := determineCid(newPod.Status,container.Name)
    err = setHandler.applyCpusetToContainer(containerId,cpuset)
    if err!=nil {
      log.Println("ERROR: Cpuset for the containers of Pod:" + string(newPod.ObjectMeta.UID) + " could not be re-adjusted, because:" + err.Error())
      continue
    }
  }
  return
}

func shouldPodBeHandled(pod v1.Pod) bool {
  setterNodeName := os.Getenv("NODE_NAME")
  podNodeName := pod.Spec.NodeName
  //Pod is not yet scheduled, or it wasn't scheduled to the Node of this specific CPUSetter instance
  if setterNodeName == "" || podNodeName != "" || setterNodeName != podNodeName {
    return false
  }
  //If the Pod is not running, its containers haven't been created yet - no cpuset cgroup is present to be adjusted by the CPUSetter
  if pod.Status.Phase != "Running" {
    return false
  }
  return true
}

func (setHandler *SetHandler) determineCorrectCpuset(pod v1.Pod, container v1.Container) (types.Pool,error) {
  for resourceName, _ := range container.Resources.Requests {
    resNameAsString := string(resourceName)
    if strings.Contains(resNameAsString, resourceBaseName) && strings.Contains(resNameAsString, "shared") {
      return setHandler.poolConfig.SelectPool(resNameAsString),nil
    } else if strings.Contains(resNameAsString, resourceBaseName) && strings.Contains(resNameAsString, "exclusive") {
      return setHandler.getListOfAllocatedExclusiveCpus(resNameAsString,pod,container)
    }
  }
  return setHandler.poolConfig.SelectPool(resourceBaseName + "/default"),nil
}

func (setHandler *SetHandler) getListOfAllocatedExclusiveCpus(exclusivePoolName string, pod v1.Pod, container v1.Container) (types.Pool,error) {
  checkpoint, err := checkpoint.GetCheckpoint()
  if err != nil {
    return types.Pool{}, errors.New("Kubelet checkpoint file could not be accessed because:"+err.Error())
  }
  podIdStr := string(pod.ObjectMeta.UID)
  devices, err := checkpoint.GetComputeDeviceMap(podIdStr)
  if err != nil {
    return types.Pool{}, errors.New("List of assigned devices could not be read from checkpoint file for Pod: "+ podIdStr +" because:"+err.Error())
  }
  exclusiveCpus, exist := devices[exclusivePoolName]
  if !exist {
    log.Println("WARNING: Container: " + container.Name + " in Pod: " + podIdStr + " asked for exclusive CPUs, but were not allocated any! Cannot adjust its default cpuset")
    return types.Pool{},nil
  }
  return calculateFinalExclusiveSet(exclusiveCpus,pod,container)
}

func calculateFinalExclusiveSet(exclusiveCpus *multusTypes.ResourceInfo, pod v1.Pod, container v1.Container) (types.Pool,error) {
  var finalCpuSet types.Pool
  var doesSetContainPinnerCore bool
  for _, deviceId := range exclusiveCpus.DeviceIDs {
    finalCpuSet.CPUs = finalCpuSet.CPUs + deviceId + ","
    if deviceId == dedicatedPinnerCoreId {
      doesSetContainPinnerCore = true
    }
  }
  if isPinnerUsedByContainer(pod,container.Name) && !doesSetContainPinnerCore {
    //1: Container is asking exclusive CPUs + 2: Container has a pool configured in the annotation = Pinner will be used
    //If pinner is used by a container, we need to the add the configured CPU core holding its thread to their cpuset
    finalCpuSet.CPUs = finalCpuSet.CPUs + dedicatedPinnerCoreId + ","
  }
  finalCpuSet.CPUs = strings.TrimSuffix(finalCpuSet.CPUs,",")
  return finalCpuSet,nil
}

func isPinnerUsedByContainer(pod v1.Pod, containerName string) bool {
  for key, value := range pod.ObjectMeta.Annotations {
    if strings.Contains(key,processConfigKey) {
      var processConfig types.CPUAnnotation
      err := json.Unmarshal([]byte(value), &processConfig)
      if err != nil {
        return false
      }
      containerConfig := processConfig.ContainerPools(containerName)
      if len(containerConfig) > 0 {
        return true
      }
    }
  }
  return false
}

func determineCid(podStatus v1.PodStatus, containerName string) string {
  for _,containerStatus := range podStatus.ContainerStatuses {
    if containerStatus.Name == containerName {
      return containerStatus.ContainerID
    } 
  }
  return ""
}

func (setHandler *SetHandler) applyCpusetToContainer(containerId string, cpuset types.Pool) error {
  if cpuset.CPUs == "" {
    //Nothing to set. We will leave the container running on the Kubernetes provisioned default cpuset
    return nil
  }
  //According to K8s documentation CID is stored in "docker://<container_id>" format
  trimmedCid := strings.TrimPrefix(containerId,"docker://")
  var pathToContainerCpusetFile string
  err := filepath.Walk(setHandler.cpusetRoot, func(path string, f os.FileInfo, err error) error {
    if strings.HasSuffix(path,trimmedCid) {
      pathToContainerCpusetFile = path
    }
    return nil
  })
  if pathToContainerCpusetFile == "" {
    return errors.New("cpuset file does not exist for container:" + trimmedCid + " under the provided cgroupfs hierarchy:" + setHandler.cpusetRoot)
  }
  //And for our grand finale, we just "echo" the calculated cpuset to the cpuset cgroupfs "file" of the given container
  file, err := os.OpenFile(pathToContainerCpusetFile, os.O_WRONLY|os.O_SYNC, 0755)
  if err != nil {
    return errors.New("Can't open cpuset file:" + pathToContainerCpusetFile + " for container:" + trimmedCid + " because:" + err.Error())
  }
  defer file.Close()
  _, err = file.WriteString(cpuset.CPUs)
  if err != nil {
    return errors.New("Can't modify cpuset file:" + pathToContainerCpusetFile + " for container:" + trimmedCid + " because:" + err.Error())
  }
  return nil
}