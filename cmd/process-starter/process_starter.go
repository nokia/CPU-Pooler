package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nokia/CPU-Pooler/pkg/types"
	"golang.org/x/sys/unix"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

func readCPUAnnotation() ([]types.Container, error) {
	var s string
	var containers []types.Container
	var ann string
	file, err := os.Open("/etc/podinfo/annotations")
	if err != nil {
		fmt.Printf("File open error %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		str := scanner.Text()
		if strings.Contains(str, "nokia.k8s.io/cpus=") {
			ann = strings.Replace(str, "nokia.k8s.io/cpus=", "", 1)
			break
		}
	}
	if len(ann) == 0 {
		return nil, nil
	}
	if err = scanner.Err(); err != nil {
		fmt.Printf("Scanner error %v", err)
		return nil, err
	}

	err = json.Unmarshal([]byte(ann), &s)
	if err != nil {
		fmt.Printf("Annotation unmarshall error %v", err)
		return nil, err
	}
	err = json.Unmarshal([]byte(s), &containers)
	if err != nil {
		fmt.Printf("Containers unmarshall error %v", err)
		return nil, err
	}
	return containers, nil
}

func setAffinity(nbrCPUs int, cpuList []int) []int {
	if len(cpuList) < nbrCPUs {
		fmt.Printf("Not enough cpus free, cannot set affinity %d:%v\n", nbrCPUs, cpuList)
		return nil
	}
	cpuset := new(unix.CPUSet)
	cpus := cpuList[:nbrCPUs]
	for _, cpu := range cpus {
		cpuset.Set(cpu)
	}
	unix.SchedSetaffinity(0, cpuset)
	return cpuList[nbrCPUs:]
}

func cpuListStrToIntSlice(cpuString string) (cpuList []int) {
	if cpuString == "" {
		return nil
	}
	for _, cpuStr := range strings.Split(cpuString, ",") {
		cpu, err := strconv.Atoi(cpuStr)
		if err != nil {
			return nil
		}
		cpuList = append(cpuList, cpu)
	}
	return cpuList
}

func pollCPUSetCompletion(exclusiveCPUs []int, sharedCPUs []int) {
	var cs cpuset.CPUSet
	expCpus := cpuset.NewBuilder()
	expCpus.Add(exclusiveCPUs...)
	expCpus.Add(sharedCPUs...)

	// Wait max 10 seconds for cpusetter to set the cgroup cpuset
	for i := 0; i < 10; i++ {
		file, err := os.Open("/sys/fs/cgroup/cpuset/cpuset.cpus")
		if err != nil {
			fmt.Printf("Cannot open cgroup cpuset %v", err)
			os.Exit(1)
		}
		scanner := bufio.NewScanner(file)
		scanner.Scan()
		cgCPUSet := scanner.Text()
		cs, err = cpuset.Parse(cgCPUSet)
		if err != nil {
			fmt.Printf("Cannot parse cgroup cpuset %v:%v", cgCPUSet, err)

		}
		if expCpus.Result().Equals(cs) {
			return
		}
		file.Close()
		time.Sleep(1 * time.Second)
	}
	fmt.Printf("Cgroup cpuset (%s) does not match to expected cpuset (%s)\n",
		cs.String(), expCpus.Result().String())
	os.Exit(1)
}

func main() {
	containers, err := readCPUAnnotation()
	if err != nil {
		panic("Cannot read pod cpu annotation")
	}
	myContainerName := os.Getenv("CONTAINER_NAME")
	exclCPUs := cpuListStrToIntSlice(os.Getenv("EXCLUSIVE_CPUS"))
	sharedCPUs := cpuListStrToIntSlice(os.Getenv("SHARED_CPUS"))
	fmt.Printf("Exclusive cpu list %v\n", exclCPUs)
	fmt.Printf("Shared cpu list %v\n", sharedCPUs)

	if myContainerName == "" {
		panic("CONTAINER_NAME envrionment variable not found")
	}
	pollCPUSetCompletion(exclCPUs, sharedCPUs)
	for _, container := range containers {
		if container.Name != myContainerName {
			continue
		}
		fmt.Printf("Start processes defined in annotation\n")
		// Last process replaces this process, other processes are started
		// as new processes in background
		for index, process := range container.Processes {
			fmt.Printf("  Process name %v\n", process.ProcName)
			fmt.Printf("    Args: %v ", process.Args)
			fmt.Printf("\n")
			if strings.HasPrefix(process.PoolName, "exclusive") {
				exclCPUs = setAffinity(process.CPUs, exclCPUs)
				if nil == exclCPUs {
					fmt.Printf("Failed to set affinity\n")
					os.Exit(1)
				}
			} else {
				setAffinity(len(sharedCPUs), sharedCPUs)
			}
			if index == len(container.Processes)-1 {
				args := []string{}
				args = append(args, process.ProcName)
				args = append(args, process.Args...)
				syscall.Exec(process.ProcName, args, os.Environ())
			} else {
				cmd := exec.Command(process.ProcName, process.Args...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				err := cmd.Start()
				if err != nil {
					fmt.Printf("Failed starting %v\n", cmd)
				}
			}
		}
	}
	fmt.Printf("No processes in pod annotation, start process from pod spec command\n")
	syscall.Exec(os.Args[1], os.Args[1:], os.Environ())
}
