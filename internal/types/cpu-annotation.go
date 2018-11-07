package types

// Process defines process information in pod annotation
// The information is used for setting CPU affinity
type Process struct {
	ProcName string   `json:"process"`
	Args     []string `json:"args"`
	CPUs     int      `json:"cpus"`
	PoolName string   `json:"pool"`
}

// Container idenfifies container and defines the processes to be started
type Container struct {
	Name      string    `json:"container"`
	Processes []Process `json:"processes"`
}
