package main

import (
	"net"
	"fmt"
	"os"
	"pipeserver"
	"os/signal"
	"syscall"
	"time"
	"sync"
)

func main() {
	var config = &pipeserver.PoolConfig{
		InitialCap  : 5,
		MaxCap      : 20,
		Factory     : func() (net.Conn, error) {return net.Dial("tcp", ":6379")},
		Destroy     : func(conn net.Conn) error {return conn.Close()},
		IdleTimeout : 3600,
	}

	pools, err := pipeserver.NewConnectionPool(config)
	if err != nil {
		fmt.Printf("fail init connection pool %v\n", err)
		os.Exit(1)
	}

	var unixSocket = "/tmp/unix.sock"
	var connWaitGroup = &sync.WaitGroup{}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{unixSocket, "unix"})
	if err != nil {
		fmt.Printf("fail recover socket from file %v\n", err)
		os.Exit(1)
	}

	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
					break;
				}
				continue
			}

			go func() {
				connWaitGroup.Add(1)
				handleConn(pools, conn)
				connWaitGroup.Done()
			}()
		}
	}()

	sigs := make(chan os.Signal)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)

	for s := range sigs {
		fmt.Printf("receive shutdown signal %v\n", s)
		listener.SetDeadline(time.Now())

		tt := time.NewTimer(time.Second * time.Duration(30))
		wait := make(chan struct{})
		go func() {
			connWaitGroup.Wait()
			wait <- struct{}{}
		}()

		select {
		case <-tt.C:
		case <-wait:
		}

		os.Remove(unixSocket)
		break;
	}
}

func handleConn(pool pipeserver.Pool, conn net.Conn) error {
	defer conn.Close()

	fmt.Printf("client connected and pool size %d\n", pool.Size())

	target, err := pool.Get()
	if err != nil {
		return fmt.Errorf("can't connect target")
	}

	fmt.Printf("client to target and pool size %d\n", pool.Size())

	defer func() {
		pool.Put(target)
		fmt.Printf("client disconnected and pool size %d\n", pool.Size())
	}()

	Pipe(conn, target)

	return nil
}

func chanFromConn(conn net.Conn) chan []byte {
	c := make(chan []byte)

	go func() {
		b := make([]byte, 1024)

		for {
			n, err := conn.Read(b)
			if n > 0 {
				res := make([]byte, n)
				// Copy the buffer so it doesn't get changed while read by the recipient.
				copy(res, b[:n])
				c <- res
			}
			if err != nil {
				c <- nil
				break
			}
		}
	}()

	return c
}


func Pipe(conn1 net.Conn, conn2 net.Conn) {
	chan1 := chanFromConn(conn1)
	chan2 := chanFromConn(conn2)

	defer func() {
		close(chan1)
		close(chan2)
	}()

	for {
		select {
		case b1 := <-chan1:
			if b1 == nil {
				return
			} else {
				conn2.Write(b1)
			}
		case b2 := <-chan2:
			if b2 == nil {
				return
			} else {
				conn1.Write(b2)
			}
		}
	}
}