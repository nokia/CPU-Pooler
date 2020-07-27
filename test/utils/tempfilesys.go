package utils

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0001/0001",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0001/infrac1",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0002/0002",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0002/infrac2",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0003/0003a",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0003/0003b",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0003/infrac3",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0004/0004a",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0004/0004b",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0004/infrac4",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0005/0005a",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0005/0005b",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0005/0005c",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0005/infrac5",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0006/0006",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0006/infrac6",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0007/0007",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0007/infrac8",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0008/0008a",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0008/0008b",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0008/infrac8",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0009/0009",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0009/infrac9",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0010/0010",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0010/infrac10",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0011/0011",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0011/0012",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0012/0012",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0013/0013",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0014/0014",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0015/0015",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0016/0016",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0017/0017",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0018/0018",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0019/0019",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0021/0021",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0022/0022",
		"/sys/fs/cgroup/cpuset/kubepods/besteffort/pod0023/0023",
	},
	fileList: map[string][]byte{
		"cpuset.cpus": []byte("E"),
	},
}

// CreateTempSysFs create temporary fake filesystem for cpusets
func CreateTempSysFs() error {
	originalRoot, err := os.Open("/")
	ts.originalRoot = originalRoot

	tmpdir, err := ioutil.TempDir("/tmp", "sethandler-")
	if err != nil {
		return err
	}

	ts.dirRoot = tmpdir
	err = exec.Command("sudo", "chmod", "777", ts.dirRoot).Run()
	if err != nil {
		return err
	}

	for _, dir := range ts.dirList {
		if err := os.MkdirAll(filepath.Join(ts.dirRoot, dir), 0777); err != nil {
			return err
		}
	}

	for _, dir := range ts.dirList {
		for filename, content := range ts.fileList {
			if err := ioutil.WriteFile(filepath.Join(ts.dirRoot, dir, filename), content, 0777); err != nil {
				return err
			}
		}
	}
	return nil
}

// CreateCheckpointFile provides the checkpoint file
func CreateCheckpointFile() error {
	var (
		chkpntPath = "/var/lib/kubelet/device-plugins"
		fileName   = "kubelet_internal_checkpoint"
		content    = `{"Data":{"PodDeviceEntries":[
			{"PodUID":"pod0002","ContainerName":"cont_exc","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["3","4"]},
			{"PodUID":"pod0003","ContainerName":"cont_excl1","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["3","4"]},
			{"PodUID":"pod0003","ContainerName":"cont_excl2","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["5","6","7"]},
			{"PodUID":"pod0004","ContainerName":"cont_exclusive","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["8"]},
			{"PodUID":"pod0005","ContainerName":"cont_exclusive","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["3"]},
			{"PodUID":"pod0008","ContainerName":"cont_exc_pin1","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["12","13"]},
			{"PodUID":"pod0008","ContainerName":"cont_exc_pin2","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["14","16"]},
			{"PodUID":"pod0009","ContainerName":"cont_exc_pin","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["6"]},
			{"PodUID":"pod0010","ContainerName":"cont_pin_exc_shared","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["16"]},
			{"PodUID":"pod0014","ContainerName":"chckpnt_no_device","ResourceName":"nokia.k8s.io/exclusive_caas"},
			{"PodUID":"pod0015","ContainerName":"chckpnt_no_res","DeviceIDs":["4"]},
			{"PodUID":"pod0016","ContainerName":"chckpnt_no_device_no_res"},
			{"PodUID":"pod0019","ContainerName":"bad_deviceID_format","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["a","b","c"]},
			{"PodUID":"pod0020","ContainerName":"no_cpuset_file","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["3","4","7"]},
			{"PodUID":"pod0021","ContainerName":"no_CID","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["3","4","7"]},
			{"PodUID":"pod0022","ContainerName":"naming_mismatch","ResourceName":"nokia.k8s.io/exclusive_caas","DeviceIDs":["3","4","7"]}],
			"RegisteredDevices":{"nokia.k8s.io/default":["0-2"],"nokia.k8s.io/exclusive_caas":["3","4","5","6","7","8","12","13","14","16"],"nokia.k8s.io/shared_caas":["5889","74","97","324","383","951"]}},
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
	if err = syscall.Chroot("."); err != nil {
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
