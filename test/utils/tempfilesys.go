package utils

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

type tmpSysFs struct {
	dirRoot      string
	dirList      []string
	fileList     map[string][]byte
	originalRoot *os.File
}

var ts = tmpSysFs{
	dirList: []string{
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0001/cont01",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0001/infrac1",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0002/cont02",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0002/infrac2",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0003/cont03a",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0003/cont03b",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0003/infrac3",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0004/cont04a",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0004/cont04b",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0004/infrac4",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0005/cont05a",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0005/cont05b",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0005/cont05c",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0005/infrac5",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0006/cont06",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0006/infrac6",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0007/cont07",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0007/infrac8",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0008/cont08a",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0008/cont08b",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0008/infrac8",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0009/cont09",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0009/infrac9",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0010/cont10",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0010/infrac10",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0011/cont11",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0012/cont12",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0013/cont13",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0014/cont14",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0015/cont15",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0016/cont16",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0017/cont17",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0018/cont18",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0019/cont19",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0021/cont21",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0022/cont22",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0023/cont23",
	},
	fileList: map[string][]byte{
		"cpuset.cpus": []byte("E"),
	},
}

// CreateTempSysFs create temporary fake filesystem for cpusets
func CreateTempSysFs() (string, error) {
	originalRoot, err := os.Open("/")
	ts.originalRoot = originalRoot

	tmpdir, err := ioutil.TempDir("/tmp", "sethandler-")
	if err != nil {
		return "", err
	}

	ts.dirRoot = tmpdir
	err = exec.Command("sudo", "chmod", "777", ts.dirRoot).Run()
	if err != nil {
		return "", err
	}

	for _, dir := range ts.dirList {
		if err := os.MkdirAll(filepath.Join(ts.dirRoot, dir), 0777); err != nil {
			return "", err
		}
	}

	for _, dir := range ts.dirList {
		for filename, content := range ts.fileList {
			if err := ioutil.WriteFile(filepath.Join(ts.dirRoot, dir, filename), content, 0777); err != nil {
				return "", err
			}
		}
	}
	return tmpdir, nil
}

// CreateCheckpointFile provides the checkpoint file
func CreateCheckpointFile() error {
	var (
		chkpntPath = "/var/lib/kubelet/device-plugins"
		fileName   = "kubelet_internal_checkpoint"
		content    = `{"Data":{"PodDeviceEntries":[
			{"PodUID":"pod0002","ContainerName":"cont_exc","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["3","4"]}},
			{"PodUID":"pod0003","ContainerName":"cont_excl1","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["3","4"]}},
			{"PodUID":"pod0003","ContainerName":"cont_excl2","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["5","6","7"]}},
			{"PodUID":"pod0004","ContainerName":"cont_exclusive","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["8"]}},
			{"PodUID":"pod0005","ContainerName":"cont_exclusive","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["3"]}},
			{"PodUID":"pod0008","ContainerName":"cont_exc_pin1","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["12","13"]}},
			{"PodUID":"pod0008","ContainerName":"cont_exc_pin2","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["14","16"]}},
			{"PodUID":"pod0009","ContainerName":"cont_exc_pin","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["6"]}},
			{"PodUID":"pod0010","ContainerName":"cont_pin_exc_shared","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["16"]}},
			{"PodUID":"pod0014","ContainerName":"chckpnt_no_device","ResourceName":"nokia.k8s.io/exclusive_caas"},
			{"PodUID":"pod0015","ContainerName":"chckpnt_no_res","DeviceIDs":{"0":["4"]}},
			{"PodUID":"pod0016","ContainerName":"chckpnt_no_device_no_res"},
			{"PodUID":"pod0019","ContainerName":"bad_deviceID_format","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["a","b","c"]}},
			{"PodUID":"pod0020","ContainerName":"no_cpuset_file","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["3","4","7"]}},
			{"PodUID":"pod0021","ContainerName":"naming_mismatch","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["3","4","7"]}},
      {"PodUID":"pod0023","ContainerName":"cont_exc_ht","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":{"0":["22","35"]}}],
			"RegisteredDevices":{"nokia.k8s.io/default":["0-2"],"nokia.k8s.io/exclusive_caas":["3","4","5","6","7","8","12","13","14","16","22","35"],"nokia.k8s.io/shared_caas":["5889","74","97","324","383","951"]}},
			"Checksum":403603645}`
	)
	err := exec.Command("sudo", "mkdir", "-p", chkpntPath).Run()
	if err != nil {
		return err
	}

	err = exec.Command("sudo", "touch", chkpntPath+"/"+fileName).Run()
	if err != nil {
		return err
	}

	err = exec.Command("sudo", "chmod", "777", chkpntPath+"/"+fileName).Run()
	if err != nil {
		return err
	}

	err = ioutil.WriteFile(filepath.Join(chkpntPath, fileName), []byte(content), 0777)
	if err != nil {
		return err
	}
	return nil
}

// RemoveTempSysFs delete temporary fake filesystem
func RemoveTempSysFs() error {
	err := ts.originalRoot.Chdir()
	if err != nil {
		return err
	}
	if err = ts.originalRoot.Close(); err != nil {
		return err
	}
	if err = exec.Command("sudo", "rm", "-rf", ts.dirRoot).Run(); err != nil {
		return err
	}
	return nil
}
