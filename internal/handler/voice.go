package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/mattventura/respond/internal/fsxml"
	"github.com/mattventura/respond/internal/store"
)

type VoiceHandler struct {
	Responders responderStore
	Sessions   sessionStore
	BaseURL    string
}

func (h *VoiceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	from := r.FormValue("Caller-ID-Number")
	callSid := r.FormValue("Unique-ID")
	ctx := r.Context()

	w.Header().Set("Content-Type", "application/xml")

	responder, err := h.Responders.FindByPhone(ctx, from)

	switch {
	case err == nil:
		h.startResponderFlow(w, r, ctx, responder, callSid)
	default:
		h.dispatchFlow(w, ctx)
	}
}

func (h *VoiceHandler) dispatchFlow(w http.ResponseWriter, ctx context.Context) {
	available, err := h.Responders.ListAvailable(ctx)
	if err != nil {
		log.Printf("list available: %v", err)
		w.Write([]byte(fsxml.Say("System error. Please try again.")))
		return
	}
	numbers := make([]string, len(available))
	for i, r := range available {
		numbers[i] = r.PhoneNumber
	}
	w.Write([]byte(fsxml.Dial(numbers)))
}

func (h *VoiceHandler) startResponderFlow(w http.ResponseWriter, r *http.Request, ctx context.Context, resp *store.Responder, callSid string) {
	if !resp.IsValidated {
		sess := &store.Session{
			CallSid: callSid,
			Caller:  resp.PhoneNumber,
			State:   store.SessionState{Step: "responder_set_pin", Pending: map[string]string{}},
		}
		if err := h.Sessions.Upsert(ctx, sess); err != nil {
			log.Printf("upsert session: %v", err)
		}
		w.Write([]byte(fsxml.Gather("Welcome. Please enter a PIN to secure your account, followed by the pound sign.", "pin_input", h.BaseURL+"/fs/gather", 0)))
		return
	}

	sess := &store.Session{
		CallSid: callSid,
		Caller:  resp.PhoneNumber,
		State:   store.SessionState{Step: "responder_pin"},
	}
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	w.Write([]byte(fsxml.Gather("Please enter your PIN followed by the pound sign.", "pin_input", h.BaseURL+"/fs/gather", 0)))
}
