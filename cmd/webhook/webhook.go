/*
Based on

*/

package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
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
	"time"
)

var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)

type Patch struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

func toAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

func containersToPatchFromAnnotation(annotation []byte) ([]string, error) {
	var cpuAnnotation []types.Container
	var containersToPatch []string

	err := json.Unmarshal(annotation, &cpuAnnotation)
	if err != nil {
		glog.Error(err)
		return nil, err
	}

	for _, cont := range cpuAnnotation {
		containersToPatch = append(containersToPatch, cont.Name)
	}
	return containersToPatch, nil

}

func isContainerPatchNeeded(containerName string, containersToPatch []string) bool {
	for _, name := range containersToPatch {
		if name == containerName {
			return true
		}
	}
	return false
}

func annotationNameFromConfig() (string, error) {
	poolConf, err := types.ReadPoolConfig()
	if err != nil {
		glog.Error("Could not read poolconfig %v", err)
		return "", err
	}
	return poolConf.ResourceBaseName + "/cpus", nil

}

func mutatePods(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	glog.V(2).Info("mutating pods")
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	if ar.Request.Resource != podResource {
		glog.Errorf("expect resource to be %s", podResource)
		return nil
	}

	raw := ar.Request.Object.Raw
	pod := corev1.Pod{}
	deserializer := codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
		glog.Error(err)
		return toAdmissionResponse(err)
	}

	reviewResponse := v1beta1.AdmissionResponse{}
	annotationName, err := annotationNameFromConfig()
	if err != nil {
		reviewResponse.Allowed = false
		return &reviewResponse
	}
	reviewResponse.Allowed = true
	if annotation, exists := pod.ObjectMeta.Annotations[annotationName]; exists {
		glog.V(2).Infof("mutatePod : Annotation %v", annotation)
		var patchList []Patch
		var patchItem Patch
		containersToPatch, err := containersToPatchFromAnnotation([]byte(annotation))
		if err != nil {
			glog.Errorf("Patch containers %v", err)
			return toAdmissionResponse(err)
		}
		glog.V(2).Infof("Patch containers %v", containersToPatch)
		for i, c := range pod.Spec.Containers {
			if !isContainerPatchNeeded(c.Name, containersToPatch) {
				continue
			}
			glog.V(2).Infof("Adding patches")

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
				json.RawMessage(`{"name":"cpu-dp-config","mountPath":"/etc/cpu-dp","readOnly":true}`)
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

		}

		if len(patchList) > 0 {
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
			patchItem.Value = json.RawMessage(`{"name":"cpu-dp-config","configMap":{ "name":"cpu-dp-configmap"} }`)
			patchList = append(patchList, patchItem)

			patch, _ := json.Marshal(patchList)
			reviewResponse.Patch = []byte(patch)
			pt := v1beta1.PatchTypeJSONPatch
			reviewResponse.PatchType = &pt
		}

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
	glog.V(2).Infof("Data  %s", body)
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
		panic(1)
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
