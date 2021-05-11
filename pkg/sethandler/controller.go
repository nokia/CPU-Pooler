package sethandler

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/nokia/CPU-Pooler/pkg/k8sclient"
	"github.com/nokia/CPU-Pooler/pkg/topology"
	"github.com/nokia/CPU-Pooler/pkg/types"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

var (
	resourceBaseName       = "nokia.k8s.io"
	processConfigKey       = resourceBaseName + "/cpus"
	setterAnnotationSuffix = "cpusets-configured"
	setterAnnotationKey    = resourceBaseName + "/" + setterAnnotationSuffix
)

type checkpointPodDevicesEntry struct {
	PodUID        string
	ContainerName string
	ResourceName  string
	DeviceIDs     []string
}

// kubelet checkpoint file structure with only relevant fields
type checkpointFile struct {
	Data struct {
		PodDeviceEntries []checkpointPodDevicesEntry
	}
}

//SetHandler is the data set encapsulating the configuration data needed for the CPUSetter Controller to be able to adjust cpusets
type SetHandler struct {
	poolConfig types.PoolConfig
	cpusetRoot string
	k8sClient  kubernetes.Interface
}

//SetHandler returns the SetHandler data set
func (setHandler SetHandler) SetHandler() SetHandler {
	return setHandler
}

//SetSetHandler a setter for SetHandler
func (setHandler *SetHandler) SetSetHandler(poolconf types.PoolConfig, cpusetRoot string, k8sClient kubernetes.Interface) {
	setHandler.poolConfig = poolconf
	setHandler.cpusetRoot = cpusetRoot
	setHandler.k8sClient = k8sClient
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
		AddFunc:    func(obj interface{}) { setHandler.PodAdded(*(reflect.ValueOf(obj).Interface().(*v1.Pod))) },
		DeleteFunc: func(obj interface{}) {},
		UpdateFunc: func(oldObj, newObj interface{}) {
			setHandler.PodChanged(*(reflect.ValueOf(oldObj).Interface().(*v1.Pod)), *(reflect.ValueOf(newObj).Interface().(*v1.Pod)))
		},
	})
	return controller
}

//PodAdded handles ADD operations
func (setHandler *SetHandler) PodAdded(pod v1.Pod) {
	//The maze wasn't meant for you
	if !shouldPodBeHandled(pod) {
		return
	}
	containersToBeSet := gatherAllContainers(pod)
	if len(containersToBeSet) > 0 {
		setHandler.adjustContainerSets(pod, containersToBeSet)
	}
}

//PodChanged handles UPDATE operations
func (setHandler *SetHandler) PodChanged(oldPod, newPod v1.Pod) {
	//The maze wasn't meant for you either
	if !shouldPodBeHandled(newPod) {
		return
	}
	containersToBeSet := map[string]int{}
	if newPod.ObjectMeta.Annotations[setterAnnotationKey] != "" {
		containersToBeSet = determineContainersToBeSet(oldPod, newPod)
	} else {
		containersToBeSet = gatherAllContainers(newPod)
	}
	if len(containersToBeSet) > 0 {
		setHandler.adjustContainerSets(newPod, containersToBeSet)
	}
}

func shouldPodBeHandled(pod v1.Pod) bool {
	setterNodeName := os.Getenv("NODE_NAME")
	podNodeName := pod.Spec.NodeName
	//Pod is not yet scheduled, or it wasn't scheduled to the Node of this specific CPUSetter instance
	if setterNodeName == "" || podNodeName == "" || setterNodeName != podNodeName {
		return false
	}

	// Pod has exited/completed and all containers have stopped
	if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
		return false
	}

	return true
}

func determineContainersToBeSet(oldPod, newPod v1.Pod) map[string]int {
	workingContainers := map[string]int{}
	found := false
	for _, newContainerStatus := range newPod.Status.ContainerStatuses {
		found = false
		for _, oldContainerStatus := range oldPod.Status.ContainerStatuses {
			if oldContainerStatus.ContainerID == newContainerStatus.ContainerID {
				found = true
				break
			}
		}
		if !found {
			workingContainers[newContainerStatus.Name] = 0
		}
	}
	return workingContainers
}

func gatherAllContainers(pod v1.Pod) map[string]int {
	workingContainers := map[string]int{}
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.ContainerID == "" {
			return map[string]int{}
		}
		workingContainers[containerStatus.Name] = 0
	}
	return workingContainers
}

func (setHandler *SetHandler) adjustContainerSets(pod v1.Pod, containersToBeSet map[string]int) {
	var (
		pathToContainerCpusetFile string
		err error
	)
	for _, container := range pod.Spec.Containers {
		if _, found := containersToBeSet[container.Name]; !found {
			continue
		}
		cpuset, err := setHandler.determineCorrectCpuset(pod, container)
		if err != nil {
			log.Printf("ERROR: Cpuset for the containers of Pod: %s ID: %s could not be re-adjusted, because: %s", pod.ObjectMeta.Name, pod.ObjectMeta.UID, err)
			continue
		}
		containerID := determineCid(pod.Status, container.Name)
		if containerID == "" {
			log.Printf("ERROR: Cannot determine container id for %s from Pod: %s ID: %s", container.Name, pod.ObjectMeta.Name, pod.ObjectMeta.UID)
			return
		}
		pathToContainerCpusetFile, err = setHandler.applyCpusetToContainer(containerID, cpuset)
		if err != nil {
			log.Printf("ERROR: Cpuset for the containers of Pod: %s with ID: %s could not be re-adjusted, because: %s", pod.ObjectMeta.Name, pod.ObjectMeta.UID, err)
			continue
		}
	}
	err = setHandler.applyCpusetToInfraContainer(pod.ObjectMeta, pod.Status, pathToContainerCpusetFile)
	if err != nil {
		log.Printf("ERROR: Cpuset for the infracontainer of Pod: %s with ID: %s could not be re-adjusted, because: %s", pod.ObjectMeta.Name, pod.ObjectMeta.UID, err)
		return
	}
	err = k8sclient.SetPodAnnotation(pod, setterAnnotationKey, "true")
	if err != nil {
		log.Printf("ERROR: %s ID: %s  annotation cannot update, because: %s", pod.ObjectMeta.Name, pod.ObjectMeta.UID, err)
	}
}

func (setHandler *SetHandler) determineCorrectCpuset(pod v1.Pod, container v1.Container) (cpuset.CPUSet, error) {
	var (
		sharedCPUSet, exclusiveCPUSet cpuset.CPUSet
		err                           error
	)
	for resourceName := range container.Resources.Requests {
		resNameAsString := string(resourceName)
		if strings.Contains(resNameAsString, resourceBaseName) && strings.Contains(resNameAsString, types.SharedPoolID) {
			sharedCPUSet = setHandler.poolConfig.SelectPool(types.SharedPoolID).CPUset
		} else if strings.Contains(resNameAsString, resourceBaseName) && strings.Contains(resNameAsString, types.ExclusivePoolID) {
			exclusiveCPUSet, err = setHandler.getListOfAllocatedExclusiveCpus(resNameAsString, pod, container)
			if err != nil {
				return cpuset.CPUSet{}, err
			}
			fullResName := strings.Split(resNameAsString, "/")
			exclusivePoolName := fullResName[1]
			if setHandler.poolConfig.SelectPool(exclusivePoolName).HTPolicy == types.MultiThreadHTPolicy {
				htMap := topology.GetHTTopology()
				exclusiveCPUSet = topology.AddHTSiblingsToCPUSet(exclusiveCPUSet, htMap)
			}
		}
	}
	if !sharedCPUSet.IsEmpty() || !exclusiveCPUSet.IsEmpty() {
		return sharedCPUSet.Union(exclusiveCPUSet), nil
	}
	return setHandler.poolConfig.SelectPool(types.DefaultPoolID).CPUset, nil
}

func (setHandler *SetHandler) getListOfAllocatedExclusiveCpus(exclusivePoolName string, pod v1.Pod, container v1.Container) (cpuset.CPUSet, error) {
	checkpointFileName := "/var/lib/kubelet/device-plugins/kubelet_internal_checkpoint"
	var cp checkpointFile
	buf, err := ioutil.ReadFile(checkpointFileName)
	if err != nil {
		log.Printf("Error reading file %s: Error: %v", checkpointFileName, err)
		return cpuset.CPUSet{}, fmt.Errorf("kubelet checkpoint file could not be accessed because: %s", err)
	}
	if err = json.Unmarshal(buf, &cp); err != nil {
		log.Printf("error unmarshalling kubelet checkpoint file: %s", err)
		return cpuset.CPUSet{}, err
	}
	podIDStr := string(pod.ObjectMeta.UID)
	deviceIDs := []string{}
	for _, entry := range cp.Data.PodDeviceEntries {
		if entry.PodUID == podIDStr && entry.ContainerName == container.Name &&
			entry.ResourceName == exclusivePoolName {
			deviceIDs = append(deviceIDs, entry.DeviceIDs...)
		}
	}
	if len(deviceIDs) == 0 {
		log.Printf("WARNING: Container: %s in Pod: %s asked for exclusive CPUs, but were not allocated any! Cannot adjust its default cpuset", container.Name, podIDStr)
		return cpuset.CPUSet{}, nil
	}
	return calculateFinalExclusiveSet(deviceIDs, pod, container)
}

func calculateFinalExclusiveSet(exclusiveCpus []string, pod v1.Pod, container v1.Container) (cpuset.CPUSet, error) {
	setBuilder := cpuset.NewBuilder()
	for _, deviceID := range exclusiveCpus {
		deviceIDasInt, err := strconv.Atoi(deviceID)
		if err != nil {
			return cpuset.CPUSet{}, err
		}
		setBuilder.Add(deviceIDasInt)
	}
	return setBuilder.Result(), nil
}

func determineCid(podStatus v1.PodStatus, containerName string) string {
	for _, containerStatus := range podStatus.ContainerStatuses {
		if containerStatus.Name == containerName {
			return containerStatus.ContainerID
		}
	}
	return ""
}

func containerIDInPodStatus(podStatus v1.PodStatus, containerDirName string) bool {
	for _, containerStatus := range podStatus.ContainerStatuses {
		trimmedCid := strings.TrimPrefix(containerStatus.ContainerID, "docker://")
		if strings.Contains(containerDirName, trimmedCid) {
			return true
		}
	}
	return false
}

func (setHandler *SetHandler) applyCpusetToContainer(containerID string, cpuset cpuset.CPUSet) (string, error) {
	if cpuset.IsEmpty() {
		//Nothing to set. We will leave the container running on the Kubernetes provisioned default cpuset
		log.Printf("WARNING: for some reason cpuset to set was quite empty for container: %s.I left it untouched.", containerID)
		return "", nil
	}
	//According to K8s documentation CID is stored in "docker://<container_id>" format when dockershim is configured for CRE
	trimmedCid := strings.TrimPrefix(containerID, "docker://")
	var pathToContainerCpusetFile string
	err := filepath.Walk(setHandler.cpusetRoot, func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(path, trimmedCid) {
			pathToContainerCpusetFile = path
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("%s cpuset path error: %s", trimmedCid, err.Error())
	}
	if pathToContainerCpusetFile == "" {
		return "", fmt.Errorf("cpuset file does not exist for container: %s under the provided cgroupfs hierarchy: %s", trimmedCid, setHandler.cpusetRoot)
	}
	returnContainerPath := pathToContainerCpusetFile
	//And for our grand finale, we just "echo" the calculated cpuset to the cpuset cgroupfs "file" of the given container
	//Find child cpuset if it exists (kube-proxy)
	err = filepath.Walk(pathToContainerCpusetFile, func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if f.IsDir() {
			pathToContainerCpusetFile = path
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("%s child cpuset path error: %s", trimmedCid, err.Error())
	}
	file, err := os.OpenFile(pathToContainerCpusetFile+"/cpuset.cpus", os.O_WRONLY|os.O_SYNC, 0755)
	if err != nil {
		return "", fmt.Errorf("can't open cpuset file: %s for container: %s because: %s", pathToContainerCpusetFile, containerID, err)
	}
	defer file.Close()
	_, err = file.WriteString(cpuset.String())
	if err != nil {
		return "", fmt.Errorf("can't modify cpuset file: %s for container: %s because: %s", pathToContainerCpusetFile, containerID, err)
	}
	return returnContainerPath, nil
}

func getInfraContainerPath(podStatus v1.PodStatus, searchPath string) string {
	var pathToInfraContainer string
	filelist, _ := filepath.Glob(filepath.Dir(searchPath) + "/*")
	for _, fpath := range filelist {
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

func (setHandler *SetHandler) applyCpusetToInfraContainer(podMeta metav1.ObjectMeta, podStatus v1.PodStatus, pathToSearchContainer string) error {
	cpuset := setHandler.poolConfig.SelectPool(types.DefaultPoolID).CPUset
	if cpuset.IsEmpty() {
		//Nothing to set. We will leave the container running on the Kubernetes provisioned default cpuset
		log.Printf("WARNING: for some reason DEFAULT cpuset was quite empty. Cannot adjust cpuset for infra container for %s in namespace: %s", podMeta.Name, podMeta.Namespace)
		return nil
	}
	if pathToSearchContainer == "" {
		return fmt.Errorf("container directory does not exists under the provided cgroupfs hierarchy: %s", setHandler.cpusetRoot)
	}
	pathToContainerCpusetFile := getInfraContainerPath(podStatus, pathToSearchContainer)
	if pathToContainerCpusetFile == "" {
		return fmt.Errorf("cpuset file does not exist for infra container under the provided cgroupfs hierarchy: %s", setHandler.cpusetRoot)
	}
	file, err := os.OpenFile(pathToContainerCpusetFile+"/cpuset.cpus", os.O_WRONLY|os.O_SYNC, 0755)
	if err != nil {
		return fmt.Errorf("can't open cpuset file: %s for infra container: %s because: %s", pathToContainerCpusetFile, filepath.Base(pathToContainerCpusetFile), err)
	}
	defer file.Close()
	_, err = file.WriteString(cpuset.String())
	if err != nil {
		return fmt.Errorf("can't modify cpuset file: %s for infra container: %s because: %s", pathToContainerCpusetFile, filepath.Base(pathToContainerCpusetFile), err)
	}
	return nil
}
