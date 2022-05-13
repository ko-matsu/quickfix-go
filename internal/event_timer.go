package internal

import (
	"sync"
	"time"

	"go.uber.org/atomic"
)

type EventTimer struct {
	f          func()
	timer      *time.Timer
	done       chan struct{}
	wg         sync.WaitGroup
	rst        chan time.Duration
	isClosed   *atomic.Bool
	curTimeout time.Duration
}

func NewEventTimer(task func()) *EventTimer {
	t := &EventTimer{
		f:        task,
		timer:    newStoppedTimer(),
		done:     make(chan struct{}),
		rst:      make(chan time.Duration),
		isClosed: atomic.NewBool(false),
	}

	t.wg.Add(1)
	go func() {
		defer t.wg.Done()

		for {
			select {

			case <-t.timer.C:
				t.f()

			case <-t.done:
				t.timer.Stop()
				return

			case rstTime := <-t.rst:
				if !t.timer.Stop() {
					select { // cleanup
					case <-t.timer.C:
					default:
					}
				}
				t.timer.Reset(rstTime)
			}
		}
	}()

	return t
}

func (t *EventTimer) Stop() {
	if t == nil {
		return
	}

	t.isClosed.Store(true)
	close(t.done)
	t.wg.Wait()
	close(t.rst)
}

func (t *EventTimer) Reset(timeout time.Duration) {
	if t == nil {
		return
	}
	if t.curTimeout == timeout {
		return
	}

	if !t.isClosed.Load() {
		t.rst <- timeout
		t.curTimeout = timeout
	}
}

func newStoppedTimer() *time.Timer {
	timer := time.NewTimer(time.Second)
	if !timer.Stop() {
		<-timer.C
	}
	return timer
}
