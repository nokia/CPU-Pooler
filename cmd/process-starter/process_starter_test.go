package main

import (
	"reflect"
	"testing"
)

func TestSetAffinity(t *testing.T) {
	cpuList := []int{2, 3, 4, 5}
	cpuList = setAffinity(2, cpuList)
	if !reflect.DeepEqual(cpuList, []int{4, 5}) {
		t.Errorf("Cpulist error %v:%v", cpuList, []int{4, 5})
	}
	cpuList = setAffinity(2, cpuList)
	if !reflect.DeepEqual(cpuList, []int{}) {
		t.Errorf("Cpulist error %v:%v", cpuList, []int{})
	}
	cpuList = setAffinity(2, cpuList)
	if cpuList != nil {
		t.Errorf("Cpulist error %v", cpuList)
	}
}
