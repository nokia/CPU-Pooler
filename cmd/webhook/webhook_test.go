package main

import (
	"bytes"
	"encoding/json"
	"k8s.io/api/admission/v1beta1"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func (p Patch) equal(p2 Patch, t *testing.T) bool {
	var value1 interface{}
	var value2 interface{}
	if err := json.Unmarshal(p.Value, &value1); err != nil {
		t.Errorf("Error unmarshaling patch 1 %s\n%v\n%v", t.Name(), p.Value, err)
	}
	if err := json.Unmarshal(p2.Value, &value2); err != nil {
		t.Errorf("Error unmarshaling patch 2 %s\n%v\n%v", t.Name(), p2.Value, err)
	}
	if !reflect.DeepEqual(value1, value2) {
		return false
	}
	if p.Op != p2.Op {
		return false
	}
	if p.Path != p2.Path {
		return false
	}
	return true
}

func (p Patch) toString(t *testing.T) string {
	output, err := json.MarshalIndent(p, "", "    ")
	if err != nil {
		t.Errorf("Marshal failed\n")
		return "FAIL"
	}
	return string(output)
}

func TestMutatePod(t *testing.T) {
	var admReviewResp v1beta1.AdmissionReview
	var patches []Patch

	const admReviewReq = string(`{"kind":"AdmissionReview","apiVersion":"admission.k8s.io/v1beta1","request":{"uid":"0e8a379c-db6e-11e8-b72a-fa163e875bcf","kind":{"group":"","version":"v1","kind":"Pod"},"resource":{"group":"","version":"v1","resource":"pods"},"namespace":"default","operation":"CREATE","userInfo":{"username":"kubernetes-admin","groups":["system:masters","system:authenticated"]},"object":{"metadata":{"name":"cpupod","namespace":"default","creationTimestamp":null,"annotations":{"nokia.k8s.io/cpus":"[{\n\"container\": \"cputestcontainer\",\n\"processes\":\n  [{\n     \"process\": \"/bin/sh\",\n     \"args\": [\"-c\",\"/thread_busyloop -n \\\"Process \\\"1\"],\n     \"cpus\": 1,\n     \"pool\": \"cpupool2\"\n   },\n   {\n     \"process\": \"/bin/sh\",\n     \"args\": [\"-c\", \"/thread_busyloop -n \\\"Process \\\"2\"],\n     \"pool\": \"sharedpool\",\n     \"cpus\": 10\n   } \n]\n}]\n"}},"spec":{"volumes":[{"name":"default-token-lf4p4","secret":{"secretName":"default-token-lf4p4"}}],"containers":[{"name":"cputestcontainer","image":"busyloop","command":["/bin/bash","-c","--"],"args":["while true; do sleep 1; done;"],"ports":[{"containerPort":80,"protocol":"TCP"}],"resources":{"limits":{"memory":"2000Mi","nokia.com/cpupool2":"2","nokia.com/sharedpool":"10"},"requests":{"memory":"2000Mi","nokia.com/cpupool2":"2","nokia.com/sharedpool":"10"}},"volumeMounts":[{"name":"default-token-lf4p4","readOnly":true,"mountPath":"/var/run/secrets/kubernetes.io/serviceaccount"}],"terminationMessagePath":"/dev/termination-log","terminationMessagePolicy":"File","imagePullPolicy":"IfNotPresent"},{"name":"busyloop","image":"busyloop","command":["/thread_busyloop"],"args":["-c","$(EXCLUSIVE_CPUS)","-n","Process 3"],"resources":{"limits":{"memory":"2000Mi","nokia.com/cpupool1":"2"},"requests":{"memory":"2000Mi","nokia.com/cpupool1":"2"}},"volumeMounts":[{"name":"default-token-lf4p4","readOnly":true,"mountPath":"/var/run/secrets/kubernetes.io/serviceaccount"}],"terminationMessagePath":"/dev/termination-log","terminationMessagePolicy":"File","imagePullPolicy":"IfNotPresent"}],"restartPolicy":"Always","terminationGracePeriodSeconds":30,"dnsPolicy":"ClusterFirst","serviceAccountName":"default","serviceAccount":"default","securityContext":{},"schedulerName":"default-scheduler","tolerations":[{"key":"node.kubernetes.io/not-ready","operator":"Exists","effect":"NoExecute","tolerationSeconds":300},{"key":"node.kubernetes.io/unreachable","operator":"Exists","effect":"NoExecute","tolerationSeconds":300}]},"status":{}},"oldObject":null}}`)
	expectedPatches := []Patch{
		Patch{Op: "add", Path: "/spec/containers/0/volumeMounts/-",
			Value: json.RawMessage(`{"name":"podinfo","mountPath":"/etc/podinfo","readOnly":true}`)},
		Patch{Op: "add", Path: "/spec/containers/0/volumeMounts/-",
			Value: json.RawMessage(`{"name":"hostbin","mountPath":"/opt/bin","readOnly":true}`)},
		Patch{Op: "add", Path: "/spec/containers/0/volumeMounts/-",
			Value: json.RawMessage(`{"mountPath":"/etc/cpu-dp","readOnly":true,"name":"cpu-dp-config"}`)},
		Patch{Op: "add", Path: "/spec/containers/0/env",
			Value: json.RawMessage(`[{"name": "CONTAINER_NAME", "value": "cputestcontainer"}]`)},
		Patch{Op: "add", Path: "/spec/containers/0/command",
			Value: json.RawMessage(`[ "/opt/bin/process-starter" ]`)},
		Patch{Op: "add", Path: "/spec/volumes/-",
			Value: json.RawMessage(`{"name":"podinfo","downwardAPI": { "items": [ { "path" : "annotations","fieldRef":{ "fieldPath": "metadata.annotations"} } ] } }`)},
		Patch{Op: "add", Path: "/spec/volumes/-",
			Value: json.RawMessage(`{"name":"hostbin","hostPath":{ "path":"/opt/bin"} }`)},
		Patch{Op: "add", Path: "/spec/volumes/-",
			Value: json.RawMessage(`{"name":"cpu-dp-config","configMap":{ "name":"cpu-dp-configmap"} }`)},
	}

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
	}
	if err := json.Unmarshal([]byte(rr.Body.Bytes()), &admReviewResp); err != nil {
		t.Errorf("Admission review unmarshal error\n")
	}
	if err := json.Unmarshal([]byte(admReviewResp.Response.Patch), &patches); err != nil {
		t.Errorf("Admission review response patch unmarshal error\n")
	}
	for _, expPatch := range expectedPatches {
		found := false
		for _, patch := range patches {
			if patch.equal(expPatch, t) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Patch %s not found\n", expPatch.toString(t))
		}
	}
	if t.Failed() {
		t.Errorf("Received patches:")
		for _, patch := range patches {
			t.Errorf("%s", patch.toString(t))
		}
	}
}
