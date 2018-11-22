package types

import (
	"reflect"
	"testing"
)

var cpuAnnotation = CPUAnnotation{
	{Name: "Container1", Processes: []Process{
		{ProcName: "proc1", Args: []string{"-c", "1"}, CPUs: 120, PoolName: "pool1"},
		{ProcName: "proc2", Args: []string{"-c", "1"}, CPUs: 1, PoolName: "pool2"},
		{ProcName: "proc3", Args: []string{"-c", "1"}, CPUs: 130, PoolName: "pool1"}}},
	{Name: "Container2", Processes: []Process{
		{ProcName: "proc4", Args: []string{"-c", "1"}, CPUs: 120, PoolName: "pool1"},
		{ProcName: "proc5", Args: []string{"-c", "1"}, CPUs: 1, PoolName: "pool2"},
		{ProcName: "proc6", Args: []string{"-c", "1"}, CPUs: 130, PoolName: "pool1"},
		{ProcName: "proc7", Args: []string{"-c", "1"}, CPUs: 300, PoolName: "pool3"},
	}}}

var poolConfig = PoolConfig{ResourceBaseName: "nokia.k8s.io", Pools: map[string]Pool{
	"pool1": {CPUs: "2,3", PoolType: "shared"},
	"pool2": {CPUs: "4,5", PoolType: "exclusive"},
	"pool3": {CPUs: "7,8", PoolType: "shared"},
}}

func TestGetContainerPools(t *testing.T) {
	pools := cpuAnnotation.ContainerPools("Container1")

	if !reflect.DeepEqual(pools, []string{"pool1", "pool2"}) {
		t.Errorf("%v", pools)
	}
}

func TestGetContainerCpuRequest(t *testing.T) {

	if 250 != cpuAnnotation.ContainerTotalCPURequest("pool1", "Container2") {
		t.Errorf("CPU request does not match %v", cpuAnnotation.ContainerTotalCPURequest("pool1", "Container1"))
	}
}

func TestGetContainers(t *testing.T) {

	if !reflect.DeepEqual([]string{"Container1", "Container2"}, cpuAnnotation.Containers()) {
		t.Errorf("Get containers failed %v", cpuAnnotation.Containers())
	}
}
func TestContainerSharedCPUTime(t *testing.T) {
	if 550 != cpuAnnotation.ContainerSharedCPUTime("Container2", poolConfig) {
		t.Errorf("CPU request does not match %v", cpuAnnotation.ContainerSharedCPUTime("Container1", poolConfig))
	}
}

func TestContainerDecodeAnnotation(t *testing.T) {
	var podannotation = []byte(`[{"container": "cputestcontainer","processes":  [{"process": "/bin/sh","args": ["-c","/thread_busyloop"], "cpus": 1,"pool": "pool1"},{"process": "/bin/sh","args": ["-c","/thread_busyloop2"], "cpus": 2,"pool": "pool2"} ] } ]`)
	ca := CPUAnnotation{}
	ca.Decode([]byte(podannotation), poolConfig)
	pools := ca.ContainerPools("cputestcontainer")
	if !reflect.DeepEqual(pools, []string{"pool1", "pool2"}) {
		t.Errorf("Failed to get pool %v", pools)
	}

}
func TestContainerDecodeAnnotationUnmarshalFail(t *testing.T) {
	var podannotationFail = []byte(`["container": "cputestcontainer","processes":  [{"process": "/bin/sh","args": ["-c","/thread_busyloop"], "cpus": 1,"pool": "cpupool1"},{"process": "/bin/sh","args": ["-c","/thread_busyloop2"], "cpus": 2,"pool": "cpupool2"} ] } ]`)
	ca := CPUAnnotation{}
	err := ca.Decode([]byte(podannotationFail), poolConfig)
	if err == nil {
		t.Errorf("Decode unexpectedly succeeded\n")
	}

}

func TestContainerDecodeAnnotationNoContainerName(t *testing.T) {
	var podannotation = []byte(`[{"processes":  [{"process": "/bin/sh","args": ["-c","/thread_busyloop"], "cpus": 1,"pool": "pool1"},{"process": "/bin/sh","args": ["-c","/thread_busyloop2"], "cpus": 2,"pool": "pool2"} ] } ]`)
	ca := CPUAnnotation{}
	err := ca.Decode([]byte(podannotation), poolConfig)

	if err == nil {
		t.Errorf("Decode unexpectedly succeeded\n")
		return
	}
	if err.Error() != validationErrStr[validationErrNoContainerName] {
		t.Errorf("Unexpected error %s\n", err.Error())

	}
}

func TestContainerDecodeAnnotationNoProcessName(t *testing.T) {
	var podannotation = []byte(`[{"container": "cputestcontainer","processes":  [{"args": ["-c","/thread_busyloop"], "cpus": 1,"pool": "pool1"},{"process": "/bin/sh","args": ["-c","/thread_busyloop2"], "cpus": 2,"pool": "pool2"} ] } ]`)
	ca := CPUAnnotation{}
	err := ca.Decode([]byte(podannotation), poolConfig)

	if err == nil {
		t.Errorf("Decode unexpectedly succeeded\n")
		return
	}
	if err.Error() != validationErrStr[validationErrNoProcessName] {
		t.Errorf("Unexpected error %s\n", err.Error())

	}
}
func TestContainerDecodeAnnotationNoProcesses(t *testing.T) {
	var podannotation = []byte(`[{"container": "cputestcontainer" } ]`)
	ca := CPUAnnotation{}
	err := ca.Decode([]byte(podannotation), poolConfig)

	if err == nil {
		t.Errorf("Decode unexpectedly succeeded\n")
		return
	}
	if err.Error() != validationErrStr[validationErrNoProcesses] {
		t.Errorf("Unexpected error %s\n", err.Error())

	}
}
func TestContainerDecodeAnnotationNoCpus(t *testing.T) {
	var podannotation = []byte(`[{"container": "cputestcontainer","processes":  [{"process": "/bin/sh","args": ["-c","/thread_busyloop"], "pool": "pool1"},{"process": "/bin/sh","args": ["-c","/thread_busyloop2"], "cpus": 2,"pool": "pool2"} ] } ]`)
	ca := CPUAnnotation{}
	err := ca.Decode([]byte(podannotation), poolConfig)
	if err == nil {
		t.Errorf("Decode unexpectedly succeeded\n")
		return
	}
	if err.Error() != validationErrStr[validationErrNoCpus] {
		t.Errorf("Unexpected error %s\n", err.Error())

	}
}
