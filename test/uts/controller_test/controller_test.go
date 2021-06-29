package controller_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
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
	wildcardFakeCpuSetPath  = "/tmp/sethandler-*/sys/fs/cgroup/cpuset"
	testPoolConf1, _        = types.ReadPoolConfigFile("../../testdata/testpoolconfig1.yaml")
	testPoolConf2, _        = types.ReadPoolConfigFile("../../testdata/testpoolconfig2.yaml")
	singleThreadPoolConf, _ = types.ReadPoolConfigFile("../../testdata/singleThreadExclusive.yaml")
	multiThreadPoolConf, _  = types.ReadPoolConfigFile("../../testdata/multiThreadExclusive.yaml")
	testKubeconfPath        = "../../testdata/testkubeconf.yml"
	quantity1, _            = resource.ParseQuantity("1")
	quantity2, _            = resource.ParseQuantity("2")
	quantity3, _            = resource.ParseQuantity("3")
	quantity100m, _         = resource.ParseQuantity("100m")
	quantity50m, _          = resource.ParseQuantity("50m")
)

var testPods = []v1.Pod{
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_shared", UID: "pod0001"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_shared", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/shared_caas": quantity100m}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_shared", Ready: true, ContainerID: "docker://cont01"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_exc", UID: "pod0002"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_exc", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_exc", Ready: true, ContainerID: "docker://cont02"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_excl_two_container", UID: "pod0003"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{
			{Name: "cont_excl1", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}},
			{Name: "cont_excl2", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity3}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{
			{Name: "cont_excl1", Ready: true, ContainerID: "docker://cont03a"},
			{Name: "cont_excl2", Ready: true, ContainerID: "docker://cont03b"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_two_container_sh_exc", UID: "pod0004"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{
			{Name: "cont_exclusive", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}},
			{Name: "cont_shared", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/shared_caas": quantity100m}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{
			{Name: "cont_exclusive", Ready: true, ContainerID: "docker://cont04a"},
			{Name: "cont_shared", Ready: true, ContainerID: "docker://cont04b"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_three_cont_sh_exc_def", UID: "pod0005"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{
			{Name: "cont_exclusive", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}},
			{Name: "cont_shared", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/shared_caas": quantity100m}}},
			{Name: "cont_default", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/default_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{
			{Name: "cont_exclusive", Ready: true, ContainerID: "docker://cont05a"},
			{Name: "cont_shared", Ready: true, ContainerID: "docker://cont05b"},
			{Name: "cont_default", Ready: true, ContainerID: "docker://cont05c"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_default_explicit", UID: "pod0006"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_default_explicit", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/default": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_default_explicit", Ready: true, ContainerID: "docker://cont06"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_default_implicit", UID: "pod0007"},
		Spec:       v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_default_implicit"}}},
		Status:     v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_default_implicit", Ready: true, ContainerID: "docker://cont07"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_exc_pin_2_proc_2_cont", UID: "pod0008",
			Annotations: map[string]string{"nokia.k8s.io/cpus": "[" +
				"{\"container\": \"cont_exc_pin1\", \"processes\": [{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep  8;\"], \"pool\": \"exclusive_caas\", \"cpus\": 2 }]}," +
				"{\"container\": \"cont_exc_pin2\", \"processes\": [{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep 88;\"], \"pool\": \"exclusive_caas\", \"cpus\": 2 }]}	]"}},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{
			{Name: "cont_exc_pin1", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}},
			{Name: "cont_exc_pin2", Resources: v1.ResourceRequirements{Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{
			{Name: "cont_exc_pin1", Ready: true, ContainerID: "docker://cont08a"},
			{Name: "cont_exc_pin2", Ready: true, ContainerID: "docker://cont08b"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_exc_pin_2_proc_1_cont", UID: "pod0009",
			Annotations: map[string]string{"nokia.k8s.io/cpus": "[{\"container\": \"cont_exc_pin\", \"processes\": [" +
				"{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep 9; \"], \"pool\": \"exclusive_caas\", \"cpus\": 1 }," +
				"{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep 99;\"], \"pool\": \"exclusive_caas\", \"cpus\": 1 }]}]"}},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_exc_pin", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_exc_pin", Ready: true, ContainerID: "docker://cont09"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_pin_2_proc_exc_shared", UID: "pod0010",
			Annotations: map[string]string{"nokia.k8s.io/cpus": "[{\"container\": \"cont_pin_exc_shared\", \"processes\": [" +
				"{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep 10; \"], \"pool\": \"exclusive_caas\", \"cpus\": 1 }," +
				"{ \"process\": \"/bin/sh\", \"args\": [\"-c\", \"sleep 1010;\"], \"pool\": \"shared_caas\", \"cpus\": 200 }]}]"}},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_pin_exc_shared", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1, "nokia.k8s.io/shared_caas": quantity100m}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_pin_exc_shared", Ready: true, ContainerID: "docker://cont10"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_empty_cont_statuses", UID: "pod0011"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "pod_empty_cont_statuses", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity3}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "nodename_missing", UID: "pod0012"},
		Spec: v1.PodSpec{Containers: []v1.Container{{Name: "nodename_missing", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "nodename_missing", Ready: true, ContainerID: "docker://cont12"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_cont_name_and_id_empty", UID: "pod0013"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "pod_cont_name_and_id_empty", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "", Ready: true, ContainerID: ""}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "chckpnt_no_device", UID: "pod0014"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "chckpnt_no_device", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "chckpnt_no_device", Ready: true, ContainerID: "docker://cont14"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "chckpnt_no_res", UID: "pod0015"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "chckpnt_no_res", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "chckpnt_no_res", Ready: true, ContainerID: "docker://cont15"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "chckpnt_no_device_no_res", UID: "pod0016"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "chckpnt_no_device_no_res", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "chckpnt_no_device_no_res", Ready: true, ContainerID: "docker://cont16"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "no_chckpnt_entry", UID: "pod0017"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "no_chckpnt_entry", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "no_chckpnt_entry", Ready: true, ContainerID: "docker://cont17"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "missing_objmeta_uid"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "missing_objmeta_uid", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "missing_objmeta_uid", Ready: true, ContainerID: "docker://cont18"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "bad_deviceID_format", UID: "pod0019"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "bad_deviceID_format", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "bad_deviceID_format", Ready: true, ContainerID: "docker://cont19"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "no_cpuset_file", UID: "pod0020"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "no_cpuset_file", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity3}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "no_cpuset_file", Ready: true, ContainerID: "docker://cont20"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "naming_mismatch", UID: "pod0021"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "naming_mismatch1", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "naming_mismatch2", Ready: true, ContainerID: "docker://cont21"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_default_explicit_no_default_pool", UID: "pod0022"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_default_explicit_no_default_pool", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/default": quantity1}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_default_explicit_no_default_pool", Ready: true, ContainerID: "docker://cont22"}}}},
	v1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod_ht_test", UID: "pod0023"},
		Spec: v1.PodSpec{NodeName: "caas_master", Containers: []v1.Container{{Name: "cont_exc_ht", Resources: v1.ResourceRequirements{
			Requests: v1.ResourceList{"nokia.k8s.io/exclusive_caas": quantity2}}}}},
		Status: v1.PodStatus{Phase: "Running", ContainerStatuses: []v1.ContainerStatus{{Name: "cont_exc_ht", Ready: true, ContainerID: "docker://cont23"}}}},
}

var podCpuSetPaths = map[string][]string{
	"pod_shared":                           {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0001/cont01"},
	"pod_exc":                              {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0002/cont02"},
	"pod_excl_two_container":               {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0003/cont03a", "/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0003/cont03b"},
	"pod_two_container_sh_exc":             {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0004/cont04a", "/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0004/cont04b"},
	"pod_three_cont_sh_exc_def":            {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0005/cont05a", "/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0005/cont05b", "/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0005/cont05c"},
	"pod_default_explicit":                 {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0006/cont06"},
	"pod_default_implicit":                 {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0007/cont07"},
	"pod_exc_pin_2_proc_2_cont":            {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0008/cont08a", "/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0008/cont08b"},
	"pod_exc_pin_2_proc_1_cont":            {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0009/cont09"},
	"pod_pin_2_proc_exc_shared":            {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0010/cont10"},
	"pod_empty_cont_statuses":              {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0011/cont11"},
	"nodename_missing":                     {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0012/cont12"},
	"pod_cont_name_and_id_empty":           {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0013/cont13"},
	"chckpnt_no_device":                    {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0014/cont14"},
	"chckpnt_no_res":                       {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0015/cont15"},
	"chckpnt_no_device_no_res":             {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0016/cont16"},
	"no_chckpnt_entry":                     {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0017/cont17"},
	"missing_objmeta_uid":                  {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0018/cont18"},
	"bad_deviceID_format":                  {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0019/cont19"},
	"no_cpuset_file":                       {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0020/cont20"},
	"naming_mismatch":                      {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0021/cont21"},
	"pod_default_explicit_no_default_pool": {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0022/cont22"},
	"pod_ht_test":                          {"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0023/cont23"},
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
	{"pod_empty_cont_statuses", false, testPoolConf1, []string{"E"}},
	{"nodename_missing", false, testPoolConf1, []string{"E"}},
	{"pod_cont_name_and_id_empty", false, testPoolConf1, []string{"E"}},
	{"chckpnt_no_device", false, testPoolConf1, []string{"0-2"}},
	{"chckpnt_no_res", false, testPoolConf1, []string{"0-2"}},
	{"chckpnt_no_device_no_res", false, testPoolConf1, []string{"0-2"}},
	{"no_chckpnt_entry", false, testPoolConf1, []string{"0-2"}},
	{"missing_objmeta_uid", true, testPoolConf1, []string{"0-2"}},
	{"bad_deviceID_format", false, testPoolConf1, []string{"E"}},
	{"no_cpuset_file", true, testPoolConf1, nil},
	{"naming_mismatch", false, testPoolConf1, []string{"E"}},
	{"pod_default_explicit_no_default_pool", false, testPoolConf2, []string{"E"}},
	{"pod_ht_test", false, singleThreadPoolConf, []string{"22,35"}},
	{"pod_ht_test", false, multiThreadPoolConf, []string{"22,35,62,75"}},
}

func TestNew(t *testing.T) {
	sh, err := sethandler.New(testKubeconfPath, testPoolConf1, getFakeCpusetRoot(wildcardFakeCpuSetPath))
	if err != nil || sh == nil {
		t.Errorf("Sethandler object creation failed because: %s", err.Error())
	}
}

//TODO: with the change in architecture we now need to stub out k8sclient handler (the fake one provided in the setup was never used internally to begin with)
//Otherwise this will never work because we now unconditionally re-read the provided Pod at least once
/*
func TestPodAdded(t *testing.T) {
	tempDirPath, err := setupEnv()
	if err != nil {
		t.Errorf("Test suite setup failed: %s", err.Error())
	}

	for _, tc := range podAddedTcs {
		t.Run(tc.podName, func(t *testing.T) {
			testSethandler := setupTestSethandler(tc.poolConf)
			testSethandler.PodAdded(getPod(tc.podName))
			cpusActual, err := readFakeCpusetFile(getPod(tc.podName), tempDirPath)
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
}*/

func setupEnv() (string, error) {
	os.Setenv("NODE_NAME", "caas_master")
	tempDirPath, err := utils.CreateTempSysFs()
	if err != nil {
		return "", err
	}
	err = utils.CreateCheckpointFile()
	if err != nil {
		return "", err
	}
	return tempDirPath, nil
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

func getPod(podName string) *v1.Pod {
	for _, pod := range testPods {
		if pod.ObjectMeta.Name == podName {
			return &pod
		}
	}
	return nil
}

func readFakeCpusetFile(pod *v1.Pod, tempDirPath string) ([]string, error) {
	var readCpusets []string

	for _, cpusetPath := range podCpuSetPaths[pod.ObjectMeta.Name] {
		cpusetFilePath := filepath.Join(tempDirPath, cpusetPath, "cpuset.cpus")
		fileContent, err := ioutil.ReadFile(cpusetFilePath)
		if err != nil {
			return nil, fmt.Errorf("can't read fake cpuset file: %s in Pod: %s because: %s", cpusetFilePath, pod.ObjectMeta.UID, err)
		}
		readCpusets = append(readCpusets, string(fileContent))
	}
	return readCpusets, nil
}
