// Copyright 2026 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// BuildsWaitingForLease tracks the number of build requests waiting to acquire a lease on a buildkit pod
	// Labels: namespace (buildkit namespace)
	BuildsWaitingForLease = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "deploy_agent_builds_waiting_for_lease",
		Help: "Number of build requests currently waiting to acquire a lease on a buildkit pod",
	}, []string{"namespace"})

	// BuildsActive tracks the number of builds currently running (have acquired lease and are building)
	// Labels: namespace (buildkit namespace)
	BuildsActive = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "deploy_agent_builds_active",
		Help: "Number of builds currently running (acquired buildkit pod and actively building)",
	}, []string{"namespace"})

	// BuildsTotal counts the total number of build requests received
	// Labels: namespace (buildkit namespace), kind (build kind)
	BuildsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "deploy_agent_builds_total",
		Help: "Total number of build requests received",
	}, []string{"namespace", "kind"})

	// BuildDuration tracks the duration of builds
	// Labels: namespace (buildkit namespace)
	BuildDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "deploy_agent_builds_duration_seconds",
		Help:    "Duration of builds in seconds",
		Buckets: prometheus.ExponentialBuckets(10, 2, 10), // 10s, 20s, 40s, 80s, 160s, 320s, 640s, 1280s, 2560s, 5120s
	}, []string{"namespace"})
)
