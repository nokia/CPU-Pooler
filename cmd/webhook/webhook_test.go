package main

import (
	"bytes"
	"encoding/json"
	"github.com/nokia/CPU-Pooler/internal/types"
	"io/ioutil"
	"k8s.io/api/admission/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func init() {
	types.PoolerConfigDir = "../../test/testdata/cpu-pooler"
	var err error
	poolerConf, err = types.ReadPoolerConfig()
	if err != nil {
		panic(err)
	}

}
func createAdmReviewReq(t *testing.T, containers []corev1.Container) []byte {
	pod := corev1.Pod{}
	pod.Spec.Containers = make([]corev1.Container, 0)
	for _, c := range containers {
		pod.Spec.Containers = append(pod.Spec.Containers, c)
	}
	podjs, err := json.MarshalIndent(&pod, "", "   ")
	if err != nil {
		t.FailNow()
	}
	admReviewReq := v1beta1.AdmissionReview{}
	admReq := v1beta1.AdmissionRequest{}
	admReq.Object.Raw = podjs
	admReq.Resource = metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	admReviewReq.Request = &admReq
	ar, err := json.MarshalIndent(&admReviewReq, "", "   ")
	if err != nil {
		t.FailNow()
	}
	return ar
}

func (p patch) equal(p2 patch, t *testing.T, checkValue bool) bool {
	var value1 interface{}
	var value2 interface{}
	if checkValue == true {
		if err := json.Unmarshal(p.Value, &value1); err != nil {
			t.Errorf("Error unmarshaling patch 1 %s\n%v\n%v", t.Name(), p.Value, err)
		}
		if err := json.Unmarshal(p2.Value, &value2); err != nil {
			t.Errorf("Error unmarshaling patch 2 %s\n%v\n%v", t.Name(), p2.Value, err)
		}
		if !reflect.DeepEqual(value1, value2) {
			return false
		}
	}
	if p.Op != p2.Op {
		return false
	}
	if p.Path != p2.Path {
		return false
	}
	return true
}

func (p patch) toString(t *testing.T) string {
	output, err := json.MarshalIndent(p, "", "    ")
	if err != nil {
		t.Errorf("Marshal failed\n")
		return "FAIL"
	}
	return string(output)
}

func checkPatches(t *testing.T, patches []patch, checkedPatches []patch, expected bool) {
	if expected {
		for _, expPatch := range checkedPatches {
			found := false
			for _, patch := range patches {
				if patch.equal(expPatch, t, true) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Patch %s not found\n", expPatch.toString(t))
			}
		}
	} else {
		for _, unexpPatch := range checkedPatches {
			found := false
			for _, patch := range patches {
				if patch.equal(unexpPatch, t, false) {
					found = true
					break
				}
			}
			if found {
				t.Errorf("Unexpected patch %s found\n", unexpPatch.toString(t))
			}
		}
	}
}

func handleAndChekAdmReview(t *testing.T, admReviewReq []byte, expectedPatches []patch, unexpectedPatches []patch) v1beta1.AdmissionReview {
	var admReviewResp v1beta1.AdmissionReview
	var patches []patch

	req, err := http.NewRequest("GET", "/mutatePods", bytes.NewBuffer([]byte(admReviewReq)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(serveMutatePod)

	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
		t.FailNow()
	}
	if err := json.Unmarshal([]byte(rr.Body.Bytes()), &admReviewResp); err != nil {
		t.Errorf("Admission review unmarshal error\n")
		t.FailNow()

	}
	if nil != admReviewResp.Response.Patch {
		if err := json.Unmarshal([]byte(admReviewResp.Response.Patch), &patches); err != nil {
			t.Errorf("Admission review response patch unmarshal error %v:%v\n", err, rr.Body)
			t.FailNow()
		}
		if expectedPatches == nil {
			t.Errorf("Patch received but not expected")
			t.FailNow()
		}
	} else {
		if expectedPatches != nil {
			t.Errorf("Patch not received but expected patches defined")
			t.FailNow()
		}
	}
	if expectedPatches != nil {
		checkPatches(t, patches, expectedPatches, true)
	}
	if unexpectedPatches != nil {
		checkPatches(t, patches, unexpectedPatches, false)
	}
	if t.Failed() {
		t.Errorf("Received patches:")
		for _, patch := range patches {
			t.Errorf("%s", patch.toString(t))
		}
		return admReviewResp
	}
	return admReviewResp
}

func TestMutatePodSharedCpu(t *testing.T) {

	admReviewReq, err := ioutil.ReadFile("../../test/testdata/pod-spec-shared-pool-req.json")
	if err != nil {
		t.Errorf("Could not read pod spec")
	}

	expectedPatches := []patch{
		patch{Op: "add", Path: "/spec/containers/0/volumeMounts/-",
			Value: json.RawMessage(`{"name":"podinfo","mountPath":"/etc/podinfo","readOnly":true}`)},
		patch{Op: "add", Path: "/spec/containers/0/volumeMounts/-",
			Value: json.RawMessage(`{"name":"hostbin","mountPath":"/opt/bin","readOnly":true}`)},
		patch{Op: "add", Path: "/spec/containers/0/volumeMounts/-",
			Value: json.RawMessage(`{"mountPath":"/etc/cpu-pooler","readOnly":true,"name":"cpu-pooler-config"}`)},
		patch{Op: "add", Path: "/spec/containers/0/env",
			Value: json.RawMessage(`[{"name": "CONTAINER_NAME", "value": "cputestcontainer"}]`)},
		patch{Op: "add", Path: "/spec/containers/0/command",
			Value: json.RawMessage(`[ "/opt/bin/process-starter" ]`)},
		patch{Op: "add", Path: "/spec/volumes/-",
			Value: json.RawMessage(`{"name":"podinfo","downwardAPI": { "items": [ { "path" : "annotations","fieldRef":{ "fieldPath": "metadata.annotations"} } ] } }`)},
		patch{Op: "add", Path: "/spec/volumes/-",
			Value: json.RawMessage(`{"name":"hostbin","hostPath":{ "path":"/opt/bin"} }`)},
		patch{Op: "add", Path: "/spec/volumes/-",
			Value: json.RawMessage(`{"name":"cpu-pooler-config","configMap":{ "name":"cpu-pooler-configmap"} }`)},
	}
	handleAndChekAdmReview(t, admReviewReq, expectedPatches, nil)
}

func TestMutatePodExclusiveCpu(t *testing.T) {

	admReviewReq, err := ioutil.ReadFile("../../test/testdata/pod-spec-exclusive-pool-req.json")
	if err != nil {
		t.Errorf("Could not read pod spec")
	}

	expectedPatches := []patch{
		patch{Op: "add", Path: "/spec/containers/0/volumeMounts/-",
			Value: json.RawMessage(`{"name":"podinfo","mountPath":"/etc/podinfo","readOnly":true}`)},
		patch{Op: "add", Path: "/spec/containers/0/volumeMounts/-",
			Value: json.RawMessage(`{"name":"hostbin","mountPath":"/opt/bin","readOnly":true}`)},
		patch{Op: "add", Path: "/spec/containers/0/volumeMounts/-",
			Value: json.RawMessage(`{"mountPath":"/etc/cpu-pooler","readOnly":true,"name":"cpu-pooler-config"}`)},
		patch{Op: "add", Path: "/spec/containers/0/env",
			Value: json.RawMessage(`[{"name": "CONTAINER_NAME", "value": "cputestcontainer"}]`)},
		patch{Op: "add", Path: "/spec/containers/0/command",
			Value: json.RawMessage(`[ "/opt/bin/process-starter" ]`)},
		patch{Op: "add", Path: "/spec/volumes/-",
			Value: json.RawMessage(`{"name":"podinfo","downwardAPI": { "items": [ { "path" : "annotations","fieldRef":{ "fieldPath": "metadata.annotations"} } ] } }`)},
		patch{Op: "add", Path: "/spec/volumes/-",
			Value: json.RawMessage(`{"name":"hostbin","hostPath":{ "path":"/opt/bin"} }`)},
		patch{Op: "add", Path: "/spec/volumes/-",
			Value: json.RawMessage(`{"name":"cpu-pooler-config","configMap":{ "name":"cpu-pooler-configmap"} }`)},
	}
	handleAndChekAdmReview(t, admReviewReq, expectedPatches, nil)
}
func TestMutatePodWrongPoolValueRequest(t *testing.T) {

	admReviewReq, err := ioutil.ReadFile("../../test/testdata/pod-spec-wrong-pool-value-req.json")
	if err != nil {
		t.Errorf("Could not read pod spec")
	}

	handleAndChekAdmReview(t, admReviewReq, nil, nil)
}
func TestMutatePodWrongExclusivePoolValueRequest(t *testing.T) {

	admReviewReq, err := ioutil.ReadFile("../../test/testdata/pod-spec-wrong-excl-pool-value-req.json")
	if err != nil {
		t.Errorf("Could not read pod spec")
	}

	admReviewResp := handleAndChekAdmReview(t, admReviewReq, nil, nil)
	if admReviewResp.Response.Allowed == true {
		t.Errorf("Pod unexpectedly allowed")
	}
}

func TestMutateMissinPoolInResource(t *testing.T) {

	admReviewReq, err := ioutil.ReadFile("../../test/testdata/pod-spec-pool-req-missing.json")
	if err != nil {
		t.Errorf("Could not read pod spec")
	}

	handleAndChekAdmReview(t, admReviewReq, nil, nil)
}

func TestMutateAnnotationPoolMissingInResource(t *testing.T) {

	admReviewReq, err := ioutil.ReadFile("../../test/testdata/pod-spec-excl-pool-req-missing.json")
	if err != nil {
		t.Errorf("Could not read pod spec")
	}

	handleAndChekAdmReview(t, admReviewReq, nil, nil)
}

func TestMutateBothPoolTypesRequestedInResource(t *testing.T) {

	admReviewReq, err := ioutil.ReadFile("../../test/testdata/pod-spec-both-pool-types-req.json")
	if err != nil {
		t.Errorf("Could not read pod spec")
	}

	handleAndChekAdmReview(t, admReviewReq, nil, nil)
}
