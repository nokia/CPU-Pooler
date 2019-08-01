package sethandler

import (
	"encoding/json"
	"errors"
	"strconv"
	"github.com/intel/multus-cni/checkpoint"
	multusTypes "github.com/intel/multus-cni/types"
	"github.com/nokia/CPU-Pooler/pkg/types"
	"k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

var (
	dedicatedPinnerCoreID = 0
	resourceBaseName      = "nokia.k8s.io"
	processConfigKey      = resourceBaseName + "/cpus"
)

//SetHandler is the data set encapsulating the configuration data needed for the CPUSetter Controller to be able to adjust cpusets
type SetHandler struct {
	poolConfig types.PoolConfig
	cpusetRoot string
	k8sClient  kubernetes.Interface
}

//New creates a new SetHandler object
//Can return error if in-cluster K8s API server client could not be initialized
func New(kubeConf string, poolConfig types.PoolConfig, cpusetRoot string) (*SetHandler, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeConf)
	if err != nil {
		return nil, err
	}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	setHandler := SetHandler{
		poolConfig: poolConfig,
		cpusetRoot: cpusetRoot,
		k8sClient:  kubeClient,
	}
	return &setHandler, nil
}

//CreateController takes the K8s client from the SetHandler object, and uses it to create a single thread K8s Controller
//The Controller registers eventhandlers for ADD and UPDATE operations happening in the core/v1/Pod API
func (setHandler *SetHandler) CreateController() cache.Controller {
	kubeInformerFactory := informers.NewSharedInformerFactory(setHandler.k8sClient, time.Second*30)
	controller := kubeInformerFactory.Core().V1().Pods().Informer()
	controller.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { setHandler.podAdded(*(reflect.ValueOf(obj).Interface().(*v1.Pod))) },
		DeleteFunc: func(obj interface{}) {},
		UpdateFunc: func(oldObj, newObj interface{}) {
			setHandler.podChanged(*(reflect.ValueOf(oldObj).Interface().(*v1.Pod)), *(reflect.ValueOf(newObj).Interface().(*v1.Pod)))
		},
	})
	return controller
}

func (setHandler *SetHandler) podAdded(pod v1.Pod) {
	//The maze wasn't meant for you
	if !shouldPodBeHandled(pod) {
		return
	}
	setHandler.adjustContainerSets(pod)
}

func (setHandler *SetHandler) podChanged(oldPod, newPod v1.Pod) {
	//The maze wasn't meant for you either
	if !shouldPodBeHandled(newPod) {
		return
	}
	setHandler.adjustContainerSets(newPod)
}

func shouldPodBeHandled(pod v1.Pod) bool {
	setterNodeName := os.Getenv("NODE_NAME")
	podNodeName := pod.Spec.NodeName
	//Pod is not yet scheduled, or it wasn't scheduled to the Node of this specific CPUSetter instance
	if setterNodeName == "" || podNodeName == "" || setterNodeName != podNodeName {
		return false
	}
	//If the Pod is not running, its containers haven't been created yet - no cpuset cgroup is present to be adjusted by the CPUSetter
	if pod.Status.Phase != "Running" {
		return false
	}
	return true
}

func (setHandler *SetHandler) adjustContainerSets(pod v1.Pod) {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if !containerStatus.Ready {
			return
		}
	}
	var pathToContainerCpusetFile string
	for _, container := range pod.Spec.Containers {
		cpuset, err := setHandler.determineCorrectCpuset(pod, container)
		if err != nil {
			log.Println("ERROR: Cpuset for the containers of Pod:" + string(pod.ObjectMeta.UID) + " could not be re-adjusted, because:" + err.Error())
			continue
		}
		containerID := determineCid(pod.Status, container.Name)
		pathToContainerCpusetFile, err = setHandler.applyCpusetToContainer(containerID, cpuset)
		if err != nil {
			log.Println("ERROR: Cpuset for the containers of Pod:" + string(pod.ObjectMeta.UID) + " could not be re-adjusted, because:" + err.Error())
			continue
		}
	}
	err := setHandler.applyCpusetToInfraContainer(pod, pathToContainerCpusetFile)
	if err != nil {
		log.Println("ERROR: Cpuset for the infracontainer of Pod:" + string(pod.ObjectMeta.UID) + " could not be re-adjusted, because:" + err.Error())
	}
}

func (setHandler *SetHandler) determineCorrectCpuset(pod v1.Pod, container v1.Container) (cpuset.CPUSet, error) {
	for resourceName := range container.Resources.Requests {
		resNameAsString := string(resourceName)
		if strings.Contains(resNameAsString, resourceBaseName) && strings.Contains(resNameAsString, types.SharedPoolID) {
			return setHandler.poolConfig.SelectPool(types.SharedPoolID).CPUs, nil
		} else if strings.Contains(resNameAsString, resourceBaseName) && strings.Contains(resNameAsString, types.ExclusivePoolID) {
			return setHandler.getListOfAllocatedExclusiveCpus(resNameAsString, pod, container)
		}
	}
	return setHandler.poolConfig.SelectPool(types.DefaultPoolID).CPUs, nil
}

func (setHandler *SetHandler) getListOfAllocatedExclusiveCpus(exclusivePoolName string, pod v1.Pod, container v1.Container) (cpuset.CPUSet, error) {
	checkpoint, err := checkpoint.GetCheckpoint()
	if err != nil {
		return cpuset.CPUSet{}, errors.New("Kubelet checkpoint file could not be accessed because:" + err.Error())
	}
	podIDStr := string(pod.ObjectMeta.UID)
	devices, err := checkpoint.GetComputeDeviceMap(podIDStr)
	if err != nil {
		return cpuset.CPUSet{}, errors.New("List of assigned devices could not be read from checkpoint file for Pod: " + podIDStr + " because:" + err.Error())
	}
	exclusiveCpus, exist := devices[exclusivePoolName]
	if !exist {
		log.Println("WARNING: Container: " + container.Name + " in Pod: " + podIDStr + " asked for exclusive CPUs, but were not allocated any! Cannot adjust its default cpuset")
		return cpuset.CPUSet{}, nil
	}
	return calculateFinalExclusiveSet(exclusiveCpus, pod, container)
}

func calculateFinalExclusiveSet(exclusiveCpus *multusTypes.ResourceInfo, pod v1.Pod, container v1.Container) (cpuset.CPUSet, error) {
	var doesSetContainPinnerCore bool
	setBuilder := cpuset.NewBuilder()
	for _, deviceID := range exclusiveCpus.DeviceIDs {
		deviceIDasInt,err := strconv.Atoi(deviceID)
		if err != nil {
			return cpuset.CPUSet{}, err
		}
		setBuilder.Add(deviceIDasInt)
		if deviceIDasInt == dedicatedPinnerCoreID {
			doesSetContainPinnerCore = true
		}
	}
	if isPinnerUsedByContainer(pod, container.Name) && !doesSetContainPinnerCore {
		//1: Container is asking exclusive CPUs + 2: Container has a pool configured in the annotation = Pinner will be used
		//If pinner is used by a container, we need to the add the configured CPU core holding its thread to their cpuset
		setBuilder.Add(dedicatedPinnerCoreID)
	}
	return setBuilder.Result(), nil
}

func isPinnerUsedByContainer(pod v1.Pod, containerName string) bool {
	for key, value := range pod.ObjectMeta.Annotations {
		if strings.Contains(key, processConfigKey) {
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
	for _, containerStatus := range podStatus.ContainerStatuses {
		if containerStatus.Name == containerName {
			return containerStatus.ContainerID
		}
	}
	return ""
}

func containerIDInPodStatus(podStatus v1.PodStatus, containerID string) bool {
	for _, containerStatus := range podStatus.ContainerStatuses {
		if strings.HasSuffix(containerStatus.ContainerID, containerID){
			return true
		}
	}
	return false
}

func (setHandler *SetHandler) applyCpusetToContainer(containerID string, cpuset cpuset.CPUSet) (string, error) {
	if cpuset.IsEmpty() {
		//Nothing to set. We will leave the container running on the Kubernetes provisioned default cpuset
		log.Println("WARNING: for some reason cpuset to set was quite empty for container:" + containerID + ".I left it untouched.")
		return "", nil
	}
	//According to K8s documentation CID is stored in "docker://<container_id>" format
	trimmedCid := strings.TrimPrefix(containerID, "docker://")
	var pathToContainerCpusetFile string
	filepath.Walk(setHandler.cpusetRoot, func(path string, f os.FileInfo, err error) error {
		if strings.HasSuffix(path, trimmedCid) {
			pathToContainerCpusetFile = path
		}
		return nil
	})
	if pathToContainerCpusetFile == "" {
		return "", errors.New("cpuset file does not exist for container:" + trimmedCid + " under the provided cgroupfs hierarchy:" + setHandler.cpusetRoot)
	}
	returnContainerPath := pathToContainerCpusetFile
	//And for our grand finale, we just "echo" the calculated cpuset to the cpuset cgroupfs "file" of the given container
  //Find child cpuset if it exists (kube-proxy)
	filepath.Walk(pathToContainerCpusetFile, func(path string, f os.FileInfo, err error) error {
		if f.IsDir() {
			pathToContainerCpusetFile = path
		}
		return nil
	})
	file, err := os.OpenFile(pathToContainerCpusetFile + "/cpuset.cpus", os.O_WRONLY|os.O_SYNC, 0755)
	if err != nil {
		return "", errors.New("Can't open cpuset file:" + pathToContainerCpusetFile + " for container:" + trimmedCid + " because:" + err.Error())
	}
	defer file.Close()
	_, err = file.WriteString(cpuset.String())
	if err != nil {
		return "", errors.New("Can't modify cpuset file:" + pathToContainerCpusetFile + " for container:" + trimmedCid + " because:" + err.Error())
	}
	return returnContainerPath, nil
}

func getInfraContainerPath(podStatus v1.PodStatus, searchPath string) string {
	var pathToInfraContainer string
	filelist, _ := filepath.Glob(filepath.Dir(searchPath) + "/*")
	for _, fpath := range filelist{
		fstat, err := os.Stat(fpath)
		if err != nil {
			continue
		}
		if fstat.IsDir() && !containerIDInPodStatus(podStatus, fstat.Name()) {
			pathToInfraContainer = fpath
		}
	}
	return pathToInfraContainer
}

func (setHandler *SetHandler) applyCpusetToInfraContainer(pod v1.Pod, pathToSearchContainer string ) error{
	cpuset := setHandler.poolConfig.SelectPool(types.DefaultPoolID).CPUs
	if cpuset.IsEmpty() {
		//Nothing to set. We will leave the container running on the Kubernetes provisioned default cpuset
		log.Println("WARNING: for some reason DEFAULT cpuset was quite empty. Cannot adjust cpuset for infra container of pod: " + string(pod.ObjectMeta.UID))
		return nil
	}
	if pathToSearchContainer == "" {
		return errors.New("container directory for pod: " +  string(pod.ObjectMeta.UID) + " does not exists under the provided cgroupfs hierarchy:" + setHandler.cpusetRoot)
	}
	pathToContainerCpusetFile := getInfraContainerPath(pod.Status, pathToSearchContainer)
	if pathToContainerCpusetFile == "" {
		return errors.New("cpuset file does not exist for infra container of pod:" + string(pod.ObjectMeta.UID) + " under the provided cgroupfs hierarchy:" + setHandler.cpusetRoot)
	}

	file, err := os.OpenFile(pathToContainerCpusetFile + "/cpuset.cpus", os.O_WRONLY|os.O_SYNC, 0755)
	if err != nil {
		return errors.New("Can't open cpuset file:" + pathToContainerCpusetFile + " for infra container:" + filepath.Base(pathToContainerCpusetFile) + " because:" + err.Error())
	}
	defer file.Close()
	_, err = file.WriteString(cpuset.String())
	if err != nil {
		return errors.New("Can't modify cpuset file:" + pathToContainerCpusetFile + " for infra container:" + filepath.Base(pathToContainerCpusetFile) + " because:" + err.Error())
	}
	return nil
}
