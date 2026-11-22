package proxy

import "github.com/zhangyw-cn/httpproxy/internal/flow"

type FlowSink interface {
	OnCreated(flow.Flow)
	OnUpdated(flow.Flow)
}
