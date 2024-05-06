package scaler

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsuru/deploy-agent/pkg/build/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/ptr"
)

func TestRunDownscaler(t *testing.T) {
	ctx := context.Background()

	lastBuild := time.Now().Add(-3 * time.Hour).Unix()

	cli := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit-0",
				Namespace: "default",
				Labels: map[string]string{
					"app": "buildkit",
				},

				Annotations: map[string]string{
					metadata.DeployAgentLastBuildEndingTimeLabelKey: strconv.Itoa(int(lastBuild)),
				},
			},
			Spec: corev1.PodSpec{},
		},
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit",
				Namespace: "default",

				Annotations: map[string]string{
					metadata.DeployAgentLastReplicasAnnotationKey: "3",
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To(int32(1)),
			},
		},
	)

	err := RunDownscaler(ctx, cli, "app=buildkit", "buildkit")
	assert.NoError(t, err)

	rs, err := cli.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	assert.NoError(t, err)

	require.Len(t, rs.Items, 1)
	assert.Equal(t, int32(0), *rs.Items[0].Spec.Replicas)
}

func TestRunDownscalerWithEarlyBuild(t *testing.T) {
	ctx := context.Background()

	lastBuild := time.Now().Add(-30 * time.Minute).Unix()

	cli := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit-0",
				Namespace: "default",
				Labels: map[string]string{
					"app": "buildkit",
				},

				Annotations: map[string]string{
					metadata.DeployAgentLastBuildEndingTimeLabelKey: strconv.Itoa(int(lastBuild)),
				},
			},
			Spec: corev1.PodSpec{},
		},
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit",
				Namespace: "default",

				Annotations: map[string]string{
					metadata.DeployAgentLastReplicasAnnotationKey: "3",
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To(int32(1)),
			},
		},
	)

	err := RunDownscaler(ctx, cli, "app=buildkit", "buildkit")
	assert.NoError(t, err)

	rs, err := cli.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	assert.NoError(t, err)

	require.Len(t, rs.Items, 1)
	assert.Equal(t, int32(1), *rs.Items[0].Spec.Replicas)
}

func TestRunDownscalerWithOnePodBuilding(t *testing.T) {
	ctx := context.Background()

	lastBuild := time.Now().Add(-3 * time.Hour).Unix()

	cli := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit-0",
				Namespace: "default",
				Labels: map[string]string{
					"app": "buildkit",
				},

				Annotations: map[string]string{
					metadata.DeployAgentLastBuildEndingTimeLabelKey: strconv.Itoa(int(lastBuild)),
				},
			},
			Spec: corev1.PodSpec{},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit-1",
				Namespace: "default",
				Labels: map[string]string{
					"app": "buildkit",
				},

				Annotations: map[string]string{}, // this pod is building for some app
			},
			Spec: corev1.PodSpec{},
		},
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit",
				Namespace: "default",

				Annotations: map[string]string{
					metadata.DeployAgentLastReplicasAnnotationKey: "3",
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To(int32(2)),
			},
		},
	)

	err := RunDownscaler(ctx, cli, "app=buildkit", "buildkit")
	assert.NoError(t, err)

	rs, err := cli.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	assert.NoError(t, err)

	require.Len(t, rs.Items, 1)
	assert.Equal(t, int32(2), *rs.Items[0].Spec.Replicas)
}

func TestRunDownscalerWithManyPods(t *testing.T) {
	ctx := context.Background()

	lastBuild := time.Now().Add(-3 * time.Hour).Unix()

	cli := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit-0",
				Namespace: "default",
				Labels: map[string]string{
					"app": "buildkit",
				},

				Annotations: map[string]string{
					metadata.DeployAgentLastBuildEndingTimeLabelKey: strconv.Itoa(int(lastBuild)),
				},
			},
			Spec: corev1.PodSpec{},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit-1",
				Namespace: "default",
				Labels: map[string]string{
					"app": "buildkit",
				},

				Annotations: map[string]string{
					metadata.DeployAgentLastBuildEndingTimeLabelKey: strconv.Itoa(int(lastBuild)),
				},
			},
			Spec: corev1.PodSpec{},
		},
		&appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "buildkit",
				Namespace: "default",

				Annotations: map[string]string{
					metadata.DeployAgentLastReplicasAnnotationKey: "3",
				},
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To(int32(2)),
			},
		},
	)

	err := RunDownscaler(ctx, cli, "app=buildkit", "buildkit")
	assert.NoError(t, err)

	rs, err := cli.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{})
	assert.NoError(t, err)

	require.Len(t, rs.Items, 1)
	assert.Equal(t, int32(0), *rs.Items[0].Spec.Replicas)
}
