package handler

import (
	"context"
	"log"
	"net/http"

	"github.com/mattventura/respond/internal/store"
	"github.com/mattventura/respond/internal/twiml"
)

type VoiceHandler struct {
	Responders *store.ResponderStore
	Sessions   *store.SessionStore
	BaseURL    string
}

func (h *VoiceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	from := r.FormValue("From")
	callSid := r.FormValue("CallSid")
	ctx := r.Context()

	w.Header().Set("Content-Type", "application/xml")

	responder, err := h.Responders.FindByPhone(ctx, from)

	switch {
	case err == nil && responder.IsAdmin:
		h.startAdminFlow(w, r, ctx, responder, callSid)
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
		w.Write([]byte(twiml.Say("System error. Please try again.")))
		return
	}
	numbers := make([]string, len(available))
	for i, r := range available {
		numbers[i] = r.PhoneNumber
	}
	w.Write([]byte(twiml.Dial(numbers)))
}

func (h *VoiceHandler) startResponderFlow(w http.ResponseWriter, r *http.Request, ctx context.Context, resp *store.Responder, callSid string) {
	if !resp.IsValidated {
		// First call: prompt to set a PIN
		sess := &store.Session{
			CallSid: callSid,
			Caller:  resp.PhoneNumber,
			State:   store.SessionState{Step: "responder_set_pin", Pending: map[string]string{}},
		}
		if err := h.Sessions.Upsert(ctx, sess); err != nil {
			log.Printf("upsert session: %v", err)
		}
		w.Write([]byte(twiml.Gather("Welcome. Please enter a PIN to secure your account, followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 0)))
		return
	}

	// Subsequent calls: require PIN
	sess := &store.Session{
		CallSid: callSid,
		Caller:  resp.PhoneNumber,
		State:   store.SessionState{Step: "responder_pin"},
	}
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	w.Write([]byte(twiml.Gather("Please enter your PIN followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 0)))
}

func (h *VoiceHandler) startAdminFlow(w http.ResponseWriter, r *http.Request, ctx context.Context, responder *store.Responder, callSid string) {
	status := "unavailable"
	if responder.Available {
		status = "available"
	}
	sess := &store.Session{
		CallSid: callSid,
		Caller:  responder.PhoneNumber,
		State:   store.SessionState{Step: "admin_responder_pre_pin", Pending: map[string]string{}},
	}
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	msg := "You are currently " + status + " as a responder. Press 1 to toggle your availability, or press 2 to continue to the admin menu."
	w.Write([]byte(twiml.Gather(msg, h.BaseURL+"/twilio/voice/gather", 1)))
}
