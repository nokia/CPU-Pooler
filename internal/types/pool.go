package types

import (
	"github.com/go-yaml/yaml"
	"github.com/golang/glog"
	"io/ioutil"
)

type Pool struct {
	Cpus     string `yaml:"cpus"`
	PoolType string `yaml:"pooltype"`
}

type PoolConfig struct {
	ResourceBaseName string          `yaml:"resourceBaseName"`
	Pools            map[string]Pool `yaml:"pools"`
}

func ReadPoolConfig() (PoolConfig, error) {
	var pools PoolConfig
	file, err := ioutil.ReadFile("/etc/cpu-dp/poolconfig.yaml")
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
