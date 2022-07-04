package loadbalancer

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

type algo int

const (
	RoundRobin algo = iota
	LeastConnections
)

type serverSelector interface {
	choose() *server
	after(*server)
}

type LoadBalancer struct {
	servers []*server
	port     int
	selector serverSelector
}

func newSelector(algo algo, servers []*server) serverSelector {
	switch (algo) {
	case RoundRobin:
		return newRoundRobin(servers)
	case LeastConnections:
		return newLeastConnections(servers)
	}
	panic("invalid load balancing algorithm")
}

func NewLoadBalancer(port int, algoStr string, urls []string) (*LoadBalancer, error) {
	if port < 1024 || port > 65535 {
		return nil, fmt.Errorf("port out of range 1024 < p < 65535 [%d]", port)
	}

	var algo algo
	switch (algoStr) {
	case "lc":
		algo = LeastConnections
	case "rr":
		algo = RoundRobin
	default:
		return nil, fmt.Errorf("algo choice not lc or rr : %s", algoStr)
	}

	serverLen := len(urls)
	if serverLen == 0 {
		return nil, fmt.Errorf("must provide at least one server")
	}
	servers := make([]*server, serverLen)
	for i, addr := range urls {
		server, err := newServer(addr)
		if err != nil {
			return nil, fmt.Errorf("newServer: %w", err)
		}
		servers[i] = server
	}

	return &LoadBalancer{
		servers,
		port,
		newSelector(algo, servers),
	}, nil
}

func (lb *LoadBalancer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		
		srvr := lb.selector.choose()
		// Keep finding new requests until one works or we run out of servers
		for srvr != nil {
			srvr.proxy.ServeHTTP(w, r)
			if srvr.alive { // Hacky way to tell if request went through
				lb.selector.after(srvr)
				return
			}
			srvr = lb.selector.choose()
		}
		w.WriteHeader(503) 
	}
}

func (lb *LoadBalancer) printStatus() {
	for _, server := range lb.servers {
		println(server.host, ":",  server.alive)
	}
}

func (lb *LoadBalancer) startHealthCheck() func() {
	ticker := time.NewTicker(time.Second * 20)

	for {
		<-ticker.C
		for _, server := range lb.servers {
			alive := isAlive(server.host)
			if alive != server.alive {
				server.mu.Lock()
				server.alive = alive
				server.mu.Unlock()
			}
		}
		lb.printStatus()
	}
}

func (lb *LoadBalancer) Start() {
	lb.printStatus()
	http.HandleFunc("/", lb.handler())
	go lb.startHealthCheck()
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", lb.port), nil))
}
