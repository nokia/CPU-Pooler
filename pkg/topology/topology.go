package topology

import (
	"bytes"
	"log"
	"os/exec"
	"strconv"
	"strings"
  "github.com/nokia/CPU-Pooler/pkg/types"
  "k8s.io/api/core/v1"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

const (
  htPolicyAnnotation = types.PoolerAnnotationPrefix + "/hyperThreadingPolicy"
)

//GetNodeTopology inspects the node's CPU architecture with lscpu, and returns a map of coreID-NUMA node ID associations
func GetNodeTopology() map[int]int {
	return listAndParseCores("node")
}

//GetHTTopology inspects the node's CPU architecture with lscpu, and returns a map of physical coreID-list of logical coreIDs associations
func GetHTTopology() map[int]string {
	coreMap := listAndParseCores("core")
	htMap := make(map[int]string)
	for logicalCoreID, physicalCoreID := range coreMap {
		//We don't want to duplicate the physical core itself into the logical core ID list
		if physicalCoreID != logicalCoreID {
			logicalCoreIDStr := strconv.Itoa(logicalCoreID)
			if htMap[physicalCoreID] != "" {
				htMap[physicalCoreID] += ","
			}
			htMap[physicalCoreID] += logicalCoreIDStr
		}
	}
	return htMap
}

//AddHTSiblingsToCPUSet takes an allocated exclusive CPU set and expands it with all the sibling threads belonging to the allocated physical cores
func AddHTSiblingsToCPUSet(exclusiveCPUSet cpuset.CPUSet, coreMap map[int]string) cpuset.CPUSet {
	tempSet := exclusiveCPUSet
	for _, coreID := range exclusiveCPUSet.ToSlice() {
		if siblings, exists := coreMap[coreID]; exists {
			siblingSet, err := cpuset.Parse(siblings)
			if err != nil {
				log.Println("ERROR: could not parse the HT siblings list of assigned exclusive cores because:" + err.Error())
				return exclusiveCPUSet
			}
			tempSet = tempSet.Union(siblingSet)
		}
	}
	return tempSet
}

//GetPodHTPreference inspects if the K8s Pod has a specific hyperthreading policy preference set, and returns it to the caller
func GetPodHTPreference(pod v1.Pod) string {
  if pod.ObjectMeta.Annotations[htPolicyAnnotation] == types.MultiThreadHTPolicy {
    return types.MultiThreadHTPolicy
  }
  return types.SingleThreadHTPolicy
}

func listAndParseCores(attribute string) map[int]int {
	cmd := exec.Command("lscpu", "-p=cpu,"+attribute)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	coreMap := make(map[int]int)
	if err != nil {
		log.Println("ERROR: could not interrogate the CPU topology of the node with lscpu, because:" + err.Error())
		return coreMap
	}
	outStr := string(stdout.Bytes())
	//Here be dragons: we need to manually parse the stdout into a CPU core map line-by-line
	//lscpu -p and -J options are mutually exclusive :(
	for _, lsLine := range strings.Split(strings.TrimSuffix(outStr, "\n"), "\n") {
		cpuInfoStr := strings.Split(lsLine, ",")
		if len(cpuInfoStr) != 2 {
			continue
		}
		cpuInt, cpuErr := strconv.Atoi(cpuInfoStr[0])
		attributeInt, numaErr := strconv.Atoi(cpuInfoStr[1])
		if cpuErr != nil || numaErr != nil {
			continue
		}
		coreMap[cpuInt] = attributeInt
	}
	return coreMap
}
