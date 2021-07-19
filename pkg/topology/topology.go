package topology

import (
	"bytes"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
	"log"
	"os/exec"
	"strconv"
	"strings"
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

//ExecCommand is generic wrapper around cmd.Run. It executes the exec.Cmd arriving as an input parameters, and either returns an error, or the stdout of the command to the caller
//Used to interrogate CPU topology and cpusets directly from the host OS
func ExecCommand(cmd *exec.Cmd) (string, error) {
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return string(stdout.Bytes()), nil
}

func listAndParseCores(attribute string) map[int]int {
	coreMap := make(map[int]int)
	outStr, err := ExecCommand(exec.Command("lscpu", "-p=cpu,"+attribute))
	if err != nil {
		log.Println("ERROR: could not interrogate the CPU topology of the node with lscpu, because:" + err.Error())
		return coreMap
	}
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
