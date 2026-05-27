package llmscheduler

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hybrid-gpu-scheduler/pkg/backend"
	"github.com/hybrid-gpu-scheduler/pkg/vram"
)

// Request LLM 推理请求
type Request struct {
	ID         string
	Model      string
	Priority   int       // 0=low, 1=normal, 2=high
	CreatedAt  time.Time
	Stream     bool
	ChatReq    backend.ChatRequest
	ResponseCh chan *Response
	Ctx        context.Context
	Cancel     context.CancelFunc
}

// Response LLM 推理响应
type Response struct {
	Content string
	Done    bool
	Error   error
}

// Task 推理任务记录
type Task struct {
	ID        string
	Model     string
	GPU       string
	Status    string // pending, running, completed, failed
	CreatedAt time.Time
	StartedAt time.Time
	EndedAt   time.Time
}

// Scheduler LLM 推理调度器
type Scheduler struct {
	planner *vram.Planner

	queue    *PriorityQueue
	active   map[string]*Request
	tasks    map[string]*Task
	modelGPU map[string]string // model -> gpu mapping

	mu     sync.RWMutex
	stopCh chan struct{}

	config Config
}

// Config 调度器配置
type Config struct {
	QueueSize         int
	BatchTimeoutMs    int
	MaxBatchSize      int
	ModelUnloadAfter  int // seconds
}

// NewScheduler 创建调度器
func NewScheduler(cfg Config, planner *vram.Planner) *Scheduler {
	pq := &PriorityQueue{}
	heap.Init(pq)

	return &Scheduler{
		planner:  planner,
		queue:    pq,
		active:   make(map[string]*Request),
		tasks:    make(map[string]*Task),
		modelGPU: make(map[string]string),
		stopCh:   make(chan struct{}),
		config:   cfg,
	}
}

// Start 启动调度器
func (s *Scheduler) Start() {
	go s.processLoop()
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

// Submit 提交推理请求
func (s *Scheduler) Submit(req *Request) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.queue.Len() >= s.config.QueueSize {
		return fmt.Errorf("queue full")
	}

	req.CreatedAt = time.Now()
	heap.Push(s.queue, req)

	s.tasks[req.ID] = &Task{
		ID:        req.ID,
		Model:     req.Model,
		Status:    "pending",
		CreatedAt: time.Now(),
	}

	return nil
}

func (s *Scheduler) processLoop() {
	batch := make([]*Request, 0, s.config.MaxBatchSize)
	timer := time.NewTimer(time.Duration(s.config.BatchTimeoutMs) * time.Millisecond)

	for {
		select {
		case <-s.stopCh:
			return
		case <-timer.C:
			if len(batch) > 0 {
				s.processBatch(batch)
				batch = batch[:0]
			}
			timer.Reset(time.Duration(s.config.BatchTimeoutMs) * time.Millisecond)
		default:
			s.mu.Lock()
			if s.queue.Len() > 0 {
				req := heap.Pop(s.queue).(*Request)
				batch = append(batch, req)
			}
			s.mu.Unlock()

			if len(batch) >= s.config.MaxBatchSize {
				s.processBatch(batch)
				batch = batch[:0]
				timer.Reset(time.Duration(s.config.BatchTimeoutMs) * time.Millisecond)
			}
		}
	}
}

func (s *Scheduler) processBatch(batch []*Request) {
	byModel := make(map[string][]*Request)
	for _, req := range batch {
		byModel[req.Model] = append(byModel[req.Model], req)
	}

	for model, requests := range byModel {
		go s.processModelBatch(model, requests)
	}
}

func (s *Scheduler) processModelBatch(model string, requests []*Request) {
	s.mu.Lock()

	// 标记任务为 running
	for _, req := range requests {
		s.active[req.ID] = req
		if task, ok := s.tasks[req.ID]; ok {
			task.Status = "running"
			task.StartedAt = time.Now()
		}
	}

	s.mu.Unlock()

	// 执行推理
	for _, req := range requests {
		s.executeRequest(req)
	}
}

func (s *Scheduler) executeRequest(req *Request) {
	defer func() {
		s.mu.Lock()
		delete(s.active, req.ID)
		if task, ok := s.tasks[req.ID]; ok {
			task.Status = "completed"
			task.EndedAt = time.Now()
		}
		s.mu.Unlock()
	}()

	// 获取主后端
	bm := backendManager
	if bm == nil {
		req.ResponseCh <- &Response{Error: fmt.Errorf("no backend configured"), Done: true}
		return
	}

	b := bm.Primary()
	if b == nil {
		req.ResponseCh <- &Response{Error: fmt.Errorf("no backend available"), Done: true}
		return
	}

	// 设置模型
	req.ChatReq.Model = req.Model

	if req.Stream {
		stream, err := b.ChatStream(req.ChatReq)
		if err != nil {
			req.ResponseCh <- &Response{Error: err, Done: true}
			return
		}
		defer stream.Close()

		buf := make([]byte, 4096)
		for {
			n, err := stream.Read(buf)
			if err != nil {
				break
			}
			req.ResponseCh <- &Response{Content: string(buf[:n]), Done: false}
		}
		req.ResponseCh <- &Response{Done: true}
	} else {
		resp, err := b.Chat(req.ChatReq)
		if err != nil {
			req.ResponseCh <- &Response{Error: err, Done: true}
			return
		}
		req.ResponseCh <- &Response{Content: resp.Message.Content, Done: true}
	}
}

// GetStatus 获取调度器状态
func (s *Scheduler) GetStatus() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"queue_length":   s.queue.Len(),
		"active_count":   len(s.active),
		"models_loaded":  s.modelGPU,
		"gpus":          s.planner.GetGPUStatus(),
	}
}

// GetTask 获取任务信息
func (s *Scheduler) GetTask(id string) *Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks[id]
}

// SetBackendManager 设置后端管理器（由 main.go 调用）
var backendManager *backend.BackendManager

func SetBackendManager(bm *backend.BackendManager) {
	backendManager = bm
}

// PriorityQueue 优先级队列
type PriorityQueue []*Request

func (pq PriorityQueue) Len() int { return len(pq) }

func (pq PriorityQueue) Less(i, j int) bool {
	if pq[i].Priority != pq[j].Priority {
		return pq[i].Priority > pq[j].Priority
	}
	return pq[i].CreatedAt.Before(pq[j].CreatedAt)
}

func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *PriorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*Request))
}

func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}
