// ingress-switch queues new requests during a wallet snapshot and preserves
// existing HTTP streams and WebSocket connections through a domain cutover.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type listener struct {
	Address string `json:"address"`
	Route   string `json:"route"`
	Service string `json:"service"`
}

type config struct {
	Listeners []listener `json:"listeners"`
	NewAPI    string     `json:"newapi"`
	Sub2API   string     `json:"sub2api"`
	Images    string     `json:"images"`
	Proof     string     `json:"proof"`
	Control   string     `json:"control"`
	State     string     `json:"state"`
}

type gateway struct {
	mu      sync.Mutex
	mode    string
	barrier chan struct{}
	lease   time.Time
	active  int
	waiting int
	config  config
	proxies map[string]*httputil.ReverseProxy
}

func (g *gateway) service(host string) string {
	isNew := host == "newapi.killai.cc" || host == "newapilax.killai.cc"
	if g.mode == "final" {
		isNew = !isNew
	}
	if isNew {
		return "newapi"
	}
	return "sub2api"
}

func (g *gateway) admit(ctx context.Context, host, fixed string) (string, bool) {
	for {
		g.mu.Lock()
		if g.barrier == nil {
			service := fixed
			if service == "" {
				service = g.service(host)
			}
			g.active++
			g.mu.Unlock()
			return service, true
		}
		barrier := g.barrier
		g.waiting++
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			g.mu.Lock()
			g.waiting--
			g.mu.Unlock()
			return "", false
		case <-barrier:
			g.mu.Lock()
			g.waiting--
			g.mu.Unlock()
		}
	}
}

func (g *gateway) handler(entry listener) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if entry.Service == "" && host != "api.killai.cc" && host != "apilax.killai.cc" && host != "newapi.killai.cc" && host != "newapilax.killai.cc" {
			http.Error(w, "unknown ingress host", http.StatusBadRequest)
			return
		}
		route := entry.Route
		if route == "peer" {
			route = r.Header.Get("X-Sub2API-Billing-Route")
		}
		if route != "ordinary" && route != "optimized" {
			http.Error(w, "unknown ingress route", http.StatusForbidden)
			return
		}
		service := entry.Service
		health := r.Method == http.MethodGet && (r.URL.Path == "/health" || r.URL.Path == "/api/status")
		if health {
			g.mu.Lock()
			if service == "" {
				service = g.service(host)
			}
			g.mu.Unlock()
		} else {
			var ok bool
			service, ok = g.admit(r.Context(), host, service)
			if !ok {
				return
			}
			defer func() { g.mu.Lock(); g.active--; g.mu.Unlock() }()
		}
		r.Header.Set("X-NewAPI-Billing-Route", route)
		r.Header.Set("X-Sub2API-Billing-Route", route)
		if service == "newapi" {
			r.Header.Set("X-NewAPI-Billing-Proof", g.config.Proof)
		} else {
			r.Header.Del("X-NewAPI-Billing-Proof")
			path := r.URL.Path
			if path == "/v1/images/generations" || path == "/v1/images/edits" || path == "/images/generations" || path == "/images/edits" {
				service = "images"
			}
		}
		g.proxies[service].ServeHTTP(w, r)
	})
}

func (g *gateway) release() {
	if g.barrier != nil {
		close(g.barrier)
		g.barrier = nil
	}
}

func (g *gateway) control(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.Method == http.MethodPost {
		switch r.URL.Path {
		case "/pause":
			if g.barrier == nil {
				g.barrier = make(chan struct{})
			}
			g.lease = time.Now().Add(45 * time.Second)
		case "/renew":
			if g.barrier != nil {
				g.lease = time.Now().Add(45 * time.Second)
			}
		case "/resume":
			g.release()
		case "/swap", "/force-swap":
			if g.barrier == nil || (g.active != 0 && r.URL.Path != "/force-swap") {
				http.Error(w, "pause and drain requests first", http.StatusConflict)
				return
			}
			if err := os.WriteFile(g.config.State+".next", []byte("final\n"), 0600); err != nil {
				http.Error(w, "state write failed", http.StatusInternalServerError)
				return
			}
			if err := os.Rename(g.config.State+".next", g.config.State); err != nil {
				http.Error(w, "state commit failed", http.StatusInternalServerError)
				return
			}
			g.mode = "final"
			g.release()
		default:
			http.NotFound(w, r)
			return
		}
	} else if r.Method != http.MethodGet || r.URL.Path != "/state" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	data, err := common.Marshal(map[string]any{"mode": g.mode, "paused": g.barrier != nil, "active": g.active, "waiting": g.waiting})
	if err != nil {
		http.Error(w, "state encode failed", 500)
		return
	}
	_, _ = w.Write(data)
}

func newGateway(c config) (*gateway, error) {
	g := &gateway{config: c, mode: "original", proxies: map[string]*httputil.ReverseProxy{}}
	if state, err := os.ReadFile(c.State); err == nil {
		g.mode = strings.TrimSpace(string(state))
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if g.mode != "original" && g.mode != "final" {
		return nil, fmt.Errorf("invalid saved mode")
	}
	for name, address := range map[string]string{"newapi": c.NewAPI, "sub2api": c.Sub2API, "images": c.Images} {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" {
			return nil, fmt.Errorf("invalid private backend")
		}
		proxy := &httputil.ReverseProxy{
			Rewrite: func(p *httputil.ProxyRequest) {
				p.SetURL(u)
				p.Out.Host = p.In.Host
				ip := net.ParseIP(p.In.Header.Get("X-Real-IP"))
				if ip == nil {
					host, _, _ := net.SplitHostPort(p.In.RemoteAddr)
					ip = net.ParseIP(host)
				}
				p.Out.Header.Set("X-Real-IP", ip.String())
				p.Out.Header.Set("X-Forwarded-For", ip.String())
				p.Out.Header.Set("X-Forwarded-Proto", "https")
				p.Out.Header.Set("X-Forwarded-Host", p.In.Host)
			},
			FlushInterval: -1,
			ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
				http.Error(w, "gateway upstream unavailable", http.StatusBadGateway)
			},
			ErrorLog:  log.New(os.Stderr, "private ingress: ", log.LstdFlags),
			Transport: &http.Transport{Proxy: nil, MaxIdleConns: 64, MaxIdleConnsPerHost: 32, IdleConnTimeout: 90 * time.Second, ForceAttemptHTTP2: false},
		}
		g.proxies[name] = proxy
	}
	return g, nil
}

func main() {
	var c config
	if len(os.Args) != 2 {
		log.Fatal("expected private configuration path")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal("configuration unavailable")
	}
	if common.Unmarshal(data, &c) != nil || len(c.Proof) < 32 || len(c.Listeners) == 0 {
		log.Fatal("invalid configuration")
	}
	g, err := newGateway(c)
	if err != nil {
		log.Fatal("invalid gateway configuration")
	}
	if err = os.MkdirAll(filepath.Dir(c.Control), 0700); err != nil {
		log.Fatal("control directory unavailable")
	}
	if _, err = os.Stat(c.Control); err == nil {
		log.Fatal("control socket already exists")
	}
	control, err := net.Listen("unix", c.Control)
	if err != nil {
		log.Fatal("control socket unavailable")
	}
	if os.Chmod(c.Control, 0600) != nil {
		log.Fatal("control socket permissions failed")
	}
	defer os.Remove(c.Control)
	servers := []*http.Server{}
	for _, entry := range c.Listeners {
		socket, err := net.Listen("tcp", entry.Address)
		if err != nil {
			log.Fatal("traffic listener unavailable")
		}
		server := &http.Server{Handler: g.handler(entry), ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 75 * time.Second}
		servers = append(servers, server)
		go func() {
			if err := server.Serve(socket); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatal("traffic listener failed")
			}
		}()
	}
	admin := &http.Server{Handler: http.HandlerFunc(g.control), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = admin.Serve(control) }()
	go func() {
		for range time.Tick(time.Second) {
			g.mu.Lock()
			if g.barrier != nil && time.Now().After(g.lease) {
				g.release()
			}
			g.mu.Unlock()
		}
	}()
	log.Print("private ingress switch ready")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	// Shutdown waits for existing streams; operators must drain before stopping.
	g.mu.Lock()
	g.release()
	g.mu.Unlock()
	var wg sync.WaitGroup
	for _, server := range servers {
		wg.Go(func() { _ = server.Shutdown(context.Background()) })
	}
	_ = admin.Shutdown(context.Background())
	wg.Wait()
}
