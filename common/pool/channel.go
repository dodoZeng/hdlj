package pool

import (
	"container/list"
	"errors"
	"fmt"
	"sync"
)

type channelPool struct {
	// storage for our interface{} connections
	mu   sync.RWMutex
	objs chan interface{}

	// interface{} generator
	factory Factory
}

type Factory func() (interface{}, error)

// NewChannelPool returns a new pool based on buffered channels with an initial
// capacity and maximum capacity. Factory is used when initial capacity is
// greater than zero to fill the pool. A zero initialCap doesn't fill the Pool
// until a new Get() is called. During a Get(), If there is no new connection
// available in the pool, a new connection will be created via the Factory()
// method.
func NewChannelPool(initialCap, maxCap int, factory Factory) (Pool, error) {
	if initialCap < 0 || maxCap <= 0 || initialCap > maxCap {
		return nil, errors.New("invalid capacity settings")
	}

	c := &channelPool{
		objs:    make(chan interface{}, maxCap),
		factory: factory,
	}

	// create initial connections, if something goes wrong,
	// just close the pool error out.
	for i := 0; i < initialCap; i++ {
		obj, err := factory()
		if err != nil {
			return nil, fmt.Errorf("factory is not able to fill the pool: %s", err)
		}
		c.objs <- obj
	}

	return c, nil
}

func (c *channelPool) getObjsAndFactory() (chan interface{}, Factory) {
	c.mu.RLock()
	objs := c.objs
	factory := c.factory
	c.mu.RUnlock()
	return objs, factory
}

func (c *channelPool) Get() (interface{}, error) {
	objs, factory := c.getObjsAndFactory()
	if objs == nil {
		return nil, errors.New("not init")
	}

	select {
	case obj := <-objs:
		if obj == nil {
			return nil, errors.New("empty")
		}

		return obj, nil
	default:
		obj, err := factory()
		if err != nil {
			return nil, err
		}

		return obj, nil
	}
}

func (c *channelPool) Put(obj interface{}) error {
	if obj == nil {
		return errors.New("obj is nil. rejecting")
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.objs == nil {
		return nil
	}

	select {
	case c.objs <- obj:
		return nil
	default:
		return nil
	}
}

func (c *channelPool) Iterate(fun IterateFun) (bool, string) {
	if fun == nil {
		return false, "iterateFun is void"
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.objs == nil {
		return false, "pool is not initialised."
	}

	l := list.New()
	stop_flag := false
	ok := false
	err := ""
	for {
		if stop_flag {
			break
		}

		select {
		case obj := <-c.objs:
			l.PushBack(obj)
			if b, e := fun(obj); !b {
				stop_flag = true
				err = e
				break
			}
		default:
			stop_flag = true
			ok = true
			break
		}
	}

	for e := l.Front(); e != nil; e = e.Next() {
		c.objs <- e.Value
	}

	return ok, err
}

func (c *channelPool) Len() int {
	objs, _ := c.getObjsAndFactory()
	return len(objs)
}
