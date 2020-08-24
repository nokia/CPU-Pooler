package types

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/golang/glog"
	"github.com/nokia/CPU-Pooler/pkg/k8sclient"
	"gopkg.in/yaml.v2"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

const (
//SharedPoolID is the constant prefix in the name of the CPU pool. It is used to signal that a CPU pool is of shared type
	SharedPoolID = "shared"
//ExclusivePoolID is the constant prefix in the name of the CPU pool. It is used to signal that a CPU pool is of exclusive type
	ExclusivePoolID = "exclusive"
//DefaultPoolID is the constant prefix in the name of the CPU pool. It is used to signal that a CPU pool is of default type
	DefaultPoolID = "default"
)

var (
//PoolConfigDir defines the pool configuration file location
	PoolConfigDir = "/etc/cpu-pooler"
)
// Pool defines cpupool
type Pool struct {
	CPUs   cpuset.CPUSet
}

// PoolConfig defines pool configuration for a node
type PoolConfig struct {
	Pools        map[string]Pool
	NodeSelector map[string]string
}

//DeterminePoolType takes the name of CPU pool as defined in the CPU-Pooler ConfigMap, and returns the type of CPU pool it represents.
//Type of the pool is determined based on the constant prefixes used in the name of the pool.
//A type can be shared, exclusive, or default.
func DeterminePoolType(poolName string) string {
	if strings.HasPrefix(poolName, SharedPoolID) {
		return SharedPoolID
	} else if strings.HasPrefix(poolName, ExclusivePoolID) {
		return ExclusivePoolID
	}
	return DefaultPoolID
}

//DeterminePoolConfig first interrogates the label set of the Node this process runs on.
//It uses this information to select the specific PoolConfig file corresponding to the Node.
//Returns the selected PoolConfig file, the name of the file, or an error if it was impossible to determine which config file is applicable.
func DeterminePoolConfig() (PoolConfig,string,error) {
	nodeLabels, err := k8sclient.GetNodeLabels()
	if err != nil {
		return PoolConfig{}, "", fmt.Errorf("following error happend when trying to read K8s API server Node object: %s", err)
	}
	return readPoolConfig(nodeLabels)
}

// ReadPoolConfig implements pool configuration file reading
func readPoolConfig(labelMap map[string]string) (PoolConfig, string, error) {
	files, err := filepath.Glob(filepath.Join(PoolConfigDir, "poolconfig-*"))
	if err != nil {
		return PoolConfig{}, "", err
	}
	for _, f := range files {
		pools, err := ReadPoolConfigFile(f)
		if err != nil {
			return PoolConfig{}, "", err
		}
		if labelMap == nil {
			glog.Infof("Using first configuration file %s as pool config in lieu of missing Node information", f)
			return pools, f, nil
		}
		for label, labelValue := range labelMap {
			if value, ok := pools.NodeSelector[label]; ok {
				if value == labelValue {
					glog.Infof("Using configuration file %s for pool config", f)
					return pools, f, nil
				}
			}
		}
	}
	return PoolConfig{}, "", fmt.Errorf("no matching pool configuration file found for provided nodeSelector labels")
}

// ReadPoolConfigFile reads a pool configuration file
func ReadPoolConfigFile(name string) (PoolConfig, error) {
	var pools PoolConfig
	var parsePools struct {
		Pools        map[string]struct{
				CPUStr string        `yaml:"cpus"`
			}   `yaml:"pools"`
		NodeSelector map[string]string `yaml:"nodeSelector"`
	}
	file, err := ioutil.ReadFile(name)
	if err != nil {
		return PoolConfig{}, fmt.Errorf("could not read poolconfig file: %s, because: %s", name, err)
	}
	err = yaml.Unmarshal([]byte(file), &parsePools)
	if err != nil {
		return PoolConfig{}, fmt.Errorf("poolconfig file could not be parsed because: %s", err)
	}
	pools.NodeSelector = parsePools.NodeSelector
	pools.Pools = map[string]Pool{}
	for pool := range parsePools.Pools {
		temp := pools.Pools[pool]
		temp.CPUs, err = cpuset.Parse(parsePools.Pools[pool].CPUStr)
		if err != nil {
			return PoolConfig{}, fmt.Errorf("CPUs could not be parsed because: %s", err)
		}
		pools.Pools[pool] = temp
	}
	return pools, err
}

//SelectPool returns the exact CPUSet belonging to either the exclusive, shared, or default pool of one PoolConfig object
//An empty CPUSet is returned in case the configuration does not contain the requested type
func (poolConf PoolConfig) SelectPool(prefix string) Pool {
	for poolName, pool := range poolConf.Pools {
		if strings.HasPrefix(poolName, prefix) {
			return pool
		}
	}
	return Pool{}
}
