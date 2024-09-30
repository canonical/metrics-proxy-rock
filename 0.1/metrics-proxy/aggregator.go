package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// PodMetrics stores the metrics endpoint details and metadata for a pod.
type PodMetrics struct {
	Port      string
	Path      string
	PodName   string
	Namespace string
}

// podMetricsEndpoints stores all pod metrics endpoints, synchronized by a mutex.
var (
	podMetricsEndpoints = make(map[string]PodMetrics)
	mu                  sync.Mutex
)

// parseLabels parses a label selector string into a map.
func parseLabels(labelString string) map[string]string {
	labels := map[string]string{}
	for _, pair := range strings.Split(labelString, ",") {
		if keyValue := strings.Split(pair, "="); len(keyValue) == 2 {
			labels[keyValue[0]] = keyValue[1]
		}
	}
	return labels
}

// watchPods watches for pod changes and updates the metrics endpoints accordingly.
func watchPods(clientset *kubernetes.Clientset, namespace string, labels map[string]string) {
	labelSelector := metav1.FormatLabelSelector(&metav1.LabelSelector{MatchLabels: labels})
	for {

		watcher, err := clientset.CoreV1().Pods(namespace).Watch(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			log.Fatalf("Error watching pods: %v", err)
		}

		for event := range watcher.ResultChan() {
			handlePodEvent(event)
		}
	}
}

// handlePodEvent processes the pod events and updates the pod metrics endpoints.
func handlePodEvent(event watch.Event) {
	pod, ok := event.Object.(*corev1.Pod)
	if !ok {
		log.Println("Error casting event object to Pod")
		return
	}

	mu.Lock()
	defer mu.Unlock()

	switch event.Type {
	case watch.Added, watch.Modified:
		updatePodMetrics(pod)
	case watch.Deleted:
		deletePodMetrics(pod)
	}
}

// updatePodMetrics updates or adds pod metrics based on the pod annotations.
func updatePodMetrics(pod *corev1.Pod) {
	annotations := pod.GetAnnotations()
	if scrape, exists := annotations["prometheus.io/scrape"]; exists && scrape == "true" {
		podIP := pod.Status.PodIP
		if podIP == "" {
			return
		}

		port := annotations["prometheus.io/port"]
		if port == "" {
			port = "80"
		}
		path := annotations["prometheus.io/path"]
		if path == "" {
			path = "/metrics"
		}

		// Store the pod IP, port, path, and additional metadata like name and namespace
		podMetricsEndpoints[podIP] = PodMetrics{
			Port:      port,
			Path:      path,
			PodName:   pod.Name,
			Namespace: pod.Namespace,
		}
		log.Printf("Updated pod %s with IP %s", pod.Name, podIP)
	}
}

// deletePodMetrics removes the pod metrics entry when a pod is deleted.
func deletePodMetrics(pod *corev1.Pod) {
	podIP := pod.Status.PodIP
	if podIP != "" {
		delete(podMetricsEndpoints, podIP)
		log.Printf("Deleted pod %s with IP %s", pod.Name, podIP)
	}
}

// appendLabels adds pod-specific labels to each metric.
// this was added to allow metrics distinction if multiple pods are reporting the same metric
func appendLabels(metricsData, podName, namespace string) string {
	// Split the metrics into lines
	lines := strings.Split(metricsData, "\n")

	// Prepend pod and namespace labels to each metric line, where applicable
	var labeledMetrics []string
	for _, line := range lines {
		// Skip comments and empty lines
		if strings.HasPrefix(line, "#") || line == "" {
			labeledMetrics = append(labeledMetrics, line)
			continue
		}

		// If the line already contains labels, insert pod-specific labels inside the existing braces.
		// Otherwise, add the labels between the metric name and its value.
		if strings.Contains(line, "{") {
			// Insert the pod and namespace labels within the existing labels.
			line = strings.Replace(line, "{", fmt.Sprintf("{k8s_pod_name=\"%s\",k8s_namespace=\"%s\",", podName, namespace), 1)
		} else {
			// Add the labels before the value (after the metric name).
			parts := strings.Fields(line)
			if len(parts) == 2 {
				metricName := parts[0]
				metricValue := parts[1]
				line = fmt.Sprintf("%s{k8s_pod_name=\"%s\",k8s_namespace=\"%s\"} %s", metricName, podName, namespace, metricValue)
			}
		}

		labeledMetrics = append(labeledMetrics, line)
	}

	return strings.Join(labeledMetrics, "\n")
}

// proxyMetrics aggregates metrics from all pods, appends pod metadata, and returns them in a HTTP response.
func proxyMetrics(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	var responses []string
	var errors []string

	for podIP, metrics := range podMetricsEndpoints {
		url := fmt.Sprintf("http://%s:%s%s", podIP, metrics.Port, metrics.Path)
		resp, err := http.Get(url)
		if err != nil {
			errors = append(errors, fmt.Sprintf("Error scraping %s: %v", url, err))
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errors = append(errors, fmt.Sprintf("Failed to scrape %s, status code: %d", url, resp.StatusCode))
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			errors = append(errors, fmt.Sprintf("Error reading response from %s: %v", url, err))
			continue
		}

		// Append pod labels to the scraped metrics
		labeledMetrics := appendLabels(string(body), metrics.PodName, metrics.Namespace)
		responses = append(responses, labeledMetrics)
	}

	w.Header().Set("Content-Type", "text/plain")
	if len(responses) > 0 {
		w.Write([]byte(strings.Join(responses, "\n")))
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(strings.Join(errors, "\n")))
	}
}

// getClientConfig returns the Kubernetes client configuration, either from kubeconfig or in-cluster.
func getClientConfig() (*rest.Config, error) {
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}

func main() {
	labelSelector := flag.String("labels", "", "Label selector for watching pods (e.g., 'app=ztunnel')")
	flag.Parse()

	if *labelSelector == "" {
		log.Fatal("Label selector is required (e.g., --labels app=ztunnel)")
	}

	labels := parseLabels(*labelSelector)

	config, err := getClientConfig()
	if err != nil {
		log.Fatalf("Error building Kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	}

	go watchPods(clientset, "", labels)

	r := mux.NewRouter()
	r.HandleFunc("/metrics", proxyMetrics).Methods(http.MethodGet)

	server := &http.Server{
		Handler:      r,
		Addr:         "0.0.0.0:15090",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}

	log.Println("Starting metrics proxy server on port 15090")
	log.Fatal(server.ListenAndServe())
}
