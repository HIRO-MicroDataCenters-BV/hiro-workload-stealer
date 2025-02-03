#!/bin/bash
echo "Delete and Create a 'kind' cluster with name 'local'"
kind delete cluster --name local
kind create cluster --name local