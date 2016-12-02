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
	"runtime"
	"flag"
)

var optionTargetListen    = flag.String("target", ":6379", "target server ip:port")
var optionMinNum          = flag.Int("min", 5, "pool min num")
var optionMaxNum          = flag.Int("max", 20, "pool max num")
var optionIdleTimeout     = flag.Int("idle", 3600, "pool connection idle timeout to close")
var optionShutdownTimeout = flag.Uint("timeout", 60, "timeout to shutdown server")
var optionUnixDomainFile  = flag.String("unix", "/tmp/utpools.sock", "unix domain socket file")
var optionVerbose         = flag.Int("verbose", 0, `show run details`)

func usage() {
	fmt.Printf("Usage: %s [options]\nOptions:\n", os.Args[0])
	flag.PrintDefaults()
	os.Exit(0)
}

func main() {
	flag.Usage = usage
	flag.Parse()

	runtime.GOMAXPROCS(runtime.NumCPU())

	if (*optionVerbose == 1) {
		os.Setenv("DEBUG", "ok")
	}

	config := &pipeserver.PoolConfig{
		InitialCap  : *optionMinNum,
		MaxCap      : *optionMaxNum,
		IdleTimeout : *optionIdleTimeout,
		Factory     : func() (net.Conn, error) {return net.Dial("tcp", *optionTargetListen)},
		Destroy     : func(conn net.Conn) error {return conn.Close()},
	}

	pools, err := pipeserver.NewConnectionPool(config)
	if err != nil {
		fmt.Printf("fail init connection pool %v\n", err)
		os.Exit(1)
	}

	connWaitGroup := &sync.WaitGroup{}

	listener, err := net.ListenUnix("unix", &net.UnixAddr{*optionUnixDomainFile, "unix"})
	if err != nil {
		fmt.Printf("fail recover socket from file %v\n", err)
		os.Exit(1)
	}

	defer func() {
		listener.Close()
		pools.Release()
	}()

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

		tt := time.NewTimer(time.Second * time.Duration(*optionShutdownTimeout))
		wait := make(chan struct{})
		go func() {
			connWaitGroup.Wait()
			wait <- struct{}{}
		}()

		select {
		case <-tt.C:
		case <-wait:
		}

		os.Remove(*optionUnixDomainFile)
		break;
	}

	os.Exit(0)
}

func handleConn(pool pipeserver.Pool, conn net.Conn) error {
	defer conn.Close()

	pipeserver.Debugf("client connected and pool size %d\n", pool.Size())

	target, err := pool.Get()
	if err != nil {
		return fmt.Errorf("can't connect target")
	}

	pipeserver.Debugf("client to target and pool size %d\n", pool.Size())

	Pipe(pool, conn, target)

	return nil
}

func chanFromConn(pool pipeserver.Pool, conn net.Conn) chan []byte {
	c := make(chan []byte)

	go func() {
		b := make([]byte, 1024)

		for {
			n, err := conn.Read(b)
			if n > 0 {
				res := make([]byte, n)
				copy(res, b[:n])
				c <- res
			}

			if err != nil {
				c <- nil
				break
			}
		}

		close(c)
		if pool != nil {
			pool.Put(conn)
			pipeserver.Debugf("client disconnected and pool size %d\n", pool.Size())
		}
	}()

	return c
}


func Pipe(pool pipeserver.Pool, src net.Conn, dst net.Conn) {
	chan1 := chanFromConn(nil, src)
	chan2 := chanFromConn(pool, dst)

	for {
		select {
		case b1 := <-chan1:
			if b1 == nil {
				return
			} else {
				dst.Write(b1)
			}
		case b2 := <-chan2:
			if b2 == nil {
				return
			} else {
				src.Write(b2)
			}
		}
	}
}