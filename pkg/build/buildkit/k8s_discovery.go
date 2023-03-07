package buildkit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moby/buildkit/client"
	pb "github.com/tsuru/deploy-agent/pkg/build/grpc_build_v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog"
)

const (
	TsuruAppNameLabelKey = "tsuru.io/app-name"
	TsuruIsBuildLabelKey = "tsuru.io/is-build"
)

var noopFunc = func() {}

func discoverBuildKitClient(ctx context.Context, cs *kubernetes.Clientset, opts KubernertesDiscoveryOptions, req *pb.BuildRequest) (*client.Client, func(), error) {
	return discoverBuildKitClientFromApp(ctx, cs, opts, req.App.Name)
}

func discoverBuildKitClientFromApp(ctx context.Context, cs *kubernetes.Clientset, opts KubernertesDiscoveryOptions, app string) (*client.Client, func(), error) {
	leaderCtx, leaderCancel := context.WithCancel(ctx)
	cfns := []func(){
		func() {
			klog.V(4).Infoln("Releasing the main leader lease...")
			leaderCancel()
		},
	}

	pod, err := discoverBuildKitPod(leaderCtx, cs, opts, app)
	if err != nil {
		return nil, cleanUps(cfns...), err
	}

	if opts.SetTsuruAppLabel {
		klog.V(4).Infoln("Setting Tsuru app labels in the pod", pod.Name)

		err = setTsuruAppLabelOnBuildKitPod(ctx, cs, pod.Name, pod.Namespace, app)
		if err != nil {
			return nil, cleanUps(cfns...), fmt.Errorf("failed to set Tsuru app labels on BuildKit's pod: %w", err)
		}

		cfns = append(cfns, func() {
			klog.V(4).Infoln("Removing Tsuru app labels in the pod", pod.Name)
			unsetTsuruAppLabelOnBuildKitPod(context.Background(), cs, pod.Name, pod.Namespace)
		})
	}

	addr := fmt.Sprintf("tcp://%s:%d", pod.Status.PodIP, opts.Port)

	c, err := client.New(ctx, addr, client.WithFailFast())
	if err != nil {
		return nil, cleanUps(cfns...), err
	}

	cfns = append(cfns, func() {
		klog.V(4).Infoln("Closing connection with BuildKit at", addr)
		c.Close()
	})

	klog.V(4).Infoln("Connecting to BuildKit at", addr)

	return c, cleanUps(cfns...), nil
}

func discoverBuildKitPod(ctx context.Context, cs *kubernetes.Clientset, opts KubernertesDiscoveryOptions, app string) (*corev1.Pod, error) {
	// TODO: missing to implement the namespace discovery based on app's pods.
	// TODO: respect deadline to discover pods.

	pods := make(chan *corev1.Pod)
	defer close(pods)

	watchCtx, watchCancel := context.WithCancel(ctx)
	defer watchCancel() // watch cancellation must happen before than closing the pods channel

	go watchBuildKitPods(watchCtx, cs, opts.PodSelector, opts.Namespace, pods)

	selected := make(chan *corev1.Pod, 1)
	defer close(selected)

	leaseCancelByPod := make(map[string]func())

	go func() {
		for pod := range pods {
			if _, found := leaseCancelByPod[pod.Name]; found {
				continue
			}

			leaseCtx, leaseCancel := context.WithCancel(ctx)
			leaseCancelByPod[pod.Name] = leaseCancel

			go acquireLeaseForPod(leaseCtx, cs, selected, pod, opts)
		}
	}()

	pod := <-selected

	for name, leaseCancel := range leaseCancelByPod {
		if pod.Name == name {
			continue
		}

		klog.V(4).Infof("Releasing lock for %s pod", name)
		leaseCancel()
	}

	return pod, nil
}

func watchBuildKitPods(ctx context.Context, cs *kubernetes.Clientset, labelSelector, ns string, pods chan<- *corev1.Pod) error {
	w, err := cs.CoreV1().Pods(ns).Watch(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
		Watch:         true,
	})
	if err != nil {
		return err
	}

	defer w.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case e := <-w.ResultChan():
			if e.Type != watch.Added && e.Type != watch.Modified {
				continue
			}

			pod := e.Object.(*corev1.Pod)
			if isPodReady(pod) {
				pods <- pod
			}

			klog.V(4).Infof("Pod %s/%s is not ready yet", pod.Namespace, pod.Name)
		}
	}
}

func acquireLeaseForPod(ctx context.Context, cs *kubernetes.Clientset, ch chan<- *corev1.Pod, pod *corev1.Pod, opts KubernertesDiscoveryOptions) {
	podname := os.Getenv("POD_NAME")
	if podname == "" {
		podname, _ = os.Hostname()
	}

	klog.V(4).Infof("Attempting to acquire the lease for pod %s/%s under holder name %q...", pod.Namespace, pod.Name, podname)

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", strings.TrimRight(opts.LeasePrefix, "-"), pod.Name),
				Namespace: pod.Namespace,
			},
			Client: cs.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: podname,
			},
		},
		ReleaseOnCancel: true,
		LeaseDuration:   5 * time.Second,
		RenewDeadline:   2 * time.Second,
		RetryPeriod:     500 * time.Millisecond,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(c context.Context) {
				select {
				case ch <- pod:
					klog.V(4).Infof("Selected BuildKit pod: %s/%s", pod.Namespace, pod.Name)

				case <-ctx.Done():
					klog.V(4).Infof("Received context cancelation: %s/%s", pod.Namespace, pod.Name)
				}
			},
			OnStoppedLeading: noopFunc,
		},
	})

	klog.V(4).Infof("Shutting off the lease for %s/%s pod", pod.Namespace, pod.Name)
}

func setTsuruAppLabelOnBuildKitPod(ctx context.Context, cs *kubernetes.Clientset, pod, ns, app string) error {
	patch, err := json.Marshal([]any{
		map[string]any{
			"op":    "replace",
			"path":  fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(TsuruAppNameLabelKey)),
			"value": app,
		},
		map[string]any{
			"op":    "replace",
			"path":  fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(TsuruIsBuildLabelKey)),
			"value": strconv.FormatBool(true),
		},
	})
	if err != nil {
		return err
	}

	_, err = cs.CoreV1().Pods(ns).Patch(ctx, pod, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}

func unsetTsuruAppLabelOnBuildKitPod(ctx context.Context, cs *kubernetes.Clientset, pod, ns string) error {
	patch, err := json.Marshal([]any{
		map[string]any{
			"op":   "remove",
			"path": fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(TsuruAppNameLabelKey)),
		},
		map[string]any{
			"op":   "remove",
			"path": fmt.Sprintf("/metadata/labels/%s", normalizeAppLabelForJSONPatch(TsuruIsBuildLabelKey)),
		},
	})
	if err != nil {
		return err
	}

	_, err = cs.CoreV1().Pods(ns).Patch(ctx, pod, types.JSONPatchType, patch, metav1.PatchOptions{})
	return err
}

func normalizeAppLabelForJSONPatch(s string) string {
	// Replaces ~ and / by ~0 and ~1, respectively
	// See: https://datatracker.ietf.org/doc/html/rfc6902/#appendix-A.14
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

func cleanUps(fns ...func()) func() {
	return func() {
		for i := range fns {
			fn := fns[(len(fns) - i - 1)]
			if fn == nil {
				continue
			}

			fn()
		}
	}
}

func isPodReady(pod *corev1.Pod) bool {
	var ready bool
	for _, c := range pod.Status.Conditions {
		if c.Type != corev1.PodReady {
			continue
		}

		ready = c.Status == corev1.ConditionTrue
	}

	return pod.Status.Phase == corev1.PodRunning && pod.Status.PodIP != "" && ready
}
