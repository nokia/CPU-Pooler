package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/nokia/CPU-Pooler/internal/types"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func readCpuAnnotation() ([]types.Container, error) {
	var s string
	var containers []types.Container
	var str string
	file, err := os.Open("/etc/podinfo/annotations")
	if err != nil {
		fmt.Printf("File open error %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		str = scanner.Text()
		if strings.Contains(str, "nokia.k8s.io/cpus=") {
			str = strings.Replace(str, "nokia.k8s.io/cpus=", "", 1)
			break
		}
	}

	if err = scanner.Err(); err != nil {
		fmt.Printf("Scanner error %v", err)
		return nil, err
	}

	err = json.Unmarshal([]byte(str), &s)
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

func waitCommand(cmd *exec.Cmd, cch chan int, index int) {
	err := cmd.Wait()
	fmt.Printf("Process ended cmd %v, err %v\n", cmd.Path, err)
	cch <- index
}

func setAffinity(nbrCpus int, cpuList []int) []int {
	if len(cpuList) < nbrCpus {
		fmt.Printf("Not enough cpus free, cannot set affinity %d:%v\n", nbrCpus, cpuList)
		return nil
	}
	cpuset := new(unix.CPUSet)
	cpus := cpuList[:nbrCpus]
	for _, cpu := range cpus {
		cpuset.Set(cpu)
	}
	unix.SchedSetaffinity(0, cpuset)
	return cpuList[nbrCpus:]
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

func main() {
	containers, err := readCpuAnnotation()
	if err != nil {
		os.Exit(1)
	}
	var cmds []*exec.Cmd
	completionChannel := make(chan int, 10)
	myContainerName := os.Getenv("CONTAINER_NAME")
	exclCpus := os.Getenv("EXCLUSIVE_CPUS")
	exclCpuList := cpuListStrToIntSlice(exclCpus)
	fmt.Printf("Exclusive cpu list %v\n", exclCpuList)

	poolConf, err := types.ReadPoolConfig()
	if err != nil {
		panic("Configuration error")
	}

	if myContainerName == "" {
		fmt.Printf("CONTAINER_NAME envrionment variable not found")
		os.Exit(1)
	}
	for _, container := range containers {
		if container.Name != myContainerName {
			continue
		}
		fmt.Printf("Container name %s\n", container.Name)
		for index, process := range container.Processes {
			fmt.Printf("  Process name %v\n", process)
			fmt.Printf("    Args: %v ", process.Args)
			fmt.Printf("\n")
			cmd := exec.Command(process.ProcName, process.Args...)

			if poolConf.Pools[process.PoolName].PoolType == "exclusive" {
				exclCpuList = setAffinity(process.Cpus, exclCpuList)
			} else {
				/* It is shared pool */
				sharedCpuList := cpuListStrToIntSlice(poolConf.Pools[process.PoolName].Cpus)
				setAffinity(len(sharedCpuList), sharedCpuList)

			}
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Start()
			if err != nil {
				fmt.Printf("Failed starting %v", cmd)
			}
			cmds = append(cmds, cmd)
			go waitCommand(cmd, completionChannel, index)
		}
	}
	if len(cmds) == 0 {
		fmt.Printf("No processes to be started found from annotations\n")
		os.Exit(1)
	}
	select {
	case cmdIndex := <-completionChannel:
		fmt.Printf("Command index %d ended\n", cmdIndex)
		for index, _ := range cmds {
			if index != cmdIndex {
				cmds[index].Process.Kill()
				fmt.Printf("Killing command index %d\n", index)
			}
		}
		os.Exit(1)

	}
}
