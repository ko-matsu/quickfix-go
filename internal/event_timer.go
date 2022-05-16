package internal

import (
	"fmt"
	"sync"
	"time"

	"go.uber.org/atomic"
)

type EventTimer struct {
	f        func()
	timer    *time.Timer
	done     chan struct{}
	wg       sync.WaitGroup
	rst      chan time.Duration
	rstLock  sync.Mutex
	isClosed *atomic.Bool
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
				fmt.Printf("EventTimer.run reset,%d\n", rstTime)
				if !t.timer.Stop() {
					select { // cleanup
					case <-t.timer.C:
					default:
					}
				}
				fmt.Printf("EventTimer.run reset call\n")
				t.timer.Reset(rstTime)
				fmt.Printf("EventTimer.run reset called\n")
			}
		}
	}()

	return t
}

func (t *EventTimer) Stop() {
	if t == nil {
		return
	}

	fmt.Printf("EventTimer.Stop start\n")
	t.isClosed.Store(true)
	close(t.done)

	fmt.Printf("EventTimer.Stop waiting\n")
	t.wg.Wait()
	fmt.Printf("EventTimer.Stop end\n")
}

func (t *EventTimer) Reset(timeout time.Duration) {
	if t == nil {
		return
	}

	go func() {
		if !t.rstLock.TryLock() {
			fmt.Println("EventTimer.Reset already locked")
			return
		}

		t.rstLock.Lock()
		defer t.rstLock.Unlock()
		if !t.isClosed.Load() {
			fmt.Printf("EventTimer.Reset(%d)\n", timeout)
			select {
			case <-t.done:
			case t.rst <- timeout:
			}
			fmt.Printf("EventTimer.Reset(%d) send\n", timeout)
		}
	}()
}

func newStoppedTimer() *time.Timer {
	timer := time.NewTimer(time.Second)
	if !timer.Stop() {
		<-timer.C
	}
	return timer
}
