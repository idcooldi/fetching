package main

import (
	"context"
	"encoding/json"
	"fetching/limiter"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type request struct {
	Urls []string `json:"urls"`
}

// В ТЗ не сказано, что должно возвращаться в json
type response struct {
	Url map[string]string
}

func (u *response) fetch(ctx context.Context, url string) {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Println(err)
		return
	}
	res, err := http.DefaultClient.Do(req)
	if err == nil {
		res.Body.Close()
		u.Url[url] = res.Status
	}
}

func handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	var (
		req request
		u   = response{Url: make(map[string]string)}
	)

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil || len(req.Urls) > 20 {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(len(req.Urls))
	for _, url := range req.Urls {
		go func(url string) {
			u.fetch(ctx, url)
			wg.Done()
		}(url)
	}
	log.Println("processing request")
	wg.Wait()
	log.Println("Response")
	select {
	case <-ctx.Done():
		log.Println("error ctx", ctx.Err())
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	default:
		err = json.NewEncoder(w).Encode(u)
		if err != nil {
			log.Println("error encode response", err)
		}
	}
}
func main() {
	ctx, cnl := context.WithCancel(context.Background())
	defer cnl()
	mux := http.NewServeMux()
	mux.HandleFunc("/", handler)

	srv := &http.Server{
		Handler: mux,
		BaseContext: func(listener net.Listener) context.Context {
			return ctx
		},
	}
	l, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatalf("Listen: %v", err)
	}
	defer l.Close()

	l = limiter.LimitListener(l, 100)

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.Serve(l); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()
	log.Print("Server Started")

	<-done
	log.Print("Server Stopped")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() {
		cancel()
	}()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server Shutdown Failed:%+v", err)
	}
	log.Print("Server Exited Properly")
}
