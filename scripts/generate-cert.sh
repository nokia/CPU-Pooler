#!/bin/bash
service="cpu-dev-pod-mutator-svc"
secret="cpu-dev-webhook-secret"
namespace="kube-system"
cluster_domain="cluster.local"

# Remove existing csr
kubectl delete csr ${service}.${namespace} &>/dev/null || true
kubectl delete secret generic ${secret} &>/dev/null || true

echo "[ req ]
default_bits = 2048
default_md = sha256
default_keyfile = privkey.pem
organizationName = system:nodes
distinguished_name = req_distinguished_name
x509_extensions = v3_ca # The extentions to add to the self signed cert
req_extensions = v3_req
prompt = no
[ req_distinguished_name ]
organizationName = system:nodes
commonName = system:node:${service}
[ v3_ca ]
basicConstraints = critical,CA:TRUE
subjectKeyIdentifier = hash
authorityKeyIdentifier = keyid:always,issuer:always
[ v3_req ]
basicConstraints = CA:FALSE
keyUsage = nonRepudiation, digitalSignature, keyEncipherment
subjectAltName = @alt_names
[ alt_names ]
DNS.1 = ${service}.${namespace}.svc.${cluster_domain}
DNS.2 = ${service}.${namespace}.svc
DNS.3 = ${service}.${namespace}" > openssl.cnf

openssl req -out server.csr -new -newkey rsa:2048  -nodes -keyout server-key.pem -config openssl.cnf


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

rm -f server.crt server-key.pem server.csr openssl.cnf
