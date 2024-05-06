// Copyright 2024 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package metadata

const (
	DeployAgentLastReplicasAnnotationKey   = "deploy-agent.tsuru.io/last-replicas"
	DeployAgentLastBuildStartingLabelKey   = "deploy-agent.tsuru.io/last-build-starting-time"
	DeployAgentLastBuildEndingTimeLabelKey = "deploy-agent.tsuru.io/last-build-ending-time"

	TsuruAppNamespace    = "tsuru"
	TsuruAppNameLabelKey = "tsuru.io/app-name"
	TsuruIsBuildLabelKey = "tsuru.io/is-build"
)
