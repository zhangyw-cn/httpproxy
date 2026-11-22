package proxy

import (
	"net/http"

	"github.com/zhangyw-cn/httpproxy/internal/flow"
)

const defaultRecordQueue = 64

type flowRecord struct {
	inline bool
	f      *flow.Flow
	reqCap *bodyCapture
	limit  int64
	reqLen int64
}

type recordJob struct {
	begin      bool
	flush      bool
	done       chan struct{}
	f          flow.Flow
	reqCap     *bodyCapture
	respCap    *bodyCapture
	respHeader http.Header
	limit      int64
	reqLen     int64
}

func (p *Proxy) ensureRecorder() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.recStarted {
		return
	}
	n := p.recordQueue
	if n < 1 {
		n = defaultRecordQueue
	}
	p.jobs = make(chan recordJob, n)
	p.recStarted = true
	p.recWG.Add(1)
	go p.recordLoop()
}

func (p *Proxy) waitRecordGate() {
	p.mu.Lock()
	gate := p.recordGate
	p.mu.Unlock()
	if gate != nil {
		<-gate
	}
}

func (p *Proxy) recordLoop() {
	defer p.recWG.Done()
	for {
		p.waitRecordGate()
		job, ok := <-p.jobs
		if !ok {
			return
		}
		p.applyJob(job)
	}
}

func (p *Proxy) stopRecorder() {
	p.recStop.Do(func() {
		p.mu.Lock()
		started := p.recStarted
		jobs := p.jobs
		p.mu.Unlock()
		if !started {
			return
		}
		close(jobs)
		p.recWG.Wait()
	})
}

func (p *Proxy) beginRecord(f *flow.Flow, reqCap *bodyCapture, limit, reqLen int64) *flowRecord {
	p.ensureRecorder()
	rec := &flowRecord{f: f, reqCap: reqCap, limit: limit, reqLen: reqLen}
	snap := *f
	snap.ReqBody = nil
	snap.RespBody = nil
	snap.RespBodyDecoded = nil
	select {
	case p.jobs <- recordJob{begin: true, f: snap}:
		return rec
	default:
		rec.inline = true
		p.created(snap)
		return rec
	}
}

func (p *Proxy) finishRecord(rec *flowRecord, respHeader http.Header, respCap *bodyCapture) {
	if rec == nil {
		return
	}
	finishFlow(rec.f, rec.f.StartedAt)
	job := recordJob{
		f:          *rec.f,
		reqCap:     rec.reqCap,
		respCap:    respCap,
		respHeader: respHeader,
		limit:      rec.limit,
		reqLen:     rec.reqLen,
	}
	job.f.ReqBody = nil
	job.f.RespBody = nil
	job.f.RespBodyDecoded = nil
	if rec.inline {
		p.applyJob(job)
		return
	}
	p.jobs <- job
}

func (p *Proxy) applyJob(job recordJob) {
	if job.done != nil {
		defer close(job.done)
	}
	if job.flush {
		return
	}
	if job.begin {
		p.created(job.f)
		return
	}
	if job.reqCap != nil {
		job.f.ReqBody, job.f.ReqSize, job.f.ReqTruncated = job.reqCap.result()
		if job.reqLen > job.f.ReqSize {
			job.f.ReqSize = job.reqLen
			job.f.ReqTruncated = job.f.ReqSize > job.limit
		}
	}
	if job.respCap != nil {
		job.f.RespBody, job.f.RespSize, job.f.RespTruncated = job.respCap.result()
		job.f.RespBodyDecoded = decodeForDisplay(job.f.RespBody, job.respHeader, job.limit)
	}
	p.updated(job.f)
}
