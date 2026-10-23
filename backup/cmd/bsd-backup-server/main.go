// Command bsd-backup-server runs the Central Management Server.
//
//	BSD_ADMIN_KEY=... bsd-backup-server -addr :8443 -data /var/lib/bsd-backup/store.json \
//	    -tls-cert cert.pem -tls-key key.pem
//
// Without -tls-cert it serves plain HTTP and must sit behind a TLS-terminating
// reverse proxy (Caddy, nginx, a cloud load balancer).
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bsd-backup/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	data := flag.String("data", "data/store.json", "path of the JSON data file")
	certFile := flag.String("tls-cert", "", "TLS certificate (PEM); empty = plain HTTP behind a proxy")
	keyFile := flag.String("tls-key", "", "TLS private key (PEM)")
	poll := flag.Int("poll-seconds", 300, "heartbeat interval sent to agents")
	ttl := flag.Duration("token-ttl", 15*time.Minute, "device session token lifetime")
	restoreInactive := flag.Bool("allow-restore-when-inactive", true, "let inactive accounts still restore from Google Drive")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	adminKey := os.Getenv("BSD_ADMIN_KEY")
	if adminKey == "" {
		log.Error("BSD_ADMIN_KEY is not set")
		os.Exit(2)
	}
	store, err := server.OpenStore(*data)
	if err != nil {
		log.Error("open store", "err", err)
		os.Exit(1)
	}
	srv, err := server.New(server.Config{
		AdminKey: adminKey, TokenTTL: *ttl, PollSeconds: *poll,
		AllowRestoreWhenInactive: *restoreInactive,
	}, store, log)
	if err != nil {
		log.Error("init", "err", err)
		os.Exit(1)
	}

	hs := &http.Server{
		Addr: *addr, Handler: logRequests(log, srv.Handler()),
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		hs.Shutdown(shutdown)
	}()

	log.Info("listening", "addr", *addr, "tls", *certFile != "")
	if *certFile != "" {
		err = hs.ListenAndServeTLS(*certFile, *keyFile)
	} else {
		err = hs.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"ms", time.Since(start).Milliseconds(), "remote", r.RemoteAddr)
	})
}
