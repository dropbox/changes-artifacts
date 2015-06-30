package common

// Lifted from https://github.com/buddyfs/buddystore/blob/master/clock.go

import (
	"sync"
	"time"

	"github.com/stretchr/testify/mock"
)

type Clock interface {
	Now() time.Time
	AfterFunc(time.Duration, func()) *time.Timer
}

type RealClock struct {
	// Implements:
	// ClockIface
}

var _ Clock = new(RealClock)

func (r *RealClock) Now() time.Time {
	return time.Now()
}

func (r *RealClock) AfterFunc(d time.Duration, f func()) *time.Timer {
	return time.AfterFunc(d, f)
}

type MockClock struct {
	frozen      bool
	currentTime time.Time
	lock        sync.RWMutex

	// AfterFunc simulation
	nextEvent      func()
	nextEventTimer time.Time
	nextEventSet   bool

	mock.Mock
	// Implements:
	// ClockIface
}

var _ Clock = new(MockClock)

func NewMockClock() *MockClock {
	return new(MockClock)
}

func NewFrozenClock() *MockClock {
	return new(MockClock).Freeze()
}

func NewRealClock() Clock {
	return new(RealClock)
}

func (m *MockClock) Freeze() *MockClock {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.frozen = true
	m.currentTime = time.Now()

	return m
}

func (m *MockClock) Advance(d time.Duration) *MockClock {
	m.lock.Lock()

	if !m.frozen {
		m.lock.Unlock()
		panic("Cannot advance live clock. Call MockClock.Freeze() first.")
	}

	m.currentTime = m.currentTime.Add(d)
	m.lock.Unlock()

	m.lock.RLock()
	defer m.lock.RUnlock()

	if m.nextEventSet {
		if m.currentTime.After(m.nextEventTimer) {
			// TODO: This is a synchronous call to avoid race conditions.
			m.nextEvent()
		}
	}

	return m
}

func (m *MockClock) Now() time.Time {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if m.frozen {
		return m.currentTime
	}

	return time.Now()
}

func (m *MockClock) AfterFunc(d time.Duration, f func()) *time.Timer {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.Mock.Called(d, f)

	m.nextEventTimer = m.currentTime.Add(d)
	m.nextEvent = f
	m.nextEventSet = true

	return time.NewTimer(d)
}
