package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/mattventura/respond/internal/config"
	"github.com/mattventura/respond/internal/db"
	"github.com/mattventura/respond/internal/handler"
	"github.com/mattventura/respond/internal/middleware"
	"github.com/mattventura/respond/internal/sms"
	"github.com/mattventura/respond/internal/store"
	"github.com/mattventura/respond/internal/voipms"
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

	// SMS tree
	treeFile, err := os.Open(cfg.SMSTreePath)
	if err != nil {
		log.Fatalf("open sms tree: %v", err)
	}
	tree, err := sms.LoadTree(treeFile)
	treeFile.Close()
	if err != nil {
		log.Fatalf("load sms tree: %v", err)
	}

	smsStore := store.NewSMSSessionStore(pool)
	voipmsClient := voipms.NewClient(cfg.VoIPMSUsername, cfg.VoIPMSPassword, cfg.VoIPMSDID, "")
	smsEngine := sms.NewEngine(tree, smsStore)
	smsHandler := &handler.SMSHandler{
		Engine:     smsEngine,
		Sender:     voipmsClient,
		Responders: responders,
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
	mux.Handle("/sms/inbound", smsHandler)
	mux.Handle("/fs/voice", fsMW(voiceHandler))
	mux.Handle("/fs/gather", fsMW(gatherHandler))
	mux.Handle("/fs/status", fsMW(statusHandler))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Start background SMS session expiry
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		timeout := time.Duration(tree.TimeoutMinutes) * time.Minute
		for range ticker.C {
			n, err := smsStore.DeleteExpired(context.Background(), timeout)
			if err != nil {
				log.Printf("sms session expiry: %v", err)
			} else if n > 0 {
				log.Printf("sms session expiry: deleted %d expired sessions", n)
			}
		}
	}()

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("server: %v", err)
	}
}
