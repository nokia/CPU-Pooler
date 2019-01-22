package main

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/golang/glog"
	"github.com/nokia/CPU-Pooler/internal/types"
	"io/ioutil"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)
var poolerConf *types.PoolerConfig

type containerPoolRequests struct {
	sharedCPURequests int
	pools             map[string]int
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

func isPinningPatchNeeded(containerName string, containersToPatch []string) bool {
	for _, name := range containersToPatch {
		if name == containerName {
			return true
		}
	}
	return false
}
func getCPUPoolRequests(pod *corev1.Pod) (poolRequestMap, error) {
	var poolRequests = make(poolRequestMap)
	for _, c := range pod.Spec.Containers {
		cPoolRequests, exists := poolRequests[c.Name]
		if !exists {
			cPoolRequests.pools = make(map[string]int)
		}
		var sharedFound, exclusiveFound bool
		for key, value := range c.Resources.Limits {
			if strings.HasPrefix(string(key), poolerConf.ResourceBaseName) {

				val, err := strconv.Atoi(value.String())
				if err != nil {
					glog.Errorf("Cannot convert cpu request to int %s:%s", key, value.String())
					return poolRequestMap{}, err
				}
				if strings.HasPrefix(string(key), poolerConf.ResourceBaseName+"/shared") {
					cPoolRequests.sharedCPURequests += val
					sharedFound = true
				}
				if strings.HasPrefix(string(key), poolerConf.ResourceBaseName+"/exclusive") {
					exclusiveFound = true
				}
				poolName := strings.TrimPrefix(string(key), poolerConf.ResourceBaseName+"/")
				cPoolRequests.pools[poolName] = val
				poolRequests[c.Name] = cPoolRequests
			}
		}
		if sharedFound && exclusiveFound {
			return poolRequestMap{}, fmt.Errorf("Only one type of pool is allowed for a container")
		}
	}
	return poolRequests, nil
}

func annotationNameFromConfig() string {
	return poolerConf.ResourceBaseName + "/cpus"

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
	patchItem.Op = "add"

	cpuVal := `"` + strconv.Itoa(sharedCPUTime) + `m"`
	if len(c.Resources.Limits) > 0 {
		patchItem.Path = "/spec/containers/" + strconv.Itoa(i) + "/resources/limits/cpu"
		patchItem.Value =
			json.RawMessage(cpuVal)
	} else {
		patchItem.Path = "/spec/containers/" + strconv.Itoa(i) + "/resources"
		patchItem.Value = json.RawMessage(`{ "limits": { "cpu":` + cpuVal + ` } }`)
	}
	patchList = append(patchList, patchItem)
	return patchList

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
	patchItem.Value =
		json.RawMessage(`{"name":"hostbin","mountPath":"/opt/bin","readOnly":true}`)
	patchList = append(patchList, patchItem)

	//  device plugin config volumeMount.
	patchItem.Path = "/spec/containers/" + strconv.Itoa(i) + "/volumeMounts/-"
	patchItem.Value =
		json.RawMessage(`{"name":"cpu-pooler-config","mountPath":"/etc/cpu-pooler","readOnly":true}`)
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
	patchItem.Value = json.RawMessage(`[ "/opt/bin/process-starter" ]`)
	patchList = append(patchList, patchItem)

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
	patchItem.Value = json.RawMessage(`{"name":"hostbin","hostPath":{ "path":"/opt/bin"} }`)
	patchList = append(patchList, patchItem)

	// cpu dp configmap volume
	patchItem.Path = "/spec/volumes/-"
	patchItem.Value = json.RawMessage(`{"name":"cpu-pooler-config","configMap":{ "name":"cpu-pooler-configmap"} }`)
	patchList = append(patchList, patchItem)
	return patchList

}

func mutatePods(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	glog.V(2).Info("mutating pods")
	var (
		patchList         []patch
		err               error
		cpuAnnotation     types.CPUAnnotation
		containersToPatch []string
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
		cpuAnnotation = types.CPUAnnotation{}

		err = cpuAnnotation.Decode([]byte(podAnnotation))
		if err != nil {
			glog.Errorf("Failed to decode pod annotation %v", err)
			return toAdmissionResponse(err)
		}
		containersToPatch = cpuAnnotation.Containers()
		if err = validateAnnotation(poolRequests, cpuAnnotation); err != nil {
			glog.Error(err)
			return toAdmissionResponse(err)
		}
		glog.V(2).Infof("Patch containers for pinning %v", containersToPatch)
	}

	// Patch container if needed.
	for i, c := range pod.Spec.Containers {
		if poolRequests[c.Name].sharedCPURequests > 0 {
			patchList = patchCPULimit(poolRequests[c.Name].sharedCPURequests,
				patchList, i, &c)
		}
		if isPinningPatchNeeded(c.Name, containersToPatch) {
			patchList, err = patchContainerForPinning(cpuAnnotation, patchList, i, &c)
			if err != nil {
				return toAdmissionResponse(err)
			}
			pinningPatchAdded = true
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

	flag.Parse()

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		glog.Fatal(err)
	}

	poolerConf, err = types.ReadPoolerConfig()
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
