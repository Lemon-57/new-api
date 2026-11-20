package service

import (
	"context"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
)

type moderationJob struct {
	instance string
	workers  int
	bytes    int
	run      func(context.Context)
}

type moderationQueue struct {
	mu          sync.Mutex
	active      map[string]int
	activeTotal int
	queued      map[string]int
	pending     []moderationJob
	pendingSize int
	dropped     uint64
}

type ModerationQueueStatus struct {
	Active  int    `json:"active"`
	Pending int    `json:"pending"`
	Dropped uint64 `json:"dropped"`
}

func newModerationQueue() *moderationQueue {
	return &moderationQueue{active: map[string]int{}, queued: map[string]int{}}
}

var moderationObservations = newModerationQueue()
var moderationRecords = newModerationQueue()

func (queue *moderationQueue) enqueue(job moderationJob, limit int) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.canRun(job) && queue.queued[job.instance] == 0 {
		queue.start(job)
		return true
	}
	if len(queue.pending) >= 1024 || queue.queued[job.instance] >= limit || queue.pendingSize+job.bytes > 16<<20 {
		queue.dropped++
		return false
	}
	queue.queued[job.instance]++
	queue.pendingSize += job.bytes
	queue.pending = append(queue.pending, job)
	return true
}

func (queue *moderationQueue) canRun(job moderationJob) bool {
	return queue.activeTotal < 16 && queue.active[job.instance] < job.workers
}

func (queue *moderationQueue) start(job moderationJob) {
	queue.activeTotal++
	queue.active[job.instance]++
	go func() {
		defer queue.finish(job.instance)
		defer func() {
			if recover() != nil {
				logger.LogWarn(context.Background(), common.LogText("content moderation background task failed"))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		job.run(ctx)
	}()
}

func (queue *moderationQueue) finish(instance string) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.activeTotal--
	queue.active[instance]--
	if queue.active[instance] == 0 {
		delete(queue.active, instance)
	}
	for index := 0; index < len(queue.pending) && queue.activeTotal < 16; {
		job := queue.pending[index]
		if !queue.canRun(job) {
			index++
			continue
		}
		copy(queue.pending[index:], queue.pending[index+1:])
		queue.pending[len(queue.pending)-1] = moderationJob{}
		queue.pending = queue.pending[:len(queue.pending)-1]
		queue.queued[job.instance]--
		if queue.queued[job.instance] == 0 {
			delete(queue.queued, job.instance)
		}
		queue.pendingSize -= job.bytes
		queue.start(job)
	}
}

func (queue *moderationQueue) status() ModerationQueueStatus {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return ModerationQueueStatus{Active: queue.activeTotal, Pending: len(queue.pending), Dropped: queue.dropped}
}

func ModerationQueueStatuses() map[string]ModerationQueueStatus {
	return map[string]ModerationQueueStatus{"observations": moderationObservations.status(), "records": moderationRecords.status(), "notifications": moderationNotifications.status()}
}
