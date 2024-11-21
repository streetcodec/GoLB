package main

import (
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"sync"
	"time"
)

func main() {
	servers := []*Server{
		{URL: "http://www.google.com", Active: true, Load: 4},
		{URL: "http://www.bing.com", Active: true, Load: 1},
		{URL: "http://www.yahoo.com", Active: true, Load: 1},
		{URL: "http://www.cricbuzz.com", Active: true, Load: 1},
		{URL: "http://www.youtube.com", Active: true, Load: 1},
	}

	loadBalancer := &LoadBalancer{
		Servers:         servers,
		loadBalanceAlgo: "least-connections",
	}

	go loadBalancer.HealthCheck(10 * time.Second) // Health check every 10 seconds

	//rateLimiter := NewRateLimiter(100) // Limit of 100 requests per IP per minute

	http.ListenAndServe(":8080", loadBalancer)
}

type Server struct {
	URL    string
	Active bool // indicates if the server is up
	Load   int  // used for Least Connections
}

type LoadBalancer struct {
	Servers         []*Server
	currentIndex    int        // for round-robin
	mu              sync.Mutex // to handle concurrent access
	loadBalanceAlgo string     // "round-robin", "least-connections", "ip-hash"
}

func (lb *LoadBalancer) RoundRobin() *Server {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	server := lb.Servers[lb.currentIndex]
	lb.currentIndex = (lb.currentIndex + 1) % len(lb.Servers)
	fmt.Print(lb.currentIndex)
	return server
}
func (lb *LoadBalancer) LeastConnections() *Server {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	var selectedServer *Server
	minLoad := int(^uint(0) >> 1) // max int value
	for _, server := range lb.Servers {
		if server.Load < minLoad && server.Active {
			selectedServer = server
			minLoad = server.Load
		}
	}
	return selectedServer
}
func (lb *LoadBalancer) IPHash(ip string) *Server {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	hash := int(hashString(ip)) % len(lb.Servers)
	return lb.Servers[hash]
}

func hashString(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
func (lb *LoadBalancer) GetNextServer(ip string) *Server {
	switch lb.loadBalanceAlgo {
	case "round-robin":
		return lb.RoundRobin()
	case "least-connections":
		return lb.LeastConnections()
	case "ip-hash":
		return lb.IPHash(ip)
	default:
		return lb.RoundRobin()
	}
}
func (lb *LoadBalancer) HealthCheck(interval time.Duration) {
	fmt.Print("HealthCheck")
	ticker := time.NewTicker(interval)
	for {
		<-ticker.C
		for _, server := range lb.Servers {
			resp, err := http.Get(server.URL + "/health")
			if err != nil || resp.StatusCode != http.StatusOK {
				server.Active = false
			} else {
				server.Active = true
			}
		}
	}
}

type RateLimiter struct {
	requests map[string]int
	mu       sync.Mutex
	limit    int
}

func NewRateLimiter(limit int) *RateLimiter {
	return &RateLimiter{
		requests: make(map[string]int),
		limit:    limit,
	}
}
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if rl.requests[ip] >= rl.limit {
		return false
	}
	rl.requests[ip]++
	return true
}

func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.requests = make(map[string]int)
}

func (lb *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ip := r.RemoteAddr // or extract client IP

	// if !rateLimiter.Allow(ip) {
	// 	http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
	// 	return
	// }

	server := lb.GetNextServer(ip)
	if server == nil || !server.Active {
		http.Error(w, "No available servers", http.StatusServiceUnavailable)
		return
	}

	resp, err := http.Get(server.URL + r.URL.Path)
	if err != nil || resp.StatusCode != http.StatusOK {
		http.Error(w, "Failed to reach server", http.StatusBadGateway)
		return
	}

	defer resp.Body.Close()
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
