#!/bin/bash

CLUSTER_NAME=${1:-local}
echo "Build Docker image"
docker build -t workloadstealworker:alpha1 .

echo "Set the kubectl context to $CLUSTER_NAME cluster"
kubectl cluster-info --context kind-$CLUSTER_NAME
kubectl config use-context kind-$CLUSTER_NAME

echo "Load Image to Kind cluster named '$CLUSTER_NAME'"
kind load docker-image --name $CLUSTER_NAME workloadstealworker:alpha1

echo "Restarting Worker Server"
kubectl rollout restart deployment workload-steal-worker -n hiro