package gc

import (
	"context"
	"fmt"
	"strconv"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

func Run(clientset *kubernetes.Clientset, podSelector, buildkitStefulset string) {
	ctx := context.Background()

	go func() {
		for {
			TickGC(ctx, clientset, podSelector, buildkitStefulset)
			time.Sleep(time.Minute * 5)
		}
	}()
}

const DeployAgentLastBuildEndingTimeLabelKey = "deploy-agent.tsuru.io/last-build-ending-time" // TODO: move to other place

func TickGC(ctx context.Context, clientset *kubernetes.Clientset, podSelector, buildkitStefulset string) error {
	defer func() {
		recoverErr := recover()
		fmt.Println("print err", recoverErr)
	}()

	buildKitPods, err := clientset.CoreV1().Pods("*").List(ctx, v1.ListOptions{
		LabelSelector: podSelector,
	})

	if err != nil {
		return err
	}

	maxEndtimeByNS := map[string]int64{}

	for _, pod := range buildKitPods.Items {
		if pod.Annotations[DeployAgentLastBuildEndingTimeLabelKey] == "" {
			maxEndtimeByNS[pod.Namespace] = -1 // mark that namespace has least one pod of buildkit running
			continue
		}

		maxUsage, err := strconv.ParseInt(pod.Annotations[DeployAgentLastBuildEndingTimeLabelKey], 10, 64)
		if err != nil {
			klog.Errorf("failed to parseint: %s", err.Error())
			continue
		}

		if maxEndtimeByNS[pod.Namespace] == -1 {
			continue
		}

		if maxEndtimeByNS[pod.Namespace] < maxUsage {
			maxEndtimeByNS[pod.Namespace] = maxUsage
		}
	}

	now := time.Now().Unix()
	gracefulPeriod := int64(60 * 30)
	zero := int32(0)

	for ns, maxEndtime := range maxEndtimeByNS {
		if maxEndtime == -1 {
			continue
		}

		if now-maxEndtime < gracefulPeriod {
			continue
		}

		statefulset, err := clientset.AppsV1().StatefulSets(ns).Get(ctx, buildkitStefulset, v1.GetOptions{})

		if err != nil {
			klog.Errorf("failed to get statefullsets from ns: %s, err: %s", ns, err.Error())
			continue
		}

		statefulset.Spec.Replicas = &zero

		_, err = clientset.AppsV1().StatefulSets(ns).Update(ctx, statefulset, v1.UpdateOptions{})
		if err != nil {
			klog.Errorf("failed to update statefullsets from ns: %s, err: %s", ns, err.Error())
			continue
		}
	}

	return nil
}
