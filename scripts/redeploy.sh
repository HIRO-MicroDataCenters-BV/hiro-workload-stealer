#!/bin/bash
echo "Build Docker image"
docker build -t workloadstealworker:alpha1 .

echo "Set the kubectl context to local cluster"
kubectl cluster-info --context kind-local
kubectl config set-context kind-local

echo "Load Image to Kind cluster named 'local'"
kind load docker-image --name local workloadstealworker:alpha1

echo "Restarting Worker Server"
kubectl rollout restart deployment workload-steal-worker -n hiro