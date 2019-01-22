package types

import (
	"encoding/json"
	"errors"
	"github.com/golang/glog"
	"strings"
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

const (
	validationErrNoContainerName int = iota
	validationErrNoProcesses
	validationErrNoProcessName
	validationErrNoCpus
)

var validationErrStr = map[int]string{
	validationErrNoContainerName: "'container' is mandatory in annotation",
	validationErrNoProcesses:     "'processes' is mandatory in annotation",
	validationErrNoProcessName:   "'process' (name) is mandatory in annotation",
	validationErrNoCpus:          "'cpus' field is mandatory in annotation",
}

// Containers returns container name string in annotation
func (cpuAnnotation CPUAnnotation) Containers() []string {
	var containers []string

	for _, cont := range cpuAnnotation {
		containers = append(containers, cont.Name)
	}
	return containers
}

// ContainerSharedCPUTime returns sum of cpu time requested from shared pool by a container
func (cpuAnnotation CPUAnnotation) ContainerSharedCPUTime(container string) int {
	var cpuTime int

	for _, cont := range cpuAnnotation {
		if cont.Name == container {
			for _, process := range cont.Processes {
				if strings.HasPrefix(process.PoolName, "shared") {
					cpuTime += process.CPUs
				}
			}
		}
	}
	return cpuTime

}

// ContainerExclusiveCPU returns sum of cpu time requested from exclusive pool by a container
func (cpuAnnotation CPUAnnotation) ContainerExclusiveCPU(container string) int {
	var cpuTime int

	for _, cont := range cpuAnnotation {
		if cont.Name == container {
			for _, process := range cont.Processes {
				if strings.HasPrefix(process.PoolName, "exclusive") {
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
	for _, c := range *cpuAnnotation {
		if len(c.Name) == 0 {
			return errors.New(validationErrStr[validationErrNoContainerName])
		}
		if len(c.Processes) == 0 {
			return errors.New(validationErrStr[validationErrNoProcesses])

		}
		for _, p := range c.Processes {
			if len(p.ProcName) == 0 {
				return errors.New(validationErrStr[validationErrNoProcessName])

			}
			if p.CPUs == 0 {
				return errors.New(validationErrStr[validationErrNoCpus])

			}
		}
	}
	return nil
}
