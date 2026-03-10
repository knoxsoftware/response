package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/mattventura/respond/internal/sms"
	"github.com/mattventura/respond/internal/store"
)

type SMSSender interface {
	SendSMS(ctx context.Context, to, message string) error
}

type SMSHandler struct {
	Engine     *sms.Engine
	Sender     SMSSender
	Responders *store.ResponderStore
}

func (h *SMSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	from := r.FormValue("from")
	body := r.FormValue("message")
	ctx := r.Context()

	log.Printf("[sms] from=%s body=%q", from, body)

	resp, err := h.Engine.Handle(ctx, from, body)
	if err != nil {
		log.Printf("[sms] engine error: %v", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := h.Sender.SendSMS(ctx, from, resp.Message); err != nil {
		log.Printf("[sms] send reply: %v", err)
	}

	if resp.Action == "notify_responders" {
		h.notifyResponders(ctx, from)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *SMSHandler) notifyResponders(ctx context.Context, customerPhone string) {
	available, err := h.Responders.ListAvailable(ctx)
	if err != nil {
		log.Printf("[sms] list available for notify: %v", err)
		return
	}
	msg := "On-call request from " + customerPhone + ". Please call them back."
	for _, r := range available {
		if err := h.Sender.SendSMS(ctx, r.PhoneNumber, msg); err != nil {
			log.Printf("[sms] notify responder %s: %v", r.PhoneNumber, err)
		}
	}
}
