package types

import (
	"github.com/go-yaml/yaml"
	"github.com/golang/glog"
	"io/ioutil"
)

// Pool defines cpupool
type Pool struct {
	CPUs     string `yaml:"cpus"`
	PoolType string `yaml:"pooltype"`
}

// PoolConfig defines pool configurtion for a node
type PoolConfig struct {
	ResourceBaseName string          `yaml:"resourceBaseName"`
	Pools            map[string]Pool `yaml:"pools"`
}

// PoolConfigFile defines the pool configuration file location
var PoolConfigFile = "/etc/cpu-dp/poolconfig.yaml"

// ReadPoolConfig implements pool configuration file reading
func ReadPoolConfig() (PoolConfig, error) {
	var pools PoolConfig
	file, err := ioutil.ReadFile(PoolConfigFile)
	if err != nil {
		glog.Errorf("Could not read poolconfig")
	} else {
		err = yaml.Unmarshal([]byte(file), &pools)
		if err != nil {
			glog.Errorf("Error in poolconfig file %v", err)
		}
	}
	return pools, err
}
