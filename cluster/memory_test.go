package cluster

import (
	"testing"

	"github.aaf.cloud/platform/websocket-service/cluster/clustertest"
)

func TestMemoryCluster(t *testing.T) {
	a := NewMemoryCluster()
	b := JoinMemoryCluster(a)
	c := JoinMemoryCluster(a)
	clustertest.Test(t, &clustertest.Cluster{
		Cluster:         a,
		ServiceRequests: a.ServiceRequests(),
	}, &clustertest.Cluster{
		Cluster:         b,
		ServiceRequests: b.ServiceRequests(),
	}, &clustertest.Cluster{
		Cluster:         c,
		ServiceRequests: c.ServiceRequests(),
	})
}
