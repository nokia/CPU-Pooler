# CPU Pooler for Kubernetes

[![Build Status](https://travis-ci.org/nokia/CPU-Pooler.svg?branch=master)](https://travis-ci.org/nokia/CPU-Pooler)

## Overview
CPU-Pooler for Kubernetes is a solution for Kubernetes to manage predefined, distinct CPU pools of Kubernetes Nodes, and physically separate the CPU resources of the containers connecting to the various pools.

Two explicit types of CPU pools are supported; exclusive and shared. If a container does not explicitly ask resources from these pools, it will belong to a third one, called default.

Request from exclusive pool will allocate CPU(s) for latency-sensitive containers requiring exclusive access.

Containers content with some, limited level of sharing, but still appreciating best-effort protection from noisy neighbours can request resources from the shared pool. Shared requests are fractional CPUs with one CPU unit exactly matching to one thousandth of a CPU core. (millicpu).

The default pool is synonymous to the Kubelet managed, by default existing shared pool. Worth noting that the shared and default pool's CPU sets, and characteristics are distinct.
By supporting the "original" CPU allocation method in parallel with the enhanced, 3rd party containers totally content with the default CPU management policy of Kubernetes can be instantiated on a CPU-Pooler upgraded system without any configuration changes.

## Topology awareness
### NUMA alignment
CPU-Pooler is officially NUMA/socket aware starting with the 0.4.0 release!

Moreover, unlike other external managers CPU-Pooler natively integrates into Kubernetes' own Topology Manager. CPU-Pooler reports the NUMA Node ID of all the CPUs belonging to an exclusive CPU pool to the upstream Topology Manager.
This means whenever a Pod requests resources from Kubernetes where topology matters -e.g. SR-IOV virtual functions, exclusive CPUs, GPUs etc.- Kubernetes will automatically assign resources with their NUMA node aligned - CPU-Pooler managed cores included!

This feature is automatic, therefore it does not require any configuration from the user.
For it to work though CPU-Pooler's version must be at least 0.4.0, while Kubernetes must be at least 1.17.X.

### Hyperthreading support
CPU-Pooler is able to recognize when it is deployed on a hyperthreading enabled node, and supports different thread allocation policies for exclusive CPU pools.

These policies are controlled by the "hyperThreadingPolicy" attribute of an exclusive pool. The following two, guaranteed policies are supported currently:
"singleThreaded" (default): when a physical core is assigned to a workload CPU-Pooler only includes the ID of the assigned core into the container's cpuset cgroup, and leaves all possible siblings un-assigned
"multiThreaded": when this policy is set Pooler automatically discovers all siblings of an assigned core, and allocates them together to the requesting container.

CPU-Pooler only implements guaranteed policies, meaning that siblings will never be accidentally assigned to neighbour containers.
Note: for HT support to work as intended you must only list phsyical core IDs in exclusive pool definitions. CPU-Pooler will automatically discover the siblings on its own.

## Components of the CPU-Pooler project
The CPU-Pooler project contains 4 core components:
- a Kubernetes standard Device Plugin seamlessly integrating the CPU pools to Kubernetes as schedulable resources
- a Kubernetes standard Informer making sure containers belonging to different CPU pools are always physically isolated from each other
- process starter binary capable of pinning specific processes to specific cores even within the confines of a container
- a mutating admission webhook for the Kubernetes core Pod API, validating and mutating CPU pool specific user requests 

The Device Plugin's job is to advertise the pools as consumable resources to Kubelet through the existing DPAPI. The CPUs allocated by the plugin are communicated to the container as environment variables containing a list of physical core IDs.
By default the application can set its processes CPU affinity according to the given CPU list, or can leave it up to the standard Linux Completely Fair Scheduler.

For the edge case where application does not implement functionality to set the CPU affinity of its processes, the CPU pooler provides mechanism to set it on behalf of the application.
This opt-in functionality is enabled by configuring the application process information to the annotation field of its Pod spec.
A mutating admission controller webhook is provided with the project to mutate the Pod's specification according to the needs of the starter binary (mounts, environment variables etc.).
The process-starter binary has to be installed to host file system in `/opt/bin` directory.

Lastly, the CPUSetter sub-component implements total physical separation of containers via Linux cpusets. This Informer constantly watches the Pod API of Kubernetes, and is triggered whenever a Pod is created, or changes its state(e.g. restarted etc.)
CPUSetter first calculates what is the appropriate cpuset for the container: the allocated CPUs in case of exclusive, the shared pool in case of shared, or the default in case the container did not explicitly ask for any pooled resources.
CPUSetter then provisions the calculated set into the relevant parameter of the container's cgroupfs filesystem (cpuset.cpus).
As CPUSetter is triggered by all Pods on all Nodes, we can be sure no containers can ever -even accidentally- access CPU resources not meant for them!  

## Using the allocated CPUs

By default CPU-Pooler only provisions the appropriate cpuset for a container based on its resource request, but does not intervene with how threads inside the container are scheduled between the allowed vCPUs.

For users who require explicitly pinning application process / thread(s) to the subset of the allocated CPUs for some reason, the following options are available:

**pod annotation**

If container has multiple processes and multiple exclusive CPUs are allocated or different pool types are used, pod annotation can be used to configure processes and CPU amounts for them.
In this case CPU Pooler takes care of pinning the processes to allocated -but only to the allocated- CPUs. This is suitable for single threaded processes running on exclusive CPUs. The container can also have process(es) running on shared CPUs

**use environment variables for pinning**

CPU Pooler sets allocated exclusive CPUs to environment variable `EXCLUSIVE_CPUS` and allocated shared CPUs to `SHARED_CPUS` environment variable. They contain CPU(s) as comma separated list. These variables can be used by the application to read the allocated CPU(s) and do pinning of threads / processes to the CPU(s).


In order to avoid possible race conditions occuring due to multiple processes trying to set affinity at the same time (or application trying to set it too early); CPU-Pooler can guarantee that the container's entrypoint is only executed once the proper CPU configuration has been provisioned.
This is achieved by the `process-starter` component under the following pre-conditions.
In order for this functionality to work as intended the `command` property must be configured in container's pod manifest. If the `command` is not configured, the `process-starer` won't be used because it is not known which process needs to be started in the container.
In such cases we fall back to the native Linux thread scheduling mechanism, but depending on user activity this might result in exotic race conditions occuring.

## Configuration

### Kubelet
Kubelet treats Devices transparently, therefore it will never realize the CPU-Pooler managed resources are actually CPUs.
In order to avoid double bookkeeping of CPU resources in the cluster, every Node's Kubelet hosting the CPU-Pooler Device Plugin should be configured according to the following formula:
--system-reserved = <TOTAL_CPU_CAPACITY> - SIZEOF(DEFAULT_POOL) + <DEFAULT_SYSTEM_RESERVED> 
This setting effectively tells Kubelet to discount the capacity belonging to the CPU-Pooler managed shared and exclusive pools.

Besides that, please note that Kubelet's inbuilt CPU Manager needs to be disabled on the Nodes which run CPU-Pooler to avoid overwriting CPU-Pooler's more fine-grained cpuset configuration.

### CPU pools

CPU pools are configured with a configMap named cpu-pooler-configmap. The schema of configMap is as follows:
```
apiVersion: v1
kind: ConfigMap
metadata:
  name: cpu-pooler-configmap
data:
  poolconfig-<name>.yaml: |
    pools:
      exclusive_<poolname1>:
        cpus : "<list of physical CPU core IDs>"
        hyperThreadingPolicy: singleThreaded
      exclusive_<poolname2>:
        cpus : "<list of physical CPU core IDs>"
        hyperThreadingPolicy: multiThreaded
      shared_<poolname3>:
        cpus : "<list of CPU thread IDs>"
      default:
        cpus : "<list of CPU thread IDs>"
      nodeSelector:
        <key> : <value>
```
The poolconfig-<name>.yaml file must exist in the data section.
The CPU pools are defined in poolconfig-<name>.yaml files. There must be at least one poolconfig-<name>.yaml file in the data section.
Pool name from the config will be the resource in the fully qualified resource name (`nokia.k8s.io/<pool name>`). The pool name must have pool type prefix - 'exclusive' for exclusive CPU pool or 'shared' for shared CPU pool.
A CPU pool not having either of these special prefixes is considered as the cluster-wide 'default' CPU pool, and as such, CPU cores belonging to this pool will not be advertised to the Device Manager as schedulable resources.


"cpus" attribute controls which CPU cores belong to this pool. Standard Linux notation including "," and "-" characters is accepted.
For exclusive pools only configure physical CPU core IDs.
For shared and default pools list all the thread IDs you want to be included in the pool (i.e. physical and HT sibling IDs both).


"hyperThreadingPolicy" controls whether exclusive CPU cores are allocated alone ("singleThreaded"), or in pairs ("multiThreaded").


The nodeSelector is used to tell which node the pool configuration file belongs to. CPU pooler and CPUSetter components both read the node labels and select the config that matches the value of nodeSelector.


In the deployment directory there is a sample pool config with two exclusive pools (both have two cpus) and one shared pool (one cpu). Nodes for the pool configurations are selected by `nodeType` label.
Please note: currently only one shared pool is supported per Node!
### Pod spec

The cpu-device-plugin advertises the resources of exclusive, and shared CPU pools as name: `nokia.k8s.io/<poolname>`. The poolname is pool name configured in cpu-pooler-configmap. The cpus are requested in the resources section of container in the pod spec.

### Annotation:

Annotation schema is following and the name for the annotation is `nokia.k8s.io/cpus`. Resource being the the advertised resource name.
```
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "array",
  "items": {
    "$ref": "#/definitions/container"
  },
  "definitions": {
    "container": {
      "type": "object",
      "required": [
        "container",
        "processes"
      ],
      "properties": {
        "container": {
          "type": "string"
        },
        "processes": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/process"
          }
        }
      }
    },
    "process": {
      "type": "object",
      "required": [
        "process",
        "args",
        "pool",
        "cpus"
      ],
      "properties": {
        "process": {
          "type": "string"
        },
        "args": {
          "type": "array",
          "items": {
            "type": "string"
          }
        },
        "pool": {
          "type": "string"
        },
        "cpus": {
          "type": "number"
        }
      }
    }
  }
}
```
An example is provided in cpu-test.yaml pod manifest in the deployment folder.

### Restrictions

Following restrictions apply when allocating cpu from pools and configuring pools:

* There can be only one shared pool in the node
* Resources belonging to the default pool are not advertised. The default pool definition is only used by the CPUSetter component

## Build
Mutating webhook

```
$ docker build --build-arg http_proxy=$http_proxy --build-arg https_proxy=$https_proxy -t cpu-device-webhook -f build/Dockerfile.webhook  .
```
The device plugin

```
$ docker build --build-arg http_proxy=$http_proxy --build-arg https_proxy=$https_proxy -t cpudp -f build/Dockerfile.cpudp  .
```

Process starter

```
$ go mod download
$ CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' github.com/nokia/CPU-Pooler/cmd/process-starter
```

CPUSetter

```
$ docker build --build-arg http_proxy=$http_proxy --build-arg https_proxy=$https_proxy -t cpusetter -f build/Dockerfile.cpusetter  .
```
## Installation

Install process starter to host file system:

```
$ cp process-starter /opt/bin/
```

Create cpu-device-plugin config and daemonset:
```
$ kubectl create -f deployment/cpu-pooler-config.yaml
$ kubectl create -f deployment/cpu-dev-ds.yaml
```
There is a helper script ```./scripts/generate-cert.sh``` that generates certificate and key for the webhook admission controller. The script ```deployment/create-webhook-conf.sh``` can be used to create the webhook configuration from the provided manifest file (```webhook-conf.yaml```).

Create CPUSetter daemonset:
```
$ kubectl create -f deployment/cpusetter-ds.yaml
```

Following steps create the webhook server with necessary configuration (including the certifcate and key)
```
$ ./scripts/generate-cert.sh
$ 
$ kubectl create -f deployment/webhook-svc-depl.yaml
$ deployment/create-webhook-conf.sh deployment/webhook-conf.yaml
```
The cpu-device-plugin with webhook should be running now. Test the installation with a cpu test pod. First create image for the test container:
```
$ docker build -t busyloop test/
```

Start the test container:
```
$ kubectl create -f ./deployment/cpu-test.yaml
```

Upon instantiation you can observe that the cpuset.cpus parameter of the created containers' cpuset cgroupfs hierarchy are set to right values:
- to the configured cpuset belonging to the shared pool for sharedtestcontainer
- to the a cpuset containing only the ID of the 2 chosen cores for exclusivetestcontainer
- to the configured cpuset belonging to the default pool for defaulttestcontainer


## License

This project is licensed under the BSD-3-Clause license - see the [LICENSE](https://github.com/nokia/CPU-Pooler/blob/master/LICENSE).
