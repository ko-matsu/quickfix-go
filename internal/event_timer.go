package internal

import (
	"sync"
	"time"
)

type EventTimer struct {
	sync.Mutex
	f        func()
	timer    *time.Timer
	done     chan struct{}
	wg       sync.WaitGroup
	rst      chan time.Duration
	isClosed bool
}

func NewEventTimer(task func()) *EventTimer {
	t := &EventTimer{
		f:        task,
		timer:    newStoppedTimer(),
		done:     make(chan struct{}),
		rst:      make(chan time.Duration),
		isClosed: false,
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

			case rstTime, ok := <-t.rst:
				if !ok {
					continue
				}
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

	t.Lock()
	t.isClosed = true
	close(t.done)
	close(t.rst)
	t.Unlock()

	t.wg.Wait()
}

func (t *EventTimer) Reset(timeout time.Duration) {
	if t == nil {
		return
	}

	t.Lock()
	defer t.Unlock()
	if !t.isClosed {
		t.rst <- timeout
	}
}

func newStoppedTimer() *time.Timer {
	timer := time.NewTimer(time.Second)
	if !timer.Stop() {
		<-timer.C
	}
	return timer
}
