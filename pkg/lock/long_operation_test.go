package lock

import (
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type leaseStub struct {
	mu       sync.Mutex
	err      error
	renewals int
}

func (s *leaseStub) Lock() error { return nil }
func (s *leaseStub) Unlock()     {}
func (s *leaseStub) Extend() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.renewals++
	return s.err
}

func (s *leaseStub) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.renewals
}

func TestLongOperationRenewsUntilItCannot(t *testing.T) {
	for name, err := range map[string]error{
		"lost ownership": errLockLost,
		"redis error":    errors.New("redis unavailable"),
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := &leaseStub{}
				l := &longOperation{lock: s, timeout: 3 * time.Second}
				require.NoError(t, l.Lock())
				defer l.Unlock()
				synctest.Wait()
				time.Sleep(time.Second)
				synctest.Wait()
				require.Equal(t, 1, s.count())

				s.mu.Lock()
				s.err = err
				s.mu.Unlock()
				time.Sleep(time.Second)
				synctest.Wait()
				require.Equal(t, 2, s.count())
				time.Sleep(2 * time.Second)
				synctest.Wait()
				require.Equal(t, 2, s.count())
			})
		})
	}
}
