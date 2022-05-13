package internal

import (
	"fmt"
	"strconv"
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
	t.Lock()
	t.isClosed = true
	close(t.done)
	close(t.rst)
	t.Unlock()

	fmt.Printf("EventTimer.Stop waiting\n")
	t.wg.Wait()
	fmt.Printf("EventTimer.Stop end\n")
}

func (t *EventTimer) Reset(timeout time.Duration) {
	if t == nil {
		return
	}

	t.Lock()
	defer t.Unlock()
	if !t.isClosed {
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
