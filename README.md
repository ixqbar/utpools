
### USAGE (./utpools -h)
```
Usage: ./utpools [options]
Options:
  -idle int
    	pool connection idle timeout to close (default 3600)
  -max int
    	pool max num (default 20)
  -min int
    	pool min num (default 5)
  -target string
    	target server ip:port (default ":6379")
  -timeout uint
    	timeout to shutdown server (default 60)
  -unix string
    	unix domain socket file (default "/tmp/utpools.sock")
```

### TEST
```
go build utpools.go
./utpools
{YOUR REDIS BIN PATH}/redis-cli -s /tmp/utpools.sock
```

### FAQ

更多疑问请+qq群 233415606 or [website http://www.hnphper.com](http://www.hnphper.com)
