package types

type Process struct {
	ProcName string   `json:"process"`
	Args     []string `json:"args"`
	Cpus     int      `json:"cpus"`
	PoolName string   `json:"pool"`
}

type Container struct {
	Name      string    `json:"container"`
	Processes []Process `json:"processes"`
}
