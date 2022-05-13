package internal

import (
	"fmt"
	"strconv"
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
	isClosed *atomic.Bool
}

func NewEventTimer(task func()) *EventTimer {
	t := &EventTimer{
		f:        task,
		timer:    newStoppedTimer(),
		done:     make(chan struct{}),
		rst:      make(chan time.Duration, 5), // for anti-blocking
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

			case rstTime, ok := <-t.rst:
				fmt.Printf("EventTimer.run reset,%s\n", strconv.FormatBool(ok))
				if !ok {
					continue
				}
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
	close(t.rst)
	fmt.Printf("EventTimer.Stop end\n")
}

func (t *EventTimer) Reset(timeout time.Duration) {
	if t == nil {
		return
	}

	if !t.isClosed.Load() {
		fmt.Printf("EventTimer.Reset(%d)\n", timeout)
		t.rst <- timeout
		fmt.Printf("EventTimer.Reset(%d) send\n", timeout)
	}
}

func newStoppedTimer() *time.Timer {
	timer := time.NewTimer(time.Second)
	if !timer.Stop() {
		<-timer.C
	}
	return timer
}
