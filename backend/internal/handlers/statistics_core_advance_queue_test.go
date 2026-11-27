package handlers

import (
	"testing"
	"time"
)

func TestCoreAdvanceQueueBoundedFIFO(t *testing.T) {
	var q coreAdvanceQueue
	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	done := make(chan int, coreAdvanceQueueCapacity)
	if !q.submit(func() { close(started); <-release }) {
		t.Fatal("first submit failed")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker not started")
	}
	for i := range coreAdvanceQueueCapacity {
		if !q.submit(func() { done <- i }) {
			t.Fatal("queue rejected available capacity")
		}
	}
	if q.submit(func() { t.Error("over-capacity work ran") }) {
		t.Fatal("queue exceeded bound")
	}
	release <- struct{}{}
	for i := range coreAdvanceQueueCapacity {
		select {
		case got := <-done:
			if got != i {
				t.Fatalf("FIFO order: got %d, want %d", got, i)
			}
		case <-time.After(time.Second):
			t.Fatal("queued work did not drain")
		}
	}
}
