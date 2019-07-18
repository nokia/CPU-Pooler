#!/bin/bash

service="cpu-dev-pod-mutator-svc"
secret="cpu-dev-webhook-secret"
namespace="kube-system"

# Remove existing csr
kubectl delete csr ${service}.${namespace} &>/dev/null || true
kubectl delete secret generic ${secret} &>/dev/null || true

openssl req -out server.csr -new -newkey rsa:2048 -subj "/CN=${service}.${namespace}.svc" -nodes -keyout server-key.pem


cat <<EOF | kubectl create -f -
apiVersion: certificates.k8s.io/v1beta1
kind: CertificateSigningRequest
metadata:
  name: ${service}.${namespace}
spec:
  groups:
  - system:authenticated
  request: $(cat server.csr | base64 | tr -d '\n')
  usages:
  - digital signature
  - key encipherment
  - server auth
EOF


kubectl certificate approve ${service}.${namespace}

kubectl get csr ${service}.${namespace} -o jsonpath='{.status.certificate}' \
    | base64 --decode > server.crt

if [ ! -s server.crt ]; then
    printf "\nFailed to get csr, try again\n"
    exit
fi

kubectl create secret generic ${secret} -n ${namespace}\
        --from-file=key.pem=server-key.pem \
        --from-file=cert.pem=server.crt

rm -f server.crt server-key.pem server.csr
