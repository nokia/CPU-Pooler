# CPU Pooler for Kubernetes

[![Build Status](https://travis-ci.org/nokia/CPU-Pooler.svg?branch=master)](https://travis-ci.org/nokia/CPU-Pooler)

## Overview
CPU Pooler for Kubernetes is a solution for Kubernetes to manage predefined, distinct CPU pools of Kubernetes Nodes, and physically separate the CPU resources of the containers connecting to the various pools.

Two explicit types of CPU pools are supported; exclusive and shared. If a container does not explicitly ask resources from these pools, it will belong to the default pool.

Request from exclusive pool will allocate CPU(s) for latency-sensitive containers requiring exclusive access.

Containers content with some, limited level of sharing, but still appreciating best-effort protection from noisy neighbours can request resources from the shared pool. Shared requests are fractional CPUs with one CPU unit exactly matching to one thousandth of a CPU core. (millicpu).

The default pool is synonymous to the Kubelet managed, by default existing shared pool. Worth noting that the shared and default pool's CPU sets, and characteristics are distinct.
By supporting the "original" CPU allocation method in parallel with the enhanced, 3rd party containers totally content with the default CPU management policy of Kubernetes can be instantiated on a CPU-Pooler upgraded system without any configuration changes.

## Components of the CPU-Pooler project
The CPU-Pooler project contains 4 core components:
- a Kubernetes standard Device Plugin seamlessly integrating the CPU pools to Kubernetes as schedulable resources
- a Kubernetes standard Informer making sure containers belonging to different CPU pools are always physically isolated from each other
- process starter binary capable of pinning specific processes to specific cores even within the confines of a container
- a mutating admission webhook for the Kubernetes core Pod API, validating and mutating CPU pool specific user requests 

The Device Plugin's job is to advertise the pools as consumable resources to Kubelet through the existing DPAPI. The cpus allocated by the plugin are communicated to the container as environment variables containing a list of physical core IDs.
By default the application can set its processes CPU affinity according to the given cpu list, or can leave it up to the standard Linux Completely Fair Scheduler.

For the edge case where application does not implement functionality to set the cpu affinity of its processes, the CPU pooler provides mechanism to set it on behalf of the application.
This opt-in functionality is enabled by configuring the application process information to the annotation field of its Pod spec.
A mutating admission controller webhook is provided with the project to mutate the Pod's specification according to the needs of the starter binary (mounts, environment variables etc.).
The process-starter binary has to be installed to host file system in `/opt/bin` directory.

Lastly, the CPUSetter sub-component implements total physical separation of containers via Linux cpusets. This Informer constantly watches the Pod API of Kubernetes, and is triggered whenever a Pod is created, or changes its state(e.g. restarted etc.)
CPUSetter first calculates what is the appropriate cpuset for the container: the allocated CPUs in case of exclusive, the shared pool in case of shared, or the default in case the container did not explicitly ask for any pooled resources.
CPUSetter then provisions the calculated set into the relevant parametet of the container's cgroupfs filesystem (cpuset.cpus).
As CPUSetter is triggered by all Pods on all Nodes, we can be sure no containers can ever -even accidentally- access CPU resources not meant for them!  

## Configuration

### Kubelet
Kubelet treats Devices transparently, therefore it will never realize the CPU-Pooler managed resources are actually CPUs.
In order to avoid double bookkeeping of CPU resources in the cluster, every Node's Kubelet hosting the CPU-Pooler Device Plugin should be configured according to the following formula:
--system-reserved = <TOTAL_CPU_CAPACITY> - SIZEOF(DEFAULT_POOL) + <DEFAULT_SYSTEM_RESERVED> 
This setting effectively tells Kubelet to discount the capacity belonging to the CPU-Pooler managed shared and exclusive pools.

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
        cpus : "<list of cpus"
      exclusive_<poolname2>:
        cpus : "<list of cpus"
      shared_<poolname3>:
        cpus : "<list of cpus>"
      default:
        cpus : "<list of cpus>"
      nodeSelector:
        <key> : <value>
```
The cpu-pooler.yaml file must exist in the data section.
The cpu pools are defined in poolconfig-<name>.yaml files. There must be at least one poolconfig-<name>.yaml file in the data section.
Pool name from the config will be the resource in the fully qualified resource name (`nokia.k8s.io/<pool name>`). The pool name must have pool type prefix - 'exclusive' for exclusive cpu pool or 'shared' for shared cpu pool.
A CPU pool not having either of these special prefixes is considered as the cluster-wide 'default' CPU pool, and as such, CPU cores belonging to this pool will not be advertised to the Device Manager as schedulable resources.
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
* Containter can ask cpus from one type of pool only (shared, exclusive, or default)
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
$ dep ensure --vendor-only
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
There is a helper script ```./scripts/generate_cert.sh``` that generates certificate and key for the webhook admission controller. The script ```deployment/create-webhook-conf.sh``` can be used to create the webhook configuration from the provided manifest file (```webhook-conf.yaml```).

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