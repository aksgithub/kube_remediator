package remediator

import (
	"context"
	"github.com/aksgithub/kube_remediator/pkg/k8s"
	"github.com/aksgithub/kube_remediator/pkg/metrics"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"sync"
	"time"
)

var CONFIG_FILE = "config/crash_loop_back_off_rescheduler.json"

type PodFilter struct {
	annotation       string
	failureThreshold int32
	namespace        string
}

type CrashLoopBackOffRescheduler struct {
	client   k8s.ClientInterface
	logger   *zap.Logger
	interval time.Duration
	filter   PodFilter
	metrics  *metrics.CrashLoopBackOff_Metrics
}

func waitDone(wg *sync.WaitGroup) {
	if wg != nil {
		wg.Done()
	}
}

// Entrypoint
func (p *CrashLoopBackOffRescheduler) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer waitDone(wg)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.logger.Info("Starting")

	p.reschedulePods() // first tick on start

	for {
		select {
		case <-ticker.C:
			p.reschedulePods()
		case <-ctx.Done():
			p.logger.Info("Stopping", zap.String("reason", "Signal"))
			return
		}
	}
}

// TODO: check logging looks good
func (p *CrashLoopBackOffRescheduler) reschedulePods() {
	p.logger.Info("Running")

	for _, pod := range *p.getCrashLoopBackOffPods() {
		podInfo := []zap.Field{
			zap.String("name", pod.ObjectMeta.Name),
			zap.String("namespace", pod.ObjectMeta.Namespace),
		}
		p.tryWithLogging("Deleting Pod", podInfo, func() error {
			p.metrics.UpdateRescheduledCount()
			return p.client.DeletePod(&pod)
		})
	}
}

func (p *CrashLoopBackOffRescheduler) tryWithLogging(message string, logInfo []zap.Field, fn func() error) {
	p.logger.Info(message, logInfo...)
	if err := fn(); err != nil {
		p.logger.Warn("Error "+message, append(logInfo, zap.Error(err))...)
	}
}

func (p *CrashLoopBackOffRescheduler) getCrashLoopBackOffPods() *[]v1.Pod {
	pods, err := p.client.GetPods(p.filter.namespace)
	if err != nil {
		p.logger.Error("Error getting pod list: ", zap.Error(err))
		return &[]v1.Pod{}
	}
	var unhealthyPods []v1.Pod
	for _, pod := range pods.Items {
		if p.shouldReschedule(&pod) {
			unhealthyPods = append(unhealthyPods, pod)
		}
	}
	return &unhealthyPods
}

func (p *CrashLoopBackOffRescheduler) shouldReschedule(pod *v1.Pod) bool {
	return (p.filter.annotation == "" || pod.ObjectMeta.Annotations[p.filter.annotation] == "true") && // Opted in
		len(pod.ObjectMeta.OwnerReferences) > 0 && // Assuming Pod has owner reference of kind Controller
		p.isPodUnhealthy(pod)
}

// This is not 100% reliable because Pod could toggle between Terminated with Error and Waiting with CrashLoopBackOff
func (p *CrashLoopBackOffRescheduler) isPodUnhealthy(pod *v1.Pod) bool {
	// Check if any of Containers is in CrashLoop
	statuses := append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...)
	for _, containerStatus := range statuses {
		if containerStatus.RestartCount > p.filter.failureThreshold {
			if containerStatus.State.Waiting != nil && containerStatus.State.Waiting.Reason == "CrashLoopBackOff" {
				return true
			}
		}
	}
	return false
}

// TODO: make a config object and read it directly via standard json serializer
func NewCrashLoopBackOffRescheduler(logger *zap.Logger,
	client k8s.ClientInterface,
	metrics *metrics.CrashLoopBackOff_Metrics) (*CrashLoopBackOffRescheduler, error) {
	logger.Info("Reading config", zap.String("file", CONFIG_FILE))
	viper.SetConfigFile(CONFIG_FILE)
	viper.SetConfigType("json")
	viper.SetDefault("annotation", "kube-remediator/CrashLoopBackOffRemediator")
	viper.SetDefault("failureThreshold", 5)
	viper.SetDefault("namespace", "")
	viper.SetDefault("interval", "1m")

	if err := viper.ReadInConfig(); err != nil {
		logger.Warn("Error reading config:", zap.Error(err))
	}

	logger.Sugar().Infof("Config %v", viper.AllSettings()) // TODO: prefer using zap.Map or something like that
	filter := PodFilter{
		annotation:       viper.GetString("annotation"),
		failureThreshold: viper.GetInt32("failureThreshold"),
		namespace:        viper.GetString("namespace"),
	}

	p := &CrashLoopBackOffRescheduler{
		client:   client,
		logger:   logger,
		interval: viper.GetDuration("interval"),
		filter:   filter,
		metrics:  metrics,
	}
	return p, nil
}
