package sethandler

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nokia/CPU-Pooler/pkg/checkpoint"
	"github.com/nokia/CPU-Pooler/pkg/k8sclient"
	"github.com/nokia/CPU-Pooler/pkg/topology"
	"github.com/nokia/CPU-Pooler/pkg/types"
	"golang.org/x/sys/unix"
	"io"
	"io/ioutil"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	//MaxRetryCount controls how many times we re-try a remote API operation
	MaxRetryCount = 150
	//RetryInterval controls how much time (in milliseconds) we wait between two retry attempts when talking to a remote API
	RetryInterval = 200
)

var (
	resourceBaseName       = "nokia.k8s.io"
	processConfigKey       = resourceBaseName + "/cpus"
	setterAnnotationSuffix = "cpusets-configured"
	setterAnnotationKey    = resourceBaseName + "/" + setterAnnotationSuffix
	containerPrefixList    = []string{"docker://", "containerd://"}
)

type workItem struct {
	oldPod *v1.Pod
	newPod *v1.Pod
}

//SetHandler is the data set encapsulating the configuration data needed for the CPUSetter Controller to be able to adjust cpusets
type SetHandler struct {
	poolConfig      types.PoolConfig
	cpusetRoot      string
	k8sClient       kubernetes.Interface
	informerFactory informers.SharedInformerFactory
	podSynced       cache.InformerSynced
	workQueue       workqueue.Interface
	stopChan        *chan struct{}
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
	setHandler.workQueue = workqueue.New()
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
	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient, time.Second)
	podInformer := kubeInformerFactory.Core().V1().Pods().Informer()
	setHandler := SetHandler{
		poolConfig:      poolConfig,
		cpusetRoot:      cpusetRoot,
		k8sClient:       kubeClient,
		informerFactory: kubeInformerFactory,
		podSynced:       podInformer.HasSynced,
		workQueue:       workqueue.New(),
	}
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			setHandler.PodAdded((reflect.ValueOf(obj).Interface().(*v1.Pod)))
		},
	})
	podInformer.SetWatchErrorHandler(setHandler.WatchErrorHandler)
	return &setHandler, nil
}

//Run kicks the CPUSetter controller into motion, synchs it with the API server, and starts the desired number of asynch worker threads to handle the Pod API events
func (setHandler *SetHandler) Run(threadiness int, stopCh *chan struct{}) error {
	setHandler.stopChan = stopCh
	setHandler.informerFactory.Start(*stopCh)
	log.Println("INFO: Starting cpusetter Controller...")
	log.Println("INFO: Waiting for Pod Controller cache to sync...")
	if ok := cache.WaitForCacheSync(*stopCh, setHandler.podSynced); !ok {
		return errors.New("failed to sync Pod Controller from cache! Are you sure everything is properly connected?")
	}
	log.Println("INFO: Starting " + strconv.Itoa(threadiness) + " cpusetter worker threads...")
	for i := 0; i < threadiness; i++ {
		go wait.Until(setHandler.runWorker, time.Second, *stopCh)
	}
	setHandler.StartReconciliation()
	log.Println("INFO: CPUSetter is successfully initialized, worker threads are now serving requests!")
	return nil
}

//PodAdded handles ADD operations
func (setHandler *SetHandler) PodAdded(pod *v1.Pod) {
	workItem := workItem{newPod: pod}
	setHandler.workQueue.Add(workItem)
}

//WatchErrorHandler is an event handler invoked when the CPUSetter Controller's connection to the K8s API server breaks
//In case the error is terminal it initiates a graceful shutdown for the whole Controller, implicitly restarting the connection by restarting the whole container
func (setHandler *SetHandler) WatchErrorHandler(r *cache.Reflector, err error) {
	if apierrors.IsResourceExpired(err) || apierrors.IsGone(err) || err == io.EOF {
		log.Println("INFO: One of the API watchers closed gracefully, re-establishing connection")
		return
	}
	//The default K8s client retry mechanism expires after a certain amount of time, and just gives-up
	//It is better to shutdown the whole process now and freshly re-build the watchers, rather than risking becoming a permanent zombie
	log.Println("ERROR: One of the API watchers closed unexpectedly with error:" + err.Error() + " restarting CPUSetter!")
	setHandler.Stop()
	//Give some time for gracefully terminating the connections
	time.Sleep(5 * time.Second)
	os.Exit(0)
}

//Stop is invoked by the main thread to initiate graceful shutdown procedure. It shuts down the event handler queue, and relays a stop signal to the Controller
func (setHandler *SetHandler) Stop() {
	*setHandler.stopChan <- struct{}{}
	setHandler.workQueue.ShutDown()
}

//StartReconciliation starts the reactive thread of SetHandler periodically checking expected and provisioned cpusets of the node
//In case a container's observed cpuset differs from the expected (i.e. container was restarted) the thread resets it to the proper value
func (setHandler *SetHandler) StartReconciliation() {
	go setHandler.startReconciliationLoop()
	log.Println("INFO: Successfully started the periodic cpuset reconciliation thread")
}

func (setHandler *SetHandler) runWorker() {
	for setHandler.processNextWorkItem() {
	}
}

func (setHandler *SetHandler) processNextWorkItem() bool {
	obj, areWeShuttingDown := setHandler.workQueue.Get()
	if areWeShuttingDown {
		log.Println("WARNING: Received shutdown command from queue in thread:" + strconv.Itoa(unix.Gettid()))
		return false
	}
	setHandler.processItemInQueue(obj)
	return true
}

func (setHandler *SetHandler) processItemInQueue(obj interface{}) {
	defer setHandler.workQueue.Done(obj)
	var item workItem
	var ok bool
	if item, ok = obj.(workItem); !ok {
		log.Println("WARNING: Cannot decode work item from queue in thread: " + strconv.Itoa(unix.Gettid()) + ", be aware that we are skipping some events!!!")
		return
	}
	setHandler.handlePods(item)
}

func (setHandler *SetHandler) handlePods(item workItem) {
	isItMyPod, pod := shouldPodBeHandled(*item.newPod)
	//The maze wasn't meant for you
	if !isItMyPod {
		return
	}
	containersToBeSet := gatherAllContainers(pod)
	if len(containersToBeSet) > 0 {
		var err error
		for i := 0; i < MaxRetryCount; i++ {
			err = setHandler.adjustContainerSets(pod, containersToBeSet)
			if err == nil {
				return
			}
			time.Sleep(RetryInterval * time.Millisecond)
		}
		log.Println("ERROR: Timed out trying to adjust the cpusets of the containers belonging to Pod:" + pod.ObjectMeta.Name + " ID: " + string(pod.ObjectMeta.UID) + " because:" + err.Error())
	} else {
		log.Println("WARNING: there were no containers to handle in: " + pod.ObjectMeta.Name + " ID: " + string(pod.ObjectMeta.UID) + " in thread:" + strconv.Itoa(unix.Gettid()))
	}
}

func shouldPodBeHandled(pod v1.Pod) (bool, v1.Pod) {
	// Pod has exited/completed and all containers have stopped
	if pod.Status.Phase == v1.PodSucceeded || pod.Status.Phase == v1.PodFailed {
		return false, pod
	}
	setterNodeName := os.Getenv("NODE_NAME")
	for i := 0; i < MaxRetryCount; i++ {
		//We will unconditionally read the Pod at least once due to two reasons:
		//1: 99% Chance that the Pod arriving in the CREATE event is not yet ready to be processed
		//2: Avoid spending cycles on a Pod which does not even exist anymore in the API server
		newPod, err := k8sclient.RefreshPod(pod)
		if err != nil {
			log.Println("WARNING: Pod:" + pod.ObjectMeta.Name + " ID: " + string(pod.ObjectMeta.UID) + " is not adjusted as reading it again failed with:" + err.Error())
			return false, pod
		}
		if isPodReadyForProcessing(*newPod) {
			pod = *newPod
			break
		}
		time.Sleep(RetryInterval * time.Millisecond)
	}
	//Pod is still haven't been scheduled, or it wasn't scheduled to the Node of this specific CPUSetter instance
	if setterNodeName != pod.Spec.NodeName {
		return false, pod
	}
	return true, pod
}

func isPodReadyForProcessing(pod v1.Pod) bool {
	if pod.Spec.NodeName == "" || len(pod.Status.ContainerStatuses) != len(pod.Spec.Containers) {
		return false
	}
	for _, cStatus := range pod.Status.ContainerStatuses {
		if cStatus.ContainerID == "" {
			//Pod might have been scheduled but its containers haven't been created yet
			return false
		}
	}
	return true
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

func (setHandler *SetHandler) adjustContainerSets(pod v1.Pod, containersToBeSet map[string]int) error {
	var (
		pathToContainerCpusetFile string
		err                       error
	)
	for _, container := range pod.Spec.Containers {
		if _, found := containersToBeSet[container.Name]; !found {
			continue
		}
		cpuset, err := setHandler.determineCorrectCpuset(pod, container)
		if err != nil {
			return errors.New("correct cpuset for the containers of Pod: " + pod.ObjectMeta.Name + " ID: " + string(pod.ObjectMeta.UID) + " could not be calculated in thread:" + strconv.Itoa(unix.Gettid()) + " because:" + err.Error())
		}
		containerID := determineCid(pod.Status, container.Name)
		if containerID == "" {
			return errors.New("cannot determine container ID of container: " + container.Name + " in Pod: " + pod.ObjectMeta.Name + " ID: " + string(pod.ObjectMeta.UID) + " in thread:" + strconv.Itoa(unix.Gettid()) + " because:" + err.Error())
		}
		pathToContainerCpusetFile, err = setHandler.applyCpusetToContainer(pod.ObjectMeta, containerID, cpuset)
		if err != nil {
			return errors.New("cpuset of container: " + container.Name + " in Pod: " + pod.ObjectMeta.Name + " ID: " + string(pod.ObjectMeta.UID) + " could not be re-adjusted in thread:" + strconv.Itoa(unix.Gettid()) + " because:" + err.Error())
		}
	}
	err = setHandler.applyCpusetToInfraContainer(pod.ObjectMeta, pod.Status, pathToContainerCpusetFile)
	if err != nil {
		return errors.New("cpuset of the infra container in Pod: " + pod.ObjectMeta.Name + " ID: " + string(pod.ObjectMeta.UID) + " could not be re-adjusted in thread:" + strconv.Itoa(unix.Gettid()) + " because:" + err.Error())
	}
	err = k8sclient.SetPodAnnotation(pod, setterAnnotationKey, "true")
	if err != nil {
		return errors.New("could not update annotation in Pod:" + pod.ObjectMeta.Name + " ID: " + string(pod.ObjectMeta.UID) + "  in thread:" + strconv.Itoa(unix.Gettid()) + " because: " + err.Error())
	}
	return nil
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
	buf, err := ioutil.ReadFile(checkpointFileName)
	if err != nil {
		log.Printf("Error reading file %s: Error: %v", checkpointFileName, err)
		return cpuset.CPUSet{}, fmt.Errorf("kubelet checkpoint file could not be accessed because: %s", err)
	}
	var cp checkpoint.File
	if err = json.Unmarshal(buf, &cp); err != nil {
		//K8s 1.21 changed internal file structure, so let's try that too before returning with error
		var newCpFile checkpoint.NewFile
		if err = json.Unmarshal(buf, &newCpFile); err != nil {
			log.Printf("error unmarshalling kubelet checkpoint file: %s", err)
			return cpuset.CPUSet{}, err
		}
		cp = checkpoint.TranslateNewCheckpointToOld(newCpFile)
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
			return trimContainerPrefix(containerStatus.ContainerID)
		}
	}
	return ""
}

func trimContainerPrefix(contName string) string {
	for _, prefix := range containerPrefixList {
		if strings.HasPrefix(contName, prefix) {
			return strings.TrimPrefix(contName, prefix)
		}
	}
	return contName
}

func containerIDInPodStatus(podStatus v1.PodStatus, containerDirName string) bool {
	for _, containerStatus := range podStatus.ContainerStatuses {
		trimmedCid := trimContainerPrefix(containerStatus.ContainerID)
		if strings.Contains(containerDirName, trimmedCid) {
			return true
		}
	}
	return false
}

func (setHandler *SetHandler) applyCpusetToContainer(podMeta metav1.ObjectMeta, containerID string, cpuset cpuset.CPUSet) (string, error) {
	if cpuset.IsEmpty() {
		//Nothing to set. We will leave the container running on the Kubernetes provisioned default cpuset
		log.Println("WARNING: cpuset to set was quite empty for container:" + containerID + " in Pod:" + podMeta.Name + " ID:" + string(podMeta.UID) + " in thread:" + strconv.Itoa(unix.Gettid()) + ". I left it untouched.")
		return "", nil
	}
	var pathToContainerCpusetFile string
	err := filepath.Walk(setHandler.cpusetRoot, func(path string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(path, containerID) {
			pathToContainerCpusetFile = path
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("%s cpuset path error: %s", containerID, err.Error())
	}
	if pathToContainerCpusetFile == "" {
		return "", fmt.Errorf("cpuset file does not exist for container: %s under the provided cgroupfs hierarchy: %s", containerID, setHandler.cpusetRoot)
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
		return "", fmt.Errorf("%s child cpuset path error: %s", containerID, err.Error())
	}
	err = os.WriteFile(pathToContainerCpusetFile+"/cpuset.cpus", []byte(cpuset.String()), 0755)
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
		log.Println("WARNING: DEFAULT cpuset to set was quite empty in Pod:" + podMeta.Name + " ID:" + string(podMeta.UID) + " in thread:" + strconv.Itoa(unix.Gettid()) + ". I left it untouched.")
		return nil
	}
	if pathToSearchContainer == "" {
		return fmt.Errorf("container directory does not exists under the provided cgroupfs hierarchy: %s", setHandler.cpusetRoot)
	}
	pathToContainerCpusetFile := getInfraContainerPath(podStatus, pathToSearchContainer)
	if pathToContainerCpusetFile == "" {
		return fmt.Errorf("cpuset file does not exist for infra container under the provided cgroupfs hierarchy: %s", setHandler.cpusetRoot)
	}
	err := os.WriteFile(pathToContainerCpusetFile+"/cpuset.cpus", []byte(cpuset.String()), 0755)
	if err != nil {
		return fmt.Errorf("can't modify cpuset file: %s for infra container: %s because: %s", pathToContainerCpusetFile, filepath.Base(pathToContainerCpusetFile), err)
	}
	return nil
}

func (setHandler *SetHandler) startReconciliationLoop() {
	timeToReconcile := time.NewTicker(5 * time.Second)
	for {
		select {
		case <-timeToReconcile.C:
			err := setHandler.reconcileCpusets()
			if err != nil {
				log.Println("WARNING: Periodic cpuset reconciliation failed with error:" + err.Error())
				continue
			}
		case <-*setHandler.stopChan:
			log.Println("INFO: Shutting down the periodic cpuset reconciliation thread")
			timeToReconcile.Stop()
			return
		}
	}
}

func (setHandler *SetHandler) reconcileCpusets() error {
	pods, err := k8sclient.GetMyPods()
	if pods == nil || err != nil {
		return errors.New("couldn't List my Pods in the reconciliation loop because:" + err.Error())
	}
	leafCpusets, err := setHandler.getLeafCpusets()
	if err != nil {
		return errors.New("couldn't interrogate leaf cpusets from cgroupfs because:" + err.Error())
	}
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			err = setHandler.reconcileContainer(leafCpusets, pod, container)
			if err != nil {
				log.Println("WARNING: Periodic reconciliation of container:" + container.Name + " of Pod:" + pod.ObjectMeta.Name + " in namespace:" + pod.ObjectMeta.Namespace + " failed with error:" + err.Error())
			}
		}
	}
	return nil
}

func (setHandler *SetHandler) getLeafCpusets() ([]string, error) {
	stdOut, err := topology.ExecCommand(exec.Command("find", setHandler.cpusetRoot, "-type", "d", "-links", "2"))
	if err != nil {
		return nil, err
	}
	cpusetLeaves := strings.Split(strings.TrimSuffix(stdOut, "\n"), "\n")
	return cpusetLeaves, nil
}

//Naive approach: we can prob afford not building a tree from the cgroup paths if we only reconcile every couple of seconds
//Can be further optimized on need
func (setHandler *SetHandler) reconcileContainer(leafCpusets []string, pod v1.Pod, container v1.Container) error {
	containerID := determineCid(pod.Status, container.Name)
	if containerID == "" {
		return nil
	}
	numOfCpus := runtime.NumCPU()
	badCpuset, _ := cpuset.Parse("0-" + strconv.Itoa(numOfCpus-1))
	for _, leaf := range leafCpusets {
		if strings.Contains(leaf, containerID) {
			currentCpusetByte, _ := ioutil.ReadFile(leaf + "/cpuset.cpus")
			currentCpusetStr := strings.TrimSpace(string(currentCpusetByte))
			currentCpuset, _ := cpuset.Parse(currentCpusetStr)
			if badCpuset.Equals(currentCpuset) {
				correctSet, err := setHandler.determineCorrectCpuset(pod, container)
				if err != nil {
					return errors.New("could not determine correct cpuset because:" + err.Error())
				}
				err = os.WriteFile(leaf+"/cpuset.cpus", []byte(correctSet.String()), 0755)
				if err != nil {
					return errors.New("could not overwrite cpuset file:" + leaf + "/cpuset.cpus because:" + err.Error())
				}
			}
			break
		}
	}
	return nil
}
