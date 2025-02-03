package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"time"

	nats "github.com/nats-io/nats.go"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Config struct {
	NATSURL     string
	NATSSubject string
}

type consume struct {
	nconfig Config
	cli     *kubernetes.Clientset
}

func getClientSet() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, err
}

func New(config Config) (*consume, error) {
	clientset, err := getClientSet()
	if err != nil {
		return nil, err
	}
	return &consume{
		cli:     clientset,
		nconfig: config,
	}, nil
}

func (n *consume) Start(stopChan chan<- bool) error {
	defer func() { stopChan <- true }()

	// Configure structured logging with slog
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Connect to NATS server
	natsConnect, err := nats.Connect(n.nconfig.NATSURL)
	if err != nil {
		slog.Error("Failed to connect to NATS server: ", "error", err)
		return err
	}
	defer natsConnect.Close()
	slog.Info("Connected to NATS server", "server", n.nconfig.NATSURL)

	slog.Info("Subscribe to Pod creation messages...", "subject", n.nconfig.NATSSubject)
	// Subscribe to the subject
	natsConnect.Subscribe(n.nconfig.NATSSubject, func(msg *nats.Msg) {
		var pod corev1.Pod
		slog.Info("Received message", "subject", n.nconfig.NATSSubject, "data", string(msg.Data))
		// Deserialize the entire Pod metadata to JSON
		err := json.Unmarshal(msg.Data, &pod)
		if err != nil {
			slog.Error("Failed to Unmarshal Pod from rawData", "error", err, "rawData", string(msg.Data))
			return
		}
		slog.Info("Deserialized Pod", "pod", pod)

		if !(CreateNamespace(n.cli, pod.Namespace)) {
			slog.Error("Failed to create Namespace", "error", err)
			return
		}

		sterilizePodInplace(&pod)
		// Create the Pod in Kubernetes
		createdPod, err := n.cli.CoreV1().Pods(pod.Namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
		if err != nil {
			slog.Error("Failed to create Pod", "error", err)
			return
		}
		if !isPodSuccesfullyRunning(n.cli, pod.Namespace, pod.Name) {
			slog.Info("Failed to stole the wrokload", "Pod", createdPod)
		}
		slog.Info("Succefully stole the wrokload", "Pod", createdPod)
	})
	select {}
}

func CreateNamespace(cli *kubernetes.Clientset, namespace string) bool {
	// Ensure Namespace exists before creating the Pod
	_, err := cli.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Namespace does not exist, create it
			slog.Info("Namespace not found, creating it", "namespace", namespace)
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}

			_, err := cli.CoreV1().Namespaces().Create(context.TODO(), namespace, metav1.CreateOptions{})
			if err != nil {
				slog.Error("Failed to create namespace", "namespace", namespace, "error", err)
				return false
			}
		} else {
			// Other errors (e.g., API failure)
			slog.Error("Failed to check namespace existence", "namespace", namespace, "error", err)
			return false
		}
	}
	return true
}

func sterilizePodInplace(pod *corev1.Pod) {
	newPodObjectMeta := metav1.ObjectMeta{
		Name:        pod.Name,
		Namespace:   pod.Namespace,
		Labels:      pod.Labels,
		Annotations: pod.Annotations,
	}
	pod.ObjectMeta = newPodObjectMeta
}

// pollPodStatus polls the status of a Pod until it is Running or a timeout occurs
func isPodSuccesfullyRunning(clientset *kubernetes.Clientset, namespace, name string) bool {
	timeout := time.After(5 * time.Minute)    // Timeout after 5 minutes
	ticker := time.NewTicker(5 * time.Second) // Poll every 5 seconds
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			slog.Error("Timeout waiting for Pod to reach Running state", "namespace", namespace, "name", name)
			return false
		case <-ticker.C:
			pod, err := clientset.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
			if err != nil {
				slog.Error("Failed to get Pod status", "namespace", namespace, "name", name, "error", err)
				return false
			}

			slog.Info("Pod status", "namespace", namespace, "name", name, "phase", pod.Status.Phase)

			// Check if the Pod is Running
			if pod.Status.Phase == corev1.PodRunning {
				slog.Info("Pod is now Running", "namespace", namespace, "name", name)
				return true
			}
		}
	}
}
