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
	//PoolConfigDir defines the pool configuration file location
	PoolConfigDir = "/etc/cpu-pooler"
	//SingleThreadHTPolicy is the constant for the single threaded value of the HT policy pool attribute. Only the physical thread is allocated for exclusive requests when this value is set
	SingleThreadHTPolicy = "singleThreaded"
	//MultiThreadHTPolicy is the constant for the multi threaded value of the HT policy pool attribute. All siblings are allocated together for exclusive requests when this value is set
	MultiThreadHTPolicy = "multiThreaded"
)

// Pool defines cpupool
type Pool struct {
	CPUset   cpuset.CPUSet
	CPUStr   string `yaml:"cpus"`
	HTPolicy string `yaml:"hyperThreadingPolicy"`
}

// PoolConfig defines pool configuration for a node
type PoolConfig struct {
	Pools        map[string]Pool   `yaml:"pools"`
	NodeSelector map[string]string `yaml:"nodeSelector"`
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
func DeterminePoolConfig() (PoolConfig, string, error) {
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
	file, err := ioutil.ReadFile(name)
	if err != nil {
		return PoolConfig{}, fmt.Errorf("could not read poolconfig file: %s, because: %s", name, err)
	}
	var poolConfig PoolConfig
	err = yaml.Unmarshal([]byte(file), &poolConfig)
	if err != nil {
		return PoolConfig{}, fmt.Errorf("CPU pool config file could not be parsed because: %s", err)
	}
	for poolName, poolBody := range poolConfig.Pools {
		tempPool := poolBody
		tempPool.CPUset, err = cpuset.Parse(poolBody.CPUStr)
		if err != nil {
			return PoolConfig{}, fmt.Errorf("CPUs could not be parsed because: %s", err)
		}
		if poolBody.HTPolicy == "" {
			tempPool.HTPolicy = SingleThreadHTPolicy
		}
		poolConfig.Pools[poolName] = tempPool

	}
	return poolConfig, err
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
