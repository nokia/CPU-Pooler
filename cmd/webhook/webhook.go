package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/nokia/CPU-Pooler/pkg/types"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)
var resourceBaseName = "nokia.k8s.io"
var processStarterPath string

type containerPoolRequests struct {
	sharedCPURequests    int
	exclusiveCPURequests bool
	pools                map[string]int
}
type poolRequestMap map[string]containerPoolRequests

type patch struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

func toAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
		Allowed: false,
	}
}

func getCPUPoolRequests(pod *corev1.Pod) (poolRequestMap, error) {
	var poolRequests = make(poolRequestMap)
	for _, c := range pod.Spec.Containers {
		cPoolRequests, exists := poolRequests[c.Name]
		if !exists {
			cPoolRequests.pools = make(map[string]int)
		}
		for key, value := range c.Resources.Limits {
			if strings.HasPrefix(string(key), resourceBaseName) {
				//convert back from human readable format
				val, err := strconv.Atoi(strings.Replace(value.String(), "k", "000", 1))
				if err != nil {
					glog.Errorf("Cannot convert cpu request to int %s:%s", key, value.String())
					return poolRequestMap{}, err
				}
				if strings.HasPrefix(string(key), resourceBaseName+"/shared") {
					cPoolRequests.sharedCPURequests += val
				}
				if strings.HasPrefix(string(key), resourceBaseName+"/exclusive") {
					cPoolRequests.exclusiveCPURequests = true
				}
				poolName := strings.TrimPrefix(string(key), resourceBaseName+"/")
				cPoolRequests.pools[poolName] = val
				poolRequests[c.Name] = cPoolRequests
			}
		}
	}
	return poolRequests, nil
}

func annotationNameFromConfig() string {
	return resourceBaseName + "/cpus"

}

func validateAnnotation(poolRequests poolRequestMap, cpuAnnotation types.CPUAnnotation) error {
	for _, cName := range cpuAnnotation.Containers() {
		for _, pool := range cpuAnnotation.ContainerPools(cName) {
			cPoolRequests, exists := poolRequests[cName]
			if !exists {
				return fmt.Errorf("Container %s has no pool requests in pod spec",
					cName)
			}
			if cpuAnnotation.ContainerSharedCPUTime(cName) != cPoolRequests.sharedCPURequests {
				return fmt.Errorf("Shared CPU requests %d do not match to annotation %d",
					cPoolRequests.sharedCPURequests,
					cpuAnnotation.ContainerSharedCPUTime(cName))
			}
			value, exists := cPoolRequests.pools[pool]
			if !exists {
				return fmt.Errorf("Container %s; Pool %s in annotation not found from resources", cName, pool)
			}
			if cpuAnnotation.ContainerTotalCPURequest(pool, cName) != value {
				return fmt.Errorf("Exclusive CPU requests %d do not match to annotation %d",
					cPoolRequests.pools[pool],
					cpuAnnotation.ContainerTotalCPURequest(pool, cName))
			}

		}
	}
	return nil
}

func patchCPULimit(sharedCPUTime int, patchList []patch, i int, c *corev1.Container) []patch {
	var patchItem patch

	patchItem.Op = "replace"
	cpuVal := `"` + strconv.Itoa(sharedCPUTime) + `m"`
	patchItem.Path = "/spec/containers/" + strconv.Itoa(i) + "/resources/limits/cpu"
	patchItem.Value = json.RawMessage(cpuVal)
	patchList = append(patchList, patchItem)

	patchItem.Op = "replace"
	cpuVal = `"0m"`
	patchItem.Path = "/spec/containers/" + strconv.Itoa(i) + "/resources/requests/cpu"
	patchItem.Value = json.RawMessage(cpuVal)
	patchList = append(patchList, patchItem)

	return patchList
}

func patchContainerEnv(poolRequests poolRequestMap, envPatched bool, patchList []patch, i int, c *corev1.Container) ([]patch, error) {
	var patchItem patch
	var poolStr string

	if poolRequests[c.Name].exclusiveCPURequests && poolRequests[c.Name].sharedCPURequests > 0 {
		poolStr = types.ExclusivePoolID + "&" + types.SharedPoolID
	} else if poolRequests[c.Name].exclusiveCPURequests{
		poolStr = types.ExclusivePoolID
	} else if poolRequests[c.Name].sharedCPURequests > 0 {
		poolStr = types.SharedPoolID
	} else {
		poolStr = types.DefaultPoolID
	}
	patchItem.Op = "add"
	cpuPoolEnvPatch := `{"name":"CPU_POOLS","value":"` + poolStr + `" }`
	patchItem.Path = "/spec/containers/" + strconv.Itoa(i) + "/env"
	if envPatched || len(c.Env) > 0 {
		patchItem.Path += "/-"
	} else {
		cpuPoolEnvPatch = `[` + cpuPoolEnvPatch + `]`
	}
	patchItem.Value = json.RawMessage(cpuPoolEnvPatch)
	patchList = append(patchList, patchItem)

	return patchList, nil
}

func patchContainerForPinning(cpuAnnotation types.CPUAnnotation, patchList []patch, i int, c *corev1.Container) ([]patch, error) {
	var patchItem patch

	glog.V(2).Infof("Adding CPU pinning patches")

	// podinfo volumeMount
	patchItem.Op = "add"
	patchItem.Path = "/spec/containers/" + strconv.Itoa(i) + "/volumeMounts/-"
	patchItem.Value =
		json.RawMessage(`{"name":"podinfo","mountPath":"/etc/podinfo","readOnly":true}`)
	patchList = append(patchList, patchItem)

	// hostbin volumeMount. Location for process starter binary
	patchItem.Path = "/spec/containers/" + strconv.Itoa(i) + "/volumeMounts/-"
	contVolumePatch := `{"name":"hostbin","mountPath":"` + processStarterPath + `","readOnly":true}`
	patchItem.Value =
		json.RawMessage(contVolumePatch)
	patchList = append(patchList, patchItem)

	// Container name to env variable
	contNameEnvPatch := `{"name":"CONTAINER_NAME","value":"` + c.Name + `" }`
	patchItem.Path = "/spec/containers/" + strconv.Itoa(i) + "/env"
	if len(c.Env) > 0 {
		patchItem.Path += "/-"
	} else {
		contNameEnvPatch = `[` + contNameEnvPatch + `]`
	}
	patchItem.Value = json.RawMessage(contNameEnvPatch)
	patchList = append(patchList, patchItem)

	// Overwrite entrypoint
	patchItem.Path = "/spec/containers/" + strconv.Itoa(i) + "/command"
	contEPPatch := `[ "` + processStarterPath + `" ]`
	patchItem.Value = json.RawMessage(contEPPatch)
	patchList = append(patchList, patchItem)

	// Put command to args if pod cpu annotation does not exist for the container
	if len(c.Command) > 0 && !cpuAnnotation.ContainerExists(c.Name) {
		patchItem.Path = "/spec/containers/" + strconv.Itoa(i) + "/args"
		args := `[ "` + strings.Join(c.Command, "\",\"") + `" `
		if len(c.Args) > 0 {
			args += `,"` + strings.Join(c.Args, "\",\"") + `"`
		}
		args += `]`
		patchItem.Value = json.RawMessage(args)
		patchList = append(patchList, patchItem)
	}

	return patchList, nil
}

func patchVolumesForPinning(patchList []patch) []patch {
	var patchItem patch
	patchItem.Op = "add"

	// podinfo volume
	patchItem.Path = "/spec/volumes/-"
	patchItem.Value = json.RawMessage(`{"name":"podinfo","downwardAPI": { "items": [ { "path" : "annotations","fieldRef":{ "fieldPath": "metadata.annotations"} } ] } }`)
	patchList = append(patchList, patchItem)
	// hostbin volume
	patchItem.Path = "/spec/volumes/-"
	volumePathPatch := `{"name":"hostbin","hostPath":{ "path":"` + processStarterPath + `"} }`
	patchItem.Value = json.RawMessage(volumePathPatch)
	patchList = append(patchList, patchItem)

	return patchList

}

func mutatePods(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	glog.V(2).Info("mutating pods")
	var (
		patchList         []patch
		err               error
		cpuAnnotation     types.CPUAnnotation
		pinningPatchAdded bool
	)

	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if ar.Request.Resource != podResource {
		glog.Errorf("expect resource to be %s", podResource)
		return nil
	}

	raw := ar.Request.Object.Raw
	pod := corev1.Pod{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err = deserializer.Decode(raw, nil, &pod); err != nil {
		glog.Error(err)
		return toAdmissionResponse(err)
	}
	reviewResponse := v1beta1.AdmissionResponse{}

	annotationName := annotationNameFromConfig()

	reviewResponse.Allowed = true

	podAnnotation, podAnnotationExists := pod.ObjectMeta.Annotations[annotationName]

	poolRequests, err := getCPUPoolRequests(&pod)
	if err != nil {
		glog.Errorf("Failed to get pod cpu pool requests: %v", err)
		return toAdmissionResponse(err)
	}

	if podAnnotationExists {
		cpuAnnotation = types.NewCPUAnnotation()

		err = cpuAnnotation.Decode([]byte(podAnnotation))
		if err != nil {
			glog.Errorf("Failed to decode pod annotation %v", err)
			return toAdmissionResponse(err)
		}
		if err = validateAnnotation(poolRequests, cpuAnnotation); err != nil {
			glog.Error(err)
			return toAdmissionResponse(err)
		}
	}

	// Patch container if needed.
	for i, c := range pod.Spec.Containers {
		// Set CPU limit if shared CPU were requested and exclusive CPUs were not requested
		containerEnvPatched := false
		if poolRequests[c.Name].sharedCPURequests > 0 &&
			!poolRequests[c.Name].exclusiveCPURequests {
			patchList = patchCPULimit(poolRequests[c.Name].sharedCPURequests,
				patchList, i, &c)
		}
		// If pod annotation has entry for this container or
		// container asks for exclusive cpus, we add patches to enable pinning.
		// The patches enable process in container to be started with cpu pooler's 'process starter'
		// The cpusetter sets cpuset for the container and that needs to be completed
		// before application container is started. If cpuset is set after the application
		// has started, the cpu affinity setting by application will be overwritten by the cpuset.
		// The process starter will wait for cpusetter to finish it's job for this container
		// and starts the application process after that.
		pinningPatchNeeded := cpuAnnotation.ContainerExists(c.Name)
		if poolRequests[c.Name].exclusiveCPURequests {
			if len(c.Command) == 0 && !pinningPatchNeeded {
				glog.Warningf("Container %s asked exclusive cpus but command not given. CPU affinity settings possibly lost for container", c.Name)
			} else {
				pinningPatchNeeded = true
			}
		}
		if pinningPatchNeeded {
			glog.V(2).Infof("Patch container for pinning %s", c.Name)

			patchList, err = patchContainerForPinning(cpuAnnotation, patchList, i, &c)
			if err != nil {
				return toAdmissionResponse(err)
			}
			pinningPatchAdded = true
			containerEnvPatched = true
		}
		if poolRequests[c.Name].sharedCPURequests > 0 ||
			 poolRequests[c.Name].exclusiveCPURequests {
				// Patch container environment variable
				patchList, err = patchContainerEnv(poolRequests, containerEnvPatched, patchList, i, &c)
				if err != nil {
					return toAdmissionResponse(err)
				}
		}
	}
	// Add volumes if any container was patched for pinning
	if pinningPatchAdded {
		patchList = patchVolumesForPinning(patchList)
	} else if podAnnotationExists {
		glog.Errorf("CPU annotation exists but no container was patched %v:%v",
			cpuAnnotation, pod.Spec.Containers)
		return toAdmissionResponse(errors.New("CPU Annotation error"))
	}

	if len(patchList) > 0 {
		patch, err := json.Marshal(patchList)
		if err != nil {
			glog.Errorf("Patch marshall error %v:%v", patchList, err)
			reviewResponse.Allowed = false
			return toAdmissionResponse(err)
		}
		reviewResponse.Patch = []byte(patch)
		pt := v1beta1.PatchTypeJSONPatch
		reviewResponse.PatchType = &pt
	}

	return &reviewResponse
}

func serveMutatePod(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	// verify the content type is accurate
	contentType := r.Header.Get("Content-Type")
	if contentType != "application/json" {
		glog.Errorf("contentType=%s, expect application/json", contentType)
		return
	}

	requestedAdmissionReview := v1beta1.AdmissionReview{}

	responseAdmissionReview := v1beta1.AdmissionReview{}

	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(body, nil, &requestedAdmissionReview); err != nil {
		glog.Error(err)
		responseAdmissionReview.Response = toAdmissionResponse(err)
	} else {
		responseAdmissionReview.Response = mutatePods(requestedAdmissionReview)
	}

	responseAdmissionReview.Response.UID = requestedAdmissionReview.Request.UID

	respBytes, err := json.Marshal(responseAdmissionReview)

	if err != nil {
		glog.Error(err)
	}
	w.Header().Set("Content-Type", "application/json")

	if _, err := w.Write(respBytes); err != nil {
		glog.Error(err)
	}
}

func main() {
	var certFile string
	var keyFile string

	flag.StringVar(&certFile, "tls-cert-file", certFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	flag.StringVar(&keyFile, "tls-private-key-file", keyFile, ""+
		"File containing the default x509 private key matching --tls-cert-file.")
	flag.StringVar(&processStarterPath, "process-starter-path", "/opt/bin/process-starter", ""+
		"Path to process-starter binary file. Optional parameter, default path is /opt/bin/process-starter.")

	flag.Parse()

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		glog.Fatal(err)
	}

	http.HandleFunc("/mutating-pods", serveMutatePod)
	server := &http.Server{
		Addr:         ":443",
		TLSConfig:    &tls.Config{Certificates: []tls.Certificate{cert}},
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	server.ListenAndServeTLS("", "")
}
