#!/bin/bash

echo "Create Kind cluster"

echo "Delete and Create a 'kind' cluster with name 'local'"
kind delete cluster --name local
kind create cluster --name local

echo "Set the kubectl context to local cluster"
kubectl cluster-info --context kind-local
