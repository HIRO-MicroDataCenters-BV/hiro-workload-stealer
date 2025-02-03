# hiro-stealer-worker
The `hiro-stealer-stealer` is a component designed to facilitate the stealing of workloads in a Kubernetes environment. It automates the process of initializing a local Kind cluster, building and deploying the necessary Docker images, and starting the worker server. This project aims to streamline the setup and management of workload stealing, making it easier for developers to test and deploy their applications in a controlled environment.
## Installation

To install the stealer, follow these steps:
1. **Clone the Repository**:
    Clone the `hiro-stealer-worker` repository to your local machine using the following command:
    ```sh
    git clone https://github.com/HIRO-MicroDataCenters-BV/hiro-workload-stealer.git
    cd hiro-stealer-worker
    chmod +x scripts/*
    ```

2. **Start the Stealer**:
   Run the `start_stealer.sh` script to execute both the `initialize.sh` and `install.sh` scripts sequentially. This script ensures that the initialization and installation steps are completed successfully.
   ```sh
   ./scripts/start_stealer.sh
   ```
    
    - **Initialize the Kind Cluster**:
        Run the `initialize.sh` script to delete any existing Kind cluster named 'local' and create a new one. This script also sets the kubectl context to the local cluster.
        ```sh
        cd scripts
        ./initialize.sh
        ```
    - **Build and Deploy the Docker Image**:
        Run the `install.sh` script to build the Docker image for the worker, load it into the Kind cluster, and deploy the worker server using the Kubernetes deployment configuration.
        ```sh
        cd scripts
        ./install.sh
        ```

3. **Redeploy the Worker**:
    If you need to redeploy the stealer server, you can use the `redeploy.sh` script. This script will rebuild the Docker image and redeploy the worker server without reinitializing the Kind cluster.
    ```sh
    cd scripts
    ./redeploy.sh
    ```
