package types

import (
	"testing"
)

func init() {
	PoolConfigDir = "../../test/testdata/cpu-pooler"
}

func TestReadPoolConfig(t *testing.T) {
	labels := make(map[string]string)
	labels["nodeType"] = "dpdk"
	labels["label1"] = "label1"

	poolConfig, err := readPoolConfig(labels)
	if err != nil {
		t.Errorf("Failed to read pool config %v", err)
	}
	if value, ok := poolConfig.NodeSelector["nodeType"]; ok {
		if value != "dpdk" {
			t.Errorf("Wrong config: %v", poolConfig)
		}
	} else {
		t.Error("Nodetype not found")
	}
}
