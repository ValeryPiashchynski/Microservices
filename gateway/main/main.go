package main

import (
	"errors"
	"flag"
	"fmt"
	ilog "log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"Microservices/gateway"

	"github.com/go-kit/kit/log"
	"github.com/gorilla/handlers"
)

func init() {
	ilog.SetFlags(ilog.Lmicroseconds)
}

func main() {
	var (
		httpPort = flag.String("http.port", "8000", "Address for HTTP server")
		useTLS   = flag.Bool("use.tls", false, "TLS for https")
	)

	flag.Parse()

	gwAddr, _ := externalIP() // get own local ip for send to consul

	//collector := &gateway.Collector{
	//	Processor: &gateway.Fake{},
	//	MaxBatchSize:5,
	//	WorkerCount:3,
	//	QueueSize:10000,
	//	FlushInterval:5*time.Second,
	//}

	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(os.Stderr)
		logger = log.With(logger, "ts", log.DefaultTimestampUTC)
		logger = log.With(logger, "caller", log.DefaultCaller)
	}

	errc := make(chan error)
	//stopChan := make(chan struct{})

	r := gateway.MakeHttpHandler()

	// HTTP transport
	go func() {
		logger.Log("transport", "HTTPS", "addr", net.JoinHostPort(gwAddr, *httpPort))
		handler := r
		var loggetRoute = handlers.LoggingHandler(os.Stdout, handler)

		//http.Serve(loggetRoute, gateway.NewCollectorHandler(collector))

		if *useTLS {
			errc <- http.ListenAndServeTLS(net.JoinHostPort(gwAddr, *httpPort), "config/server.crt", "config/server.key", loggetRoute)
		} else {
			errc <- http.ListenAndServe(net.JoinHostPort(gwAddr, *httpPort), loggetRoute)
		}
	}()

	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
		errc <- fmt.Errorf("%s", <-c)
		//close(stopChan) //close channel
	}()

	//collector.Run(stopChan) //Stop collector

	chErr := <-errc
	logger.Log("exit", chErr)
}

func externalIP() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue // not an ipv4 address
			}
			return ip.String(), nil
		}
	}
	return "", errors.New("not connected")
}
