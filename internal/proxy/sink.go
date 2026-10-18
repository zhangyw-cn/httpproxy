package proxy

import "httpproxy/internal/flow"

type FlowSink interface {
	OnCreated(flow.Flow)
	OnUpdated(flow.Flow)
}
