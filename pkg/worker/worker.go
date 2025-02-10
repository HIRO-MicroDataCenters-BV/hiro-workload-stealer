package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

var (
	jetStreamBucket    = "message_tracking"
	jetStreamQueue     = "worker-group" //should be same for all workers/stealers
	stolenPodLablesMap = map[string]string{
		"is-pod-stolen": "true",
	}
)

type NATSConfig struct {
	NATSURL     string
	NATSSubject string
}

type Config struct {
	Nconfig     NATSConfig
	StealerUUID string
}

type consume struct {
	config Config
	cli    *kubernetes.Clientset
}

type DonorPod struct {
	DonorUUID string      `json:"donorUUID"`
	Pod       *corev1.Pod `json:"pod"`
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
		cli:    clientset,
		config: config,
	}, nil
}

func mergeMaps(map1, map2 map[string]string) map[string]string {
	merged := make(map[string]string)
	for k, v := range map1 {
		merged[k] = v
	}
	for k, v := range map2 {
		merged[k] = v
	}
	return merged
}

func (c *consume) Start(stopChan chan<- bool) error {
	defer func() { stopChan <- true }()

	// Configure structured logging with slog
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	// Connect to NATS server
	natsConnect, err := nats.Connect(c.config.Nconfig.NATSURL)
	if err != nil {
		slog.Error("Failed to connect to NATS server: ", "error", err)
		return err
	}
	defer natsConnect.Close()
	slog.Info("Connected to NATS server", "server", c.config.Nconfig.NATSURL)

	// Connect to JetStreams
	js, err := natsConnect.JetStream()
	if err != nil {
		slog.Error("Failed to connect to JetStreams server: ", "error", err)
		return err
	}

	jsStreamName := "Stream" + c.config.Nconfig.NATSSubject
	// Create a stream for message processing
	_, err = js.AddStream(&nats.StreamConfig{
		Name:      jsStreamName,
		Subjects:  []string{c.config.Nconfig.NATSSubject},
		Storage:   nats.FileStorage,
		Replicas:  1,
		Retention: nats.WorkQueuePolicy, // Ensures a message is only processed once
	})
	if err != nil && err != nats.ErrStreamNameAlreadyInUse {
		slog.Error("Failed to add a streams to JetStream server: ", "error", err)
	}

	// Create or get the KV Store for message tracking
	kv, err := js.KeyValue(jetStreamBucket)
	if err != nil && err != nats.ErrBucketNotFound {
		slog.Error("Failed to get KeyValue: ", "error", err)
		return err
	}
	if kv == nil {
		kv, _ = js.CreateKeyValue(&nats.KeyValueConfig{Bucket: jetStreamBucket})
	}

	slog.Info("Subscribe to Pod stealing messages...", "stream", jsStreamName,
		"queue", jetStreamQueue, "subject", c.config.Nconfig.NATSSubject)
	// // Subscribe to the subject
	//natsConnect.Subscribe(n.nconfig.NATSSubject, func(msg *nats.Msg) {
	// Queue Group ensures only one consumer gets a message
	js.QueueSubscribe(c.config.Nconfig.NATSSubject, jetStreamQueue, func(msg *nats.Msg) {
		var pod corev1.Pod
		var donorPod DonorPod
		var donorUUID string
		var stealerUUID = c.config.StealerUUID
		slog.Info("Received message", "subject", c.config.Nconfig.NATSSubject, "data", string(msg.Data))

		// Deserialize the entire donotPodMap metadata to JSON
		err := json.Unmarshal(msg.Data, &donorPod)
		if err != nil {
			slog.Error("Failed to Unmarshal donorPodMap from rawData",
				"error", err, "rawData", string(msg.Data))
			msg.Nak()
			return
		}
		pod = *donorPod.Pod
		slog.Info("Deserialized Pod", "pod", pod)
		// Check if the Pod already exists
		_, err = c.cli.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		if err == nil {
			slog.Info("Pod already exists, skipping",
				"podName", pod.Name, "podNamespace", pod.Namespace)
			msg.Nak()
			return
		} else if !apierrors.IsNotFound(err) {
			slog.Error("Failed to check if Pod exists", "podName", pod.Name,
				"podNamespace", pod.Namespace, "error", err)
			msg.Nak()
			return
		}
		donorUUID = donorPod.DonorUUID
		slog.Info("Deserialized donorUUID", "donorUUID", donorUUID)

		// Check if message is already processed
		entry, err := kv.Get(donorUUID)
		if err == nil && string(entry.Value()) != "Pending" {
			otherStealerUUID := string(entry.Value())
			if otherStealerUUID == stealerUUID {
				slog.Info("Skipping Pod, I am already processed it", "podName", pod.Name,
					"podNamespace", pod.Namespace, "stealerUUID", stealerUUID)
				StealPod(c.cli, pod, donorUUID, stealerUUID)
				msg.Ack()
			} else {
				slog.Info("Skipping Pod, already processed by another stealer", "podName", pod.Name,
					"podNamespace", pod.Namespace, "otherStealerUUID", otherStealerUUID)
			}
			return
		}

		// Mark with stealerUUID in KV by this stealer
		_, err = kv.Put(donorUUID, []byte(stealerUUID))
		if err != nil && !errors.Is(err, nats.ErrKeyExists) {
			slog.Error("Failed to put value in KV bucket: ", "error", err)
			msg.Nak()
		}

		_, err = StealPod(c.cli, pod, donorUUID, stealerUUID)
		if err != nil {
			slog.Error("Failed to steal Pod", "error", err)
			msg.Nak()
		}

		// Acknowledge JetStream message
		msg.Ack()

	})
	select {}
}

func StealPod(cli *kubernetes.Clientset, pod corev1.Pod, donorUUID string, stealerUUID string) (*corev1.Pod, error) {
	success, err := CreateNamespace(cli, pod.Namespace)
	if !success || err != nil {
		slog.Error("Error occurred", "error", err)
		return nil, err
	}

	sterilizePodInplace(&pod, donorUUID, stealerUUID)

	// Get the Pod with the specified name and namespace
	existingPod, err := cli.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
	if err == nil {
		if areMapsEqual(existingPod.Labels, pod.Labels) {
			slog.Info("Pod already stolen", "podName", pod.Name, "podNamespace", pod.Namespace)
			return existingPod, nil
		} else {
			slog.Error("Cannot Steal; Same Pod exists with the specified details", "podName", pod.Name, "podNamespace", pod.Namespace)
			return nil, fmt.Errorf("cannot steal; same pod already exists with the specified details: podName=%s, podNamespace=%s", pod.Name, pod.Namespace)
		}
	} else if !apierrors.IsNotFound(err) {
		slog.Error("Failed to get Pod with specified details", "podName", pod.Name, "podNamespace", pod.Namespace, "error", err)
		return nil, err
	}

	// Create the Pod in Kubernetes
	createdPod, err := cli.CoreV1().Pods(pod.Namespace).Create(context.TODO(), &pod, metav1.CreateOptions{})
	if err != nil {
		slog.Error("Failed to create Pod", "error", err)
		return nil, err
	}

	if !isPodSuccesfullyRunning(cli, pod.Namespace, pod.Name) {
		slog.Error("Pod is not running within 5 min", "pod", createdPod)
		return nil, fmt.Errorf("pod is not running within 5 min: podName=%s, podNamespace=%s", pod.Name, pod.Namespace)
	}
	slog.Info("Successfully stole the workload", "pod", createdPod)
	return createdPod, nil
}

func areMapsEqual(map1, map2 map[string]string) bool {
	if len(map1) != len(map2) {
		return false
	}
	for k, v := range map1 {
		if map2[k] != v {
			return false
		}
	}
	return true
}

func CreateNamespace(cli *kubernetes.Clientset, namespace string) (bool, error) {
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
				return false, err
			}
		} else {
			// Other errors (e.g., API failure)
			slog.Error("Failed to check namespace existence", "namespace", namespace, "error", err)
			return false, err
		}
	}
	return true, nil
}

func sterilizePodInplace(pod *corev1.Pod, donorUUID string, stealerUUID string) {
	newPodObjectMeta := metav1.ObjectMeta{
		Name:        pod.Name,
		Namespace:   pod.Namespace,
		Labels:      fetchStolenPodLablesMap(*pod, donorUUID, stealerUUID),
		Annotations: pod.Annotations,
	}
	pod.ObjectMeta = newPodObjectMeta
}

func fetchStolenPodLablesMap(pod corev1.Pod, donorUUID string, stealerUUID string) map[string]string {
	stolenPodLablesMap["donorUUID"] = donorUUID
	stolenPodLablesMap["stealerUUID"] = stealerUUID
	return mergeMaps(pod.Labels, stolenPodLablesMap)
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
