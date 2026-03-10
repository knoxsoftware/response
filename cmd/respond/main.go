package main

import (
	"context"
	"log"
	"net/http"

	"github.com/mattventura/respond/internal/config"
	"github.com/mattventura/respond/internal/db"
	"github.com/mattventura/respond/internal/handler"
	"github.com/mattventura/respond/internal/middleware"
	"github.com/mattventura/respond/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	responders := store.NewResponderStore(pool)
	sessions := store.NewSessionStore(pool)

	if len(cfg.BootstrapAdmins) > 0 {
		count, err := responders.CountAdmins(ctx)
		if err != nil {
			log.Fatalf("bootstrap: count admins: %v", err)
		}
		if count == 0 {
			for _, a := range cfg.BootstrapAdmins {
				if err := responders.CreateAdmin(ctx, a.Phone, a.PIN); err != nil {
					log.Fatalf("bootstrap: create admin %s: %v", a.Phone, err)
				}
				log.Printf("bootstrap: created admin %s", a.Phone)
			}
		}
	}

	voiceHandler := &handler.VoiceHandler{
		Responders: responders,
		Sessions:   sessions,
		BaseURL:    cfg.BaseURL,
	}
	gatherHandler := &handler.GatherHandler{
		Responders: responders,
		Sessions:   sessions,
		BaseURL:    cfg.BaseURL,
	}
	statusHandler := &handler.StatusHandler{Sessions: sessions}

	fsMW := func(h http.Handler) http.Handler {
		return middleware.FSAuth(cfg.FSSharedSecret, h)
	}

	mux := http.NewServeMux()
	mux.Handle("/fs/voice", fsMW(voiceHandler))
	mux.Handle("/fs/gather", fsMW(gatherHandler))
	mux.Handle("/fs/status", fsMW(statusHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
