package main

import (
	"testing"
	"time"
)

func TestWaitForWorkerDrainsAndTimesOut(t *testing.T) {
	done := make(chan struct{})
	drained := make(chan bool, 1)
	go func() { drained <- waitForWorker(done, time.Second) }()
	select {
	case <-drained:
		t.Fatal("returned while worker was active")
	case <-time.After(20 * time.Millisecond):
	}
	close(done)
	if !<-drained {
		t.Fatal("did not observe completion")
	}
	if waitForWorker(make(chan struct{}), time.Millisecond) {
		t.Fatal("expected drain timeout")
	}
}
