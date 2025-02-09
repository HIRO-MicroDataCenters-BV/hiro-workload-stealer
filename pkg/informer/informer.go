package informer

import (
	"context"
	"encoding/json"
	"log/slog"

	nats "github.com/nats-io/nats.go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	watch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	k8SNamespaces = []string{"default", "kube-system", "kube-public",
		"kube-node-lease", "kube-admission", "kube-proxy", "kube-controller-manager",
		"kube-scheduler", "kube-dns"}
	stolenPodLablesList = []string{"is-pod-stolen", "donorUUID", "stealerUUID"}
)

type NATSConfig struct {
	NATSURL     string
	NATSSubject string
}

type Config struct {
	Nconfig          NATSConfig
	StealerUUID      string
	IgnoreNamespaces []string
}

type notify struct {
	config Config
	cli    *kubernetes.Clientset
}

type Results struct {
	stealerUUID    string
	donorUUID      string
	message        string
	status         string
	startTime      metav1.Time
	completionTime metav1.Time
	duration       string
}

type PodResults struct {
	Results Results     `json:"results"`
	Pod     *corev1.Pod `json:"pod"`
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

func New(config Config) (*notify, error) {
	clientset, err := getClientSet()
	if err != nil {
		return nil, err
	}
	return &notify{
		cli:    clientset,
		config: config,
	}, nil
}

func (n *notify) Start(stopChan chan<- bool) error {
	defer func() { stopChan <- true }()

	nsubject := n.config.Nconfig.NATSSubject
	nurl := n.config.Nconfig.NATSURL
	inamespace := n.config.IgnoreNamespaces
	// Connect to NATS server
	natsConnect, err := nats.Connect(nurl)
	if err != nil {
		slog.Error("Failed to connect to NATS server: ", "error", err)
		return err
	}
	defer natsConnect.Close()
	slog.Info("Connected to NATS server")

	slog.Info("Watching for Pod events")
	podWatch, err := n.cli.CoreV1().Pods("").Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		slog.Error("Failed to watch pods: ", "error", err)
		return err
	}
	defer podWatch.Stop()

	slog.Info("Listening for Pod deletion events...")
	for event := range podWatch.ResultChan() {
		pod, ok := event.Object.(*corev1.Pod)
		if !ok {
			continue
		}

		if event.Type == watch.Deleted {
			slog.Info("Received", "event", event)
			slog.Info("Pod delete event", "namespace", pod.Namespace, "name", pod.Name)
			if contains(inamespace, pod.Namespace) {
				slog.Info("Ignoring as Pod belongs to Ignored Namespaces", "namespaces", inamespace)
				continue
			}

			for _, label := range stolenPodLablesList {
				_, ok := pod.Labels[label]
				if !ok {
					slog.Info("Ignoring as Pod is not stolen one", "namespace", pod.Namespace, "name", pod.Name)
					continue
				}
			}

			slog.Info("Pod is stolen one, publishing results to NATS", "subject", nsubject,
				"namespace", pod.Namespace, "name", pod.Name, "labels", pod.Labels)

			PodResults := PodResults{
				Results: Results{
					stealerUUID:    n.config.StealerUUID,
					donorUUID:      pod.Labels["donorUUID"],
					message:        "Pod Execution Completed",
					status:         "Success",
					startTime:      *pod.Status.StartTime,
					completionTime: metav1.Now(),
					duration:       metav1.Now().Sub(pod.Status.StartTime.Time).String(),
				},
				Pod: pod,
			}
			// Serialize the entire Pod metadata to JSON
			metadataJSON, err := json.Marshal(PodResults)
			if err != nil {
				slog.Error("Failed to serialize PodResults", "error", err, "podResults", PodResults)
				continue
			}

			// Publish notification to NATS
			err = natsConnect.Publish(nsubject, metadataJSON)
			if err != nil {
				slog.Error("Failed to publish message to NATS", "error", err, "subject", nsubject, "podResults", PodResults)
				continue
			}
			slog.Info("Published Pod metadata to NATS", "subject", nsubject, "metadata", string(metadataJSON))
		}
	}
	return nil
}

func contains(list []string, item string) bool {
	for _, str := range mergeUnique(list, k8SNamespaces) {
		if str == item {
			return true
		}
	}
	return false
}

func mergeUnique(slice1, slice2 []string) []string {
	uniqueMap := make(map[string]bool)
	result := []string{}

	for _, item := range slice1 {
		uniqueMap[item] = true
	}
	for _, item := range slice2 {
		uniqueMap[item] = true
	}

	for key := range uniqueMap {
		result = append(result, key)
	}

	return result
}
