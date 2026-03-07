package handler

import (
	"log"
	"net/http"

	"github.com/mattventura/respond/internal/store"
)

type StatusHandler struct {
	Sessions *store.SessionStore
}

func (h *StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	callSid := r.FormValue("CallSid")
	callStatus := r.FormValue("CallStatus")
	sequenceNumber := r.FormValue("SequenceNumber")
	log.Printf("[status] CallSid=%s CallStatus=%s SequenceNumber=%s", callSid, callStatus, sequenceNumber)

	terminal := callStatus == "completed" || callStatus == "failed" || callStatus == "busy" || callStatus == "no-answer" || callStatus == "canceled"
	if callSid != "" && terminal {
		if err := h.Sessions.Delete(r.Context(), callSid); err != nil {
			log.Printf("[status] delete session %s: %v", callSid, err)
		} else {
			log.Printf("[status] deleted session %s (status=%s)", callSid, callStatus)
		}
	} else if callSid != "" {
		log.Printf("[status] skipping session delete for non-terminal status=%s", callStatus)
	}
	w.WriteHeader(http.StatusNoContent)
}
