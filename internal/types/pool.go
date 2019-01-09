package types

import (
	"errors"
	"github.com/go-yaml/yaml"
	"github.com/golang/glog"
	"io/ioutil"
	"path/filepath"
)

// Pool defines cpupool
type Pool struct {
	CPUs string `yaml:"cpus"`
}

// PoolConfig defines pool configurtion for a node
type PoolConfig struct {
	Pools        map[string]Pool   `yaml:"pools"`
	NodeSelector map[string]string `yaml:"nodeSelector"`
}

// PoolConfigDir defines the pool configuration file location
var PoolConfigDir = "/etc/cpu-pooler"

// ReadPoolConfig implements pool configuration file reading
func ReadPoolConfig(labelMap map[string]string) (PoolConfig, string, error) {
	files, err := filepath.Glob(filepath.Join(PoolConfigDir, "poolconfig-*"))
	if err != nil {
		glog.Fatal(err)
	}
	for _, f := range files {
		var pools PoolConfig
		file, err := ioutil.ReadFile(f)
		if err != nil {
			glog.Errorf("Could not read poolconfig: %s:%v", f, err)
		} else {
			err = yaml.Unmarshal([]byte(file), &pools)
			if err != nil {
				glog.Errorf("Error in poolconfig file %v", err)
			}
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
	glog.Fatalf("No labels matching pool configuration files, labels: %v", labelMap)
	return PoolConfig{}, "", errors.New("Poolconfiguration not found for node")
}

// ReadPoolConfigFile reads a pool configuration file
func ReadPoolConfigFile(name string) (PoolConfig, error) {
	var pools PoolConfig
	file, err := ioutil.ReadFile(name)
	if err != nil {
		glog.Errorf("Could not read poolconfig: %s:%v", name, err)
	} else {
		err = yaml.Unmarshal([]byte(file), &pools)
		if err != nil {
			glog.Errorf("Error in poolconfig file %v", err)
		}
	}
	return pools, err
}
