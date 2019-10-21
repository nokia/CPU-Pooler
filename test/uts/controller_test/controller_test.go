package controller_test

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nokia/CPU-Pooler/pkg/sethandler"
	"github.com/nokia/CPU-Pooler/pkg/types"
	"github.com/nokia/CPU-Pooler/test/utils"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	wildcardFakeCpuSetPath = "/tmp/sethandler-*/sys/fs/cgroup/cpuset"
	testPoolConf1, _       = types.ReadPoolConfigFile("../../testdata/testpoolconfig1.yaml")
	testPoolConf2, _       = types.ReadPoolConfigFile("../../testdata/testpoolconfig2.yaml")
	testKubeconfPath       = "../../testdata/testkubeconf.yml"
	quantity1, _           = resource.ParseQuantity("1")
	quantity2, _           = resource.ParseQuantity("2")
	quantity3, _           = resource.ParseQuantity("3")
	quantity100m, _        = resource.ParseQuantity("100m")
	quantity50m, _         = resource.ParseQuantity("50m")
)
var testPods = []v1.Pod{
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_shared", UID: "pod0001"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_shared", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/shared_caas": quantity100m}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_shared", Ready: true, ContainerID: "docker://0001"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_exc", UID: "pod0002"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_exc", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_exc", Ready: true, ContainerID: "docker://0002"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_excl_two_container", UID: "pod0003"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{
			{Name: "cont_excl1", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}},
			{Name: "cont_excl2", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity3}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{
			{Name: "cont_excl1", Ready: true, ContainerID: "docker://0003a"},
			{Name: "cont_excl2", Ready: true, ContainerID: "docker://0003b"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_two_container_sh_exc", UID: "pod0004"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{
			{Name: "cont_exclusive", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}},
			{Name: "cont_shared", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/shared_caas": quantity100m}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{
			{Name: "cont_exclusive", Ready: true, ContainerID: "docker://0004a"},
			{Name: "cont_shared", Ready: true, ContainerID: "docker://0004b"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_three_cont_sh_exc_def", UID: "pod0005"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{
			{Name: "cont_exclusive", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}},
			{Name: "cont_shared", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/shared_caas": quantity100m}}},
			{Name: "cont_default", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/default_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{
			{Name: "cont_exclusive", Ready: true, ContainerID: "docker://0005a"},
			{Name: "cont_shared", Ready: true, ContainerID: "docker://0005b"},
			{Name: "cont_default", Ready: true, ContainerID: "docker://0005c"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_default_explicit", UID: "pod0006"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_default_explicit", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/default": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_default_explicit", Ready: true, ContainerID: "docker://0006"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_default_implicit", UID: "pod0007"},
		Spec:       v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_default_implicit"}}},
		Status:     v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_default_implicit", Ready: true, ContainerID: "docker://0007"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_exc_pin_2_proc_2_cont", UID: "pod0008",
			Annotations: map[string]string{"nokia.k8s.io/cpus": "[" +
				"{\"container\": \"cont_exc_pin1\", \"processes\": [{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep  8;\"], \"pool\": \"exclusive_caas\", \"cpus\": 2 }]}," +
				"{\"container\": \"cont_exc_pin2\", \"processes\": [{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep 88;\"], \"pool\": \"exclusive_caas\", \"cpus\": 2 }]}	]"}},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{
			{Name: "cont_exc_pin1", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}},
			{Name: "cont_exc_pin2", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{
			{Name: "cont_exc_pin1", Ready: true, ContainerID: "docker://0008a"},
			{Name: "cont_exc_pin2", Ready: true, ContainerID: "docker://0008b"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_exc_pin_2_proc_1_cont", UID: "pod0009",
			Annotations: map[string]string{"nokia.k8s.io/cpus": "[{\"container\": \"cont_exc_pin\", \"processes\": [" +
				"{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep 9; \"], \"pool\": \"exclusive_caas\", \"cpus\": 1 }," +
				"{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep 99;\"], \"pool\": \"exclusive_caas\", \"cpus\": 1 }]}]"}},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_exc_pin", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_exc_pin", Ready: true, ContainerID: "docker://0009"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_pin_2_proc_exc_shared", UID: "pod0010",
			Annotations: map[string]string{"nokia.k8s.io/cpus": "[{\"container\": \"cont_pin_exc_shared\", \"processes\": [" +
				"{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep 10; \"], \"pool\": \"exclusive_caas\", \"cpus\": 1 }," +
				"{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep 1010;\"], \"pool\": \"shared_caas\", \"cpus\": 200 }]}]"}},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_pin_exc_shared", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1, "nokia.k8s.io/shared_caas": quantity100m}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_pin_exc_shared", Ready: true, ContainerID: "docker://0010"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_not_running", UID: "pod0011"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "pod_not_running", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity3}}}}},
		Status: v1.PodStatus{Phase: "Pending", ContainerStatuses: []v1.ContainerStatus{{Name: "pod_not_running", Ready: true, ContainerID: "docker://0011"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "nodename_missing", UID: "pod0012"},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "nodename_missing", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "nodename_missing", Ready: true, ContainerID: "docker://0012"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_not_ready", UID: "pod0013"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "pod_not_ready", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "pod_not_ready", Ready: false, ContainerID: "docker://0013"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "chckpnt_no_device", UID: "pod0014"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "chckpnt_no_device", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "chckpnt_no_device", Ready: true, ContainerID: "docker://0014"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "chckpnt_no_res", UID: "pod0015"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "chckpnt_no_res", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "chckpnt_no_res", Ready: true, ContainerID: "docker://0015"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "chckpnt_no_device_no_res", UID: "pod0016"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "chckpnt_no_device_no_res", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "chckpnt_no_device_no_res", Ready: true, ContainerID: "docker://0016"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "no_chckpnt_entry", UID: "pod0017"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "no_chckpnt_entry", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "no_chckpnt_entry", Ready: true, ContainerID: "docker://0017"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "missing_objmeta_uid"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "missing_objmeta_uid", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "missing_objmeta_uid", Ready: true, ContainerID: "docker://0018"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "bad_deviceID_format", UID: "pod0019"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "bad_deviceID_format", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "bad_deviceID_format", Ready: true, ContainerID: "docker://0019"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "no_cpuset_file", UID: "pod0020"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "no_cpuset_file", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity3}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "no_cpuset_file", Ready: true, ContainerID: "docker://0020"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "no_CID", UID: "pod0021"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "no_CID", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity100m}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "no_CID", Ready: true}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "naming_mismatch", UID: "pod0022"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "naming_mismatch1", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "naming_mismatch2", Ready: true, ContainerID: "docker://0022"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_default_explicit_no_default_pool", UID: "pod0023"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_default_explicit_no_default_pool", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/default": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_default_explicit_no_default_pool", Ready: true, ContainerID: "docker://0023"}}}},
}

var podAddedTcs = []struct {
	podName                       string
	isErrorExpectedAtFakeFileRead bool
	poolConf                      types.PoolConfig
	expectedCpus                  []string
}{
	{"pod_shared", false, testPoolConf1, []string{"9-12,15,17"}},
	{"pod_exc", false, testPoolConf1, []string{"3-4"}},
	{"pod_excl_two_container", false, testPoolConf1, []string{"3-4", "5-7"}},
	{"pod_two_container_sh_exc", false, testPoolConf1, []string{"8", "9-12,15,17"}},
	{"pod_three_cont_sh_exc_def", false, testPoolConf1, []string{"3", "9-12,15,17", "0-2"}},
	{"pod_default_explicit", false, testPoolConf1, []string{"0-2"}},
	{"pod_default_implicit", false, testPoolConf1, []string{"0-2"}},
	{"pod_exc_pin_2_proc_2_cont", false, testPoolConf1, []string{"12-13", "14,16"}},
	{"pod_exc_pin_2_proc_1_cont", false, testPoolConf1, []string{"6"}},
	{"pod_pin_2_proc_exc_shared", false, testPoolConf1, []string{"9-12,15-17"}},
	{"pod_not_running", false, testPoolConf1, []string{"E"}},
	{"nodename_missing", false, testPoolConf1, []string{"E"}},
	{"pod_not_ready", false, testPoolConf1, []string{"E"}},
	{"chckpnt_no_device", false, testPoolConf1, []string{"0-2"}},
	{"chckpnt_no_res", false, testPoolConf1, []string{"0-2"}},
	{"chckpnt_no_device_no_res", false, testPoolConf1, []string{"0-2"}},
	{"no_chckpnt_entry", false, testPoolConf1, []string{"0-2"}},
	{"missing_objmeta_uid", true, testPoolConf1, nil},
	{"bad_deviceID_format", false, testPoolConf1, []string{"E"}},
	{"no_cpuset_file", true, testPoolConf1, nil},
	{"no_CID", true, testPoolConf1, nil},
	{"naming_mismatch", false, testPoolConf1, []string{"E"}},
	{"pod_default_explicit_no_default_pool", false, testPoolConf2, []string{"E"}},
}

var podChangedTcs = []struct {
	oldPodName                    string
	newPodName                    string
	isErrorExpectedAtFakeFileRead bool
	poolConf                      types.PoolConfig
	expectedCpus                  []string
}{
	{"pod_exc", "pod_shared", false, testPoolConf1, []string{"9-12,15,17"}},
	{"pod_shared", "pod_exc", false, testPoolConf1, []string{"3-4"}},
	{"pod_exc", "pod_excl_two_container", false, testPoolConf1, []string{"3-4", "5-7"}},
	{"pod_exc", "pod_two_container_sh_exc", false, testPoolConf1, []string{"8", "9-12,15,17"}},
	{"pod_exc", "pod_three_cont_sh_exc_def", false, testPoolConf1, []string{"3", "9-12,15,17", "0-2"}},
	{"pod_exc", "pod_default_explicit", false, testPoolConf1, []string{"0-2"}},
	{"pod_exc", "pod_default_implicit", false, testPoolConf1, []string{"0-2"}},
	{"pod_exc", "pod_exc_pin_2_proc_2_cont", false, testPoolConf1, []string{"12-13", "14,16"}},
	{"pod_exc", "pod_exc_pin_2_proc_1_cont", false, testPoolConf1, []string{"6"}},
	{"pod_exc", "pod_pin_2_proc_exc_shared", false, testPoolConf1, []string{"9-12,15-17"}},
	{"pod_exc", "pod_not_running", false, testPoolConf1, []string{"E"}},
	{"pod_exc", "nodename_missing", false, testPoolConf1, []string{"E"}},
	{"pod_exc", "pod_not_ready", false, testPoolConf1, []string{"E"}},
	{"pod_exc", "chckpnt_no_device", false, testPoolConf1, []string{"0-2"}},
	{"pod_exc", "chckpnt_no_res", false, testPoolConf1, []string{"0-2"}},
	{"pod_exc", "chckpnt_no_device_no_res", false, testPoolConf1, []string{"0-2"}},
	{"pod_exc", "no_chckpnt_entry", false, testPoolConf1, []string{"0-2"}},
	{"pod_exc", "missing_objmeta_uid", true, testPoolConf1, nil},
	{"pod_exc", "bad_deviceID_format", false, testPoolConf1, []string{"E"}},
	{"pod_exc", "no_cpuset_file", true, testPoolConf1, nil},
	{"pod_exc", "no_CID", true, testPoolConf1, nil},
	{"pod_exc", "naming_mismatch", false, testPoolConf1, []string{"E"}},
}

func TestCreateController(t *testing.T) {
	testSh := setupTestSethandler(testPoolConf1)
	ctrl := testSh.CreateController()
	if ctrl == nil {
		t.Errorf("Controller creation failed")
	}
}

func TestNew(t *testing.T) {
	sh, err := sethandler.New(testKubeconfPath, testPoolConf1, getFakeCpusetRoot(wildcardFakeCpuSetPath))
	if err != nil || sh == nil {
		t.Errorf("Sethandler object creation failed because: %s", err.Error())
	}
}

func TestPodAdded(t *testing.T) {
	err := setupEnv()
	if err != nil {
		t.Errorf("Test suite setup failed: %s", err.Error())
	}

	for _, tc := range podAddedTcs {
		t.Run(tc.podName, func(t *testing.T) {
			testSethandler := setupTestSethandler(tc.poolConf)
			testSethandler.PodAdded(getPod(tc.podName))
			cpusActual, err := readFakeCpusetFile(getPod(tc.podName))
			if err != nil && !tc.isErrorExpectedAtFakeFileRead {
				t.Logf("Could not process FAKE cpuset file because: %s", err.Error())
			}
			if !reflect.DeepEqual(cpusActual, tc.expectedCpus) {
				t.Errorf("Mismatch in expected (%s) vs actual (%s) cpus written in cpuset file: ", tc.expectedCpus, cpusActual)
			}
		})
	}
	err = utils.RemoveTempSysFs()
	if err != nil {
		t.Logf("Removal of temp fs for cpusets was unsuccessful due to: %s", err.Error())
	}
}

func TestPodChanged(t *testing.T) {
	err := setupEnv()
	if err != nil {
		t.Errorf("Test suite could not be set up: %s", err.Error())
	}

	for _, tc := range podChangedTcs {
		t.Run(tc.newPodName, func(t *testing.T) {
			testSethandler := setupTestSethandler(tc.poolConf)
			testSethandler.PodChanged(getPod(tc.oldPodName), getPod(tc.newPodName))
			cpusActual, err := readFakeCpusetFile(getPod(tc.newPodName))
			if err != nil && !tc.isErrorExpectedAtFakeFileRead {
				t.Logf("Could not process FAKE cpuset file because: %s", err.Error())
			}
			if !reflect.DeepEqual(cpusActual, tc.expectedCpus) {
				t.Errorf("Mismatch in expected (%s) vs actual (%s) cpus written in cpuset file:", tc.expectedCpus, cpusActual)
			}

		})
	}
	err = utils.RemoveTempSysFs()
	if err != nil {
		t.Logf("Removal of temp fs for cpusets was unsuccessful due to: %s", err.Error())
	}
}

func setupEnv() error {
	os.Setenv("NODE_NAME", "caas_master")
	err := utils.CreateTempSysFs()
	if err != nil {
		return err
	}
	err = utils.CreateCheckpointFile()
	if err != nil {
		return err
	}
	return nil
}

func setupTestSethandler(poolConf types.PoolConfig) sethandler.SetHandler {
	sh := sethandler.SetHandler{}
	sh.SetSetHandler(poolConf, getFakeCpusetRoot(wildcardFakeCpuSetPath), fake.NewSimpleClientset())
	return sh
}

func getFakeCpusetRoot(cpusetPath string) string {
	paths, _ := filepath.Glob(cpusetPath)
	var p string
	for _, path := range paths {
		p = path
	}
	return p
}

func getPod(podName string) v1.Pod {
	for _, pod := range testPods {
		if pod.ObjectMeta.Name == podName {
			return pod
		}
	}
	return v1.Pod{}
}

func readFakeCpusetFile(pod v1.Pod) ([]string, error) {
	var (
		fakepath     string
		containerIDs []string
		readCpusets  []string
	)

	filepath.Walk(getFakeCpusetRoot(wildcardFakeCpuSetPath), func(path string, f os.FileInfo, err error) error {
		if strings.HasSuffix(path, string(pod.ObjectMeta.UID)) && f.IsDir() {
			fakepath = path
		}
		return nil
	})
	for _, status := range pod.Status.ContainerStatuses {
		containerIDs = append(containerIDs, strings.TrimPrefix(status.ContainerID, "docker://"))
	}
	for _, containerID := range containerIDs {
		fileContent, err := ioutil.ReadFile(fakepath + "/" + containerID + "/cpuset.cpus")
		readCpusets = append(readCpusets, string(fileContent))
		if err != nil {
			return nil, errors.New("Can't read fake cpuset file:" + fakepath + " for container:" + containerID + " in Pod: " + string(pod.ObjectMeta.UID) + " because:" + err.Error())
		}
	}
	return readCpusets, nil
}
