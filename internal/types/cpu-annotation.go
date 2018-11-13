package types

import (
	"encoding/json"
	"github.com/golang/glog"
)

// Process defines process information in pod annotation
// The information is used for setting CPU affinity
type Process struct {
	ProcName string   `json:"process"`
	Args     []string `json:"args"`
	CPUs     int      `json:"cpus"`
	PoolName string   `json:"pool"`
}

// Container idenfifies container and defines the processes to be started
type Container struct {
	Name      string    `json:"container"`
	Processes []Process `json:"processes"`
}

// CPUAnnotation defines the pod cpu annotation structure
type CPUAnnotation []Container

// Containers returns container name string in annotation
func (cpuAnnotation CPUAnnotation) Containers() []string {
	var containersToPatch []string

	for _, cont := range cpuAnnotation {
		containersToPatch = append(containersToPatch, cont.Name)
	}
	return containersToPatch
}

// ContainerSharedCPUTime returns sum of cpu time requested from shared pool by a container
func (cpuAnnotation CPUAnnotation) ContainerSharedCPUTime(container string, poolConf PoolConfig) int {
	var cpuTime int

	for _, cont := range cpuAnnotation {
		if cont.Name == container {
			for _, process := range cont.Processes {
				if "shared" == poolConf.Pools[process.PoolName].PoolType {
					cpuTime += process.CPUs
				}
			}
		}
	}
	return cpuTime

}

// ContainerPools returns all pools configured for container
func (cpuAnnotation CPUAnnotation) ContainerPools(cName string) (pools []string) {
	var poolMap = make(map[string]bool)
	for _, container := range cpuAnnotation {
		if container.Name == cName {
			for _, process := range container.Processes {
				if _, ok := poolMap[process.PoolName]; !ok {
					pools = append(pools, process.PoolName)
					poolMap[process.PoolName] = true
				}
			}
		}
	}
	return pools
}

// ContainerTotalCPURequest returns CPU requests of container from pool
func (cpuAnnotation CPUAnnotation) ContainerTotalCPURequest(pool string, cName string) int {
	var cpuRequest int
	for _, container := range cpuAnnotation {
		if container.Name == cName {
			for _, process := range container.Processes {
				if process.PoolName == pool {
					cpuRequest += process.CPUs
				}
			}
		}
	}
	return cpuRequest
}

// Decode unmarshals json annotation to CPUAnnotation
func (cpuAnnotation *CPUAnnotation) Decode(annotation []byte) error {
	err := json.Unmarshal(annotation, cpuAnnotation)
	if err != nil {
		glog.Error(err)
		return err
	}
	return nil
}
