package k8s

import (
	"flag"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"go.uber.org/zap"
	apiv1 "k8s.io/api/core/v1"
	v1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"path/filepath"
)

type Client struct {
	logger *zap.SugaredLogger
	clientSet *kubernetes.Clientset
}

func (c *Client) GetPods(namespace string) (*apiv1.PodList , error) {
	return c.clientSet.CoreV1().Pods(namespace).List(metav1.ListOptions{})
}

func (c *Client) DeletePod(name string, namespace string) error {
	return c.clientSet.CoreV1().Pods(namespace).Delete(name, &metav1.DeleteOptions{})
}

func (c *Client) RestartPod(pod *apiv1.Pod) (*apiv1.Pod, error) {
	if err := c.DeletePod(pod.ObjectMeta.Name, pod.ObjectMeta.Namespace); err != nil {
		return nil, err
	}
	return c.clientSet.CoreV1().Pods(pod.ObjectMeta.Namespace).Create(pod)
}

func (c *Client) GetPodDisruptionBudget(name string, namespace string) (*v1beta1.PodDisruptionBudget, error) {
	return c.clientSet.PolicyV1beta1().PodDisruptionBudgets(namespace).Get(name, metav1.GetOptions{})
}

func (c *Client) GetPodDisruptionBudgets(namespace string) (*v1beta1.PodDisruptionBudgetList, error) {
	return c.clientSet.PolicyV1beta1().PodDisruptionBudgets(namespace).List(metav1.ListOptions{})
}

func NewClient(logger *zap.SugaredLogger) (*Client, error) {
	var err error
	var config *restclient.Config
	if os.Getenv("KUBERNETES_SERVICE_HOST") == "" {
		// Read kubeconfig flag from command line
		kubeconfig := flag.String("kubeconfig", filepath.Join(os.Getenv("HOME"), ".kube", "config"),"")
		flag.Parse()
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)

	} else {
		// Reads config when in cluster
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}

	clientSet, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &Client{clientSet: clientSet, logger: logger}, err
}
