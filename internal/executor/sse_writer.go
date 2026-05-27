package executor

import (
	"bufio"
	"fmt"
	"io"
	"sync"
)

// sseLogger is an optional SSE broadcaster injected into the executor
type SSELogger interface {
	BroadcastTaskLog(taskID, line string)
}

// taskLogWriter wraps a file and optionally broadcasts each line to SSE
type taskLogWriter struct {
	taskID  string
	file    io.WriteCloser
	reader  *io.PipeReader
	writer  *io.PipeWriter
	mu      sync.Mutex
	onLine  func(taskID, line string)
	closed  bool
}

func newTaskLogWriter(taskID string, file io.WriteCloser, onLine func(taskID, line string)) *taskLogWriter {
	w := &taskLogWriter{
		taskID: taskID,
		file:   file,
		onLine: onLine,
	}
	// Use a pipe so we can read back what was written and broadcast
	r, pipeW := io.Pipe()
	w.reader = r
	w.writer = pipeW

	// Background: read from pipe and broadcast + write to file
	go w.scanLines(file)

	return w
}

func (w *taskLogWriter) scanLines(file io.WriteCloser) {
	defer w.reader.Close()
	scanner := bufio.NewScanner(w.reader)
	// Increase scanner buffer for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// Broadcast to SSE if callback set
		if w.onLine != nil {
			w.onLine(w.taskID, line)
		}
		// Write to file
		fmt.Fprintln(file, line)
	}
	file.Close()
}

func (w *taskLogWriter) Write(p []byte) (n int, err error) {
	// Write to pipe so scanLines can broadcast + persist
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, fmt.Errorf("writer closed")
	}
	_, err = w.writer.Write(p)
	return len(p), err
}

func (w *taskLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	w.writer.Close()
	return nil
}
