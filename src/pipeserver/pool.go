package pipeserver

import (
	"errors"
	"time"
	"sync"
	"fmt"
	"net"
)

type Pool interface {
	Get() (net.Conn, error)
	Put(net.Conn) error
	Close(net.Conn) error
	Release()
	Size() int
}

type PoolConfig struct {
	InitialCap int
	MaxCap int
	Factory func() (net.Conn, error)
	Destroy func(net.Conn) error
	IdleTimeout int
}

type ConnWrap struct {
	conn net.Conn
	t    time.Time
}

type connectionPool struct {
	sync.Mutex
	conns       chan *ConnWrap
	factory     func() (net.Conn, error)
	destory     func(net.Conn) error
	idleTimeout time.Duration
}

func NewConnectionPool(poolConfig *PoolConfig) (Pool, error) {
	if poolConfig.InitialCap < 0 || poolConfig.MaxCap <= 0 || poolConfig.InitialCap > poolConfig.MaxCap {
		return nil, errors.New("invalid capacity settings")
	}

	c := &connectionPool{
		conns       : make(chan *ConnWrap, poolConfig.MaxCap),
		factory     : poolConfig.Factory,
		destory     : poolConfig.Destroy,
		idleTimeout : time.Duration(poolConfig.IdleTimeout) * time.Second,
	}

	for i := 0; i < poolConfig.InitialCap; i++ {
		conn, err := c.factory()
		if err != nil {
			return nil, fmt.Errorf("factory is not able to fill the pool: %s", err)
		}

		c.conns <- &ConnWrap{
			conn : conn,
			t    : time.Now(),
		}
	}

	return c, nil
}

func (c *connectionPool) Get() (net.Conn, error) {
	c.Lock()
	defer c.Unlock()

	for {
		select {
		case connWrap := <-c.conns:
			if connWrap == nil {
				continue
			}

			if connWrap.t.Add(c.idleTimeout).Before(time.Now()) {
				c.destory(connWrap.conn)
				continue
			}
			return connWrap.conn, nil
		default:
			conn, err := c.factory()
			if err != nil {
				return nil, err
			}

			return conn, nil
		}
	}
}

func (c *connectionPool) Put(conn net.Conn) error {
	if conn == nil {
		return errors.New("unkown connection")
	}

	c.Lock()
	defer c.Unlock()

	if c.conns == nil {
		return c.destory(conn)
	}

	select {
	case c.conns <- &ConnWrap{conn: conn, t: time.Now()}:
		return nil
	default:
		return c.destory(conn)
	}
}

func (c *connectionPool) Close(conn net.Conn) error {
	if conn == nil {
		return errors.New("unkown connection")
	}

	return c.destory(conn)
}

func (c *connectionPool) Release() {
	c.Lock()
	defer c.Unlock()

	for connWrap := range c.conns {
		c.destory(connWrap.conn)
	}

	close(c.conns)
}

func (c *connectionPool) Size() int {
	return len(c.conns)
}
