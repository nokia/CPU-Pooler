# CPU Pooler for Kubernetes

[![Build Status](https://travis-ci.org/nokia/CPU-Pooler.svg?branch=master)](https://travis-ci.org/nokia/CPU-Pooler)

## Overview
CPU Pooler for Kubernetes is a solution for Kubernetes to manage predefined CPU pools in Kubernetes nodes. Two types of cpu pools are supported; exclusive and shared. Request from exclusive pool will allocate cpu(s) for the container with exclusive access. From the shared pool the container can request fractional cpus with one cpu unit matching to one thousandth of cpu (millicpu).

The core component of the CPU pooler is a cpu-device-plugin which is implemented as a standard Kubernetes device plugin. The plugin advertises cpus in the pools as consumable resources. The cpus allocated by the plugin are communicated to the container as environment variable containing a list cpus. The application can set cpu affinity according to the given cpu list.

For the case where application does not implement functionality to set the cpu affinity, the CPU pooler provides mechanism to set it on behalf of the application. This is enabled by configuring the application process information to the annotation field ofthe pod spec. A mutating admission controller webhook is provided to mutate the pod specification to include necessary information (mounts, environment variables etc.) for the cpu affinity setting. Setting of the cpu affinity is achieved by setting the container entry point (by the webhook) to a special program called process-starter which is provided by the CPU Pooler. This program will start all the processes configured in the annotation field and sets the cpu affinity as configured.

The process-starter binary has to be installed to host file system in `/opt/bin` directory. A host volume mount to that directory is added to container.

## Configuration

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
      <poolname1>:
        cpus : "<list of cpus"
      <poolname2>:
        cpus : "<list of cpus>"
      nodeSelector:
        <key> : <value>
```
The cpu-pooler.yaml file must exist in the data section.
The cpu pools are defined in poolconfig-<name>.yaml files. There must be at least one poolconfig-<name>.yaml file in the data section.
Pool name from the config will be the resource in the fully qualified resource name (`nokia.k8s.io/<pool name>`). The pool name must have pool type prefix - 'exclusive' for exclusive cpu pool or 'shared' for shared cpu pool.
A CPU pool not having either of these special prefixes is considered as the cluster-wide 'default' CPU pool, and as such, CPU cores belonging to this pool will not be advertised to the Device Manager as schedulable resources.
The nodeSelector is used to tell in which node this pool configuration is used. CPU pooler reads the node labels and selects the config that matches the nodeSelector.

In the deployment directory there is a sample pool config with two exclusive pools (both have two cpus) and one shared pool (one cpu). Nodes for the pool configurations are selected by `nodeType` label.

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
* Containter can ask cpus from one type of pool only (shared or exclusive)
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
$ dep ensure
$ CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' github.com/nokia/CPU-Pooler/cmd/process-starter
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
