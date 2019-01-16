package types

import (
	"github.com/go-yaml/yaml"
	"github.com/golang/glog"
	"io/ioutil"
	"path/filepath"
)

// PoolerConfig defines CPU-Pooler configuration for a cluster
type PoolerConfig struct {
	ResourceBaseName string `yaml:"resourceBaseName"`
}

// PoolerConfigDir defines the CPU-Pooler global configuration file location
var PoolerConfigDir = "/etc/cpu-pooler"

// ReadPoolerConfig implements CPU-Pooler configuration file reading
func ReadPoolerConfig() (*PoolerConfig, error) {
	var poolerConfig PoolerConfig
	var err error
	f := filepath.Join(PoolerConfigDir, "cpu-pooler.yaml")
	file, err := ioutil.ReadFile(f)
	if err != nil {
		glog.Errorf("Could not read CPU-Pooler config: %s:%v", f, err)
	} else {
		err = yaml.Unmarshal([]byte(file), &poolerConfig)
		if err != nil {
			glog.Errorf("Error in CPU-Pooler config file %v", err)
		}
		return &poolerConfig, nil
	}
	return nil, err
}
