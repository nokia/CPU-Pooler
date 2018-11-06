# CPU Pooler for Kubernetes

CPU Pooler for Kubernetes is a solution for Kubernetes to manage predefined CPU pools in Kubernetes nodes. Two types of cpu pools are supported; exclusive and shared. Request from exclusive pool will allocate cpu(s) for the container with exclusive access. From the shared pool the container can request fractional cpus with one cpu unit matching to one thousandth of cpu (millicpu).

The core component of the CPU pooler is a cpu-device-plugin which is implemented as a standard Kubernetes device plugin. The plugin advertises cpus in the pools as consumable resources. The cpus allocated by the plugin are communicated to the container as environment variable containing a list cpus. The application can set cpu affinity according to the given cpu list.

For the case where application does not implement functionality to set the cpu affinity, the CPU pooler provides mechanism to set it on behalf of the application. This is enabled by configuring the application process information to the annotation field ofthe pod spec. A mutating admission controller webhook is provided to mutate the pod specification to include necessary information (mounts, environment variables etc.) for the cpu affinity setting. Setting of the cpu affinity is achieved by setting the container entry point (by the webhook) to a special program called process-starter which is provided by the CPU Pooler. This program will start all the processes configured in the annotation field and sets the cpu affinity as configured.

The process-starter binary has to be installed to host file system in `/opt/bin` directory. A host volume mount to that directory is added to container.

## Configuration

### CPU pools

CPU pools are configured with a configMap named cpu-dp-configmap. The schema of configMap is as follows:
```
apiVersion: v1
kind: ConfigMap
metadata:
  name: cpu-dp-configmap
data:
  poolconfig.yaml: |
    resourceBaseName: <name>
    pools:
      <poolname1>:
        cpus : "<list of cpus"
        pooltype: "exclusive|shared"
      <poolname2>:
        cpus : "<list of cpus>"
        pooltype: "exclusive|shared"
```
The resurceBaseName is the advertised resource name without the resource - i.e only the `vendor-domain`. Pool name from the config will be the resource in the fully qualified resource name (`<resurceBaseName>/<pool name>`). In the deployment directory there is a sample pool config with two exclusive pools (both have two cpus) and one shared pool (one cpu).

### Pod spec

The cpu-device-plugin advertises the resources as name: `<resoruceBaseName>/<poolname>`. The poolname is pool name configured in cpu-dp-configmap. The cpus are requested in the resources section of container in the pod spec.

### Annotation:

Annotation schema is following and the name for the annotation is `<resourceBaseName>/cpus`. Resource being the the advertised resource name.
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

## Build

Mutating webhook

```
docker build --build-arg http_proxy=$http_proxy --build-arg https_proxy=$https_proxy -t cpu-device-webhook -f build/Dockerfile.webhook  .
```
The device plugin

```
docker build --build-arg http_proxy=$http_proxy --build-arg https_proxy=$https_proxy -t cpudp -f build/Dockerfile.cpudp  .
```

Process starter

```
CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' github.com/nokia/CPU-Pooler/cmd/process-starter
```
## Installation

Install process starter to host file system:

```
cp process-starter /opt/bin/
```

Create cpu-device-plugin config and daemonset:
```
kubectl create -f deployment/cpu-dp-config.yaml
kubectl create -f deployment/cpu-dev-ds.yaml
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
docker build -t busyloop test/
```

Start the test container:
```
$ kubectl create -f ./deployment/cpu-test.yaml
```
