package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/golang/glog"
	"github.com/nokia/CPU-Pooler/pkg/topology"
	"github.com/nokia/CPU-Pooler/pkg/types"
	"golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpuset"
)

var (
	resourceBaseName = "nokia.k8s.io"
	cdms             []*cpuDeviceManager
)

type cpuDeviceManager struct {
	pool           types.Pool
	socketFile     string
	grpcServer     *grpc.Server
	sharedPoolCPUs string
	poolType       string
	nodeTopology   map[int]int
	htTopology     map[int]string
}

func (cdm *cpuDeviceManager) PreStartContainer(ctx context.Context, psRqt *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

func (cdm *cpuDeviceManager) Start() error {
	pluginEndpoint := filepath.Join(pluginapi.DevicePluginPath, cdm.socketFile)
	glog.Infof("Starting CPU Device Plugin server at: %s\n", pluginEndpoint)
	lis, err := net.Listen("unix", pluginEndpoint)
	if err != nil {
		glog.Errorf("Error. Starting CPU Device Plugin server failed: %v", err)
	}
	cdm.grpcServer = grpc.NewServer()

	// Register all services
	pluginapi.RegisterDevicePluginServer(cdm.grpcServer, cdm)

	go cdm.grpcServer.Serve(lis)

	// Wait for server to start by launching a blocking connection
	conn, err := grpc.Dial(pluginEndpoint, grpc.WithInsecure(), grpc.WithBlock(),
		grpc.WithTimeout(5*time.Second),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}),
	)
	if err != nil {
		glog.Errorf("Error. Could not establish connection with gRPC server: %v", err)
		return err
	}
	glog.Infoln("CPU Device Plugin server started serving")
	conn.Close()
	return nil
}

func (cdm *cpuDeviceManager) cleanup() error {
	pluginEndpoint := filepath.Join(pluginapi.DevicePluginPath, cdm.socketFile)
	if err := os.Remove(pluginEndpoint); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (cdm *cpuDeviceManager) Stop() error {
	glog.Infof("CPU Device Plugin gRPC server..")
	if cdm.grpcServer == nil {
		return nil
	}
	cdm.grpcServer.Stop()
	cdm.grpcServer = nil
	return cdm.cleanup()
}

func (cdm *cpuDeviceManager) ListAndWatch(e *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	var updateNeeded = true
	for {
		if updateNeeded {
			resp := new(pluginapi.ListAndWatchResponse)
			if cdm.poolType == "shared" {
				nbrOfCPUs := cdm.pool.CPUset.Size()
				for i := 0; i < nbrOfCPUs*1000; i++ {
					cpuID := strconv.Itoa(i)
					resp.Devices = append(resp.Devices, &pluginapi.Device{ID: cpuID, Health: pluginapi.Healthy})
				}
			} else {
				for _, cpuID := range cdm.pool.CPUset.ToSlice() {
					exclusiveCore := pluginapi.Device{ID: strconv.Itoa(cpuID), Health: pluginapi.Healthy}
					if numaNode, exists := cdm.nodeTopology[cpuID]; exists {
						exclusiveCore.Topology = &pluginapi.TopologyInfo{Nodes: []*pluginapi.NUMANode{{ID: int64(numaNode)}}}
					}
					resp.Devices = append(resp.Devices, &exclusiveCore)
				}
			}
			if err := stream.Send(resp); err != nil {
				glog.Errorf("Error. Cannot update device states: %v\n", err)
				return err
			}
			updateNeeded = false
		}
		//TODO: When is update needed ?
		time.Sleep(5 * time.Second)
	}
	return nil

}

func (cdm *cpuDeviceManager) Allocate(ctx context.Context, rqt *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resp := new(pluginapi.AllocateResponse)
	for _, container := range rqt.ContainerRequests {
		envmap := make(map[string]string)
		cpusAllocated, _ := cpuset.Parse("")
		for _, id := range container.DevicesIDs {
			tempSet, _ := cpuset.Parse(id)
			cpusAllocated = cpusAllocated.Union(tempSet)
		}
		if cdm.pool.HTPolicy == types.MultiThreadHTPolicy {
			cpusAllocated = topology.AddHTSiblingsToCPUSet(cpusAllocated, cdm.htTopology)
		}
		if cdm.poolType == "shared" {
			envmap["SHARED_CPUS"] = cdm.sharedPoolCPUs
		} else {
			envmap["EXCLUSIVE_CPUS"] = cpusAllocated.String()
		}
		containerResp := new(pluginapi.ContainerAllocateResponse)
		glog.Infof("CPUs allocated: %s: Num of CPUs %s", cpusAllocated.String(),
			strconv.Itoa(cpusAllocated.Size()))

		containerResp.Envs = envmap
		resp.ContainerResponses = append(resp.ContainerResponses, containerResp)
	}
	return resp, nil
}

func (cdm *cpuDeviceManager) GetDevicePluginOptions(context.Context, *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

func (cdm *cpuDeviceManager) Register(kubeletEndpoint, resourceName string) error {
	conn, err := grpc.Dial(kubeletEndpoint, grpc.WithInsecure(),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}))
	if err != nil {
		glog.Errorf("CPU Device Plugin cannot connect to Kubelet service: %v", err)
		return err
	}
	defer conn.Close()
	client := pluginapi.NewRegistrationClient(conn)

	request := &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     cdm.socketFile,
		ResourceName: resourceName,
	}

	if _, err = client.Register(context.Background(), request); err != nil {
		glog.Errorf("CPU Device Plugin cannot register to Kubelet service: %v", err)
		return err
	}
	return nil
}

func newCPUDeviceManager(poolName string, pool types.Pool, sharedCPUs string) *cpuDeviceManager {
	glog.Infof("Starting plugin for pool: %s", poolName)
	return &cpuDeviceManager{
		pool:           pool,
		socketFile:     fmt.Sprintf("cpudp_%s.sock", poolName),
		sharedPoolCPUs: sharedCPUs,
		poolType:       types.DeterminePoolType(poolName),
		nodeTopology:   topology.GetNodeTopology(),
		htTopology:     topology.GetHTTopology(),
	}
}

func validatePools(poolConf types.PoolConfig) (string, error) {
	var sharedCPUs string
	var err error
	for poolName, pool := range poolConf.Pools {
		poolType := types.DeterminePoolType(poolName)
		if poolType == types.SharedPoolID {
			if sharedCPUs != "" {
				err = fmt.Errorf("Only one shared pool allowed")
				glog.Errorf("Pool config : %v", poolConf)
				break
			}
			sharedCPUs = pool.CPUset.String()
		}
	}
	return sharedCPUs, err
}

func createCDMs(poolConf types.PoolConfig, sharedCPUs string) error {
	var err error
	for poolName, pool := range poolConf.Pools {
		poolType := types.DeterminePoolType(poolName)
		//Deault or unrecognizable pools need not be made available to Device Manager as schedulable devices
		if poolType == types.DefaultPoolID {
			continue
		}
		cdm := newCPUDeviceManager(poolName, pool, sharedCPUs)
		cdms = append(cdms, cdm)
		if err := cdm.Start(); err != nil {
			glog.Errorf("cpuDeviceManager.Start() failed: %v", err)
			break
		}
		resourceName := resourceBaseName + "/" + poolName
		err := cdm.Register(path.Join(pluginapi.DevicePluginPath, "kubelet.sock"), resourceName)
		if err != nil {
			// Stop server
			cdm.grpcServer.Stop()
			glog.Error(err)
			break
		}
		glog.Infof("CPU device plugin registered with the Kubelet")
	}
	return err
}

func createPluginsForPools() error {
	files, err := filepath.Glob(filepath.Join(pluginapi.DevicePluginPath, "cpudp*"))
	if err != nil {
		glog.Fatal(err)
	}
	for _, f := range files {
		if err := os.Remove(f); err != nil {
			glog.Fatal(err)
		}
	}
	poolConf, err := types.DeterminePoolConfig()
	if err != nil {
		glog.Fatal(err)
	}
	glog.Infof("Pool configuration %v", poolConf)

	var sharedCPUs string
	sharedCPUs, err = validatePools(poolConf)
	if err != nil {
		return err
	}

	if err := createCDMs(poolConf, sharedCPUs); err != nil {
		for _, cdm := range cdms {
			cdm.Stop()
		}
	}
	return err
}

func main() {
	flag.Parse()
	watcher, _ := fsnotify.NewWatcher()
	watcher.Add(path.Join(pluginapi.DevicePluginPath, "kubelet.sock"))
	defer watcher.Close()

	// respond to syscalls for termination
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	if err := createPluginsForPools(); err != nil {
		glog.Fatalf("Failed to start device plugin: %v", err)
	}

	/* Monitor file changes for kubelet socket file and termination signals */
	for {
		select {
		case sig := <-sigCh:
			switch sig {
			case syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGINT:
				glog.Infof("Received signal \"%v\", shutting down.", sig)
				for _, cdm := range cdms {
					cdm.Stop()
				}
				return
			}
			glog.Infof("Received signal \"%v\"", sig)

		case event := <-watcher.Events:
			glog.Infof("Kubelet change event in pluginpath %v", event)
			for _, cdm := range cdms {
				cdm.Stop()
			}
			cdms = nil
			if err := createPluginsForPools(); err != nil {
				panic("Failed to restart device plugin")
			}
		}
	}
}
