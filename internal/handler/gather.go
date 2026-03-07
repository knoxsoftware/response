package handler

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/mattventura/respond/internal/store"
	"github.com/mattventura/respond/internal/twiml"
)

type GatherHandler struct {
	Responders *store.ResponderStore
	Sessions   *store.SessionStore
	BaseURL    string
}

func (h *GatherHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	callSid := r.FormValue("CallSid")
	digits := r.FormValue("Digits")
	callStatus := r.FormValue("CallStatus")
	ctx := r.Context()

	log.Printf("[gather] CallSid=%s CallStatus=%s Digits=%q", callSid, callStatus, digits)

	w.Header().Set("Content-Type", "application/xml")

	sess, err := h.Sessions.Get(ctx, callSid)
	if err != nil {
		log.Printf("[gather] get session %s: %v", callSid, err)
		w.Write([]byte(twiml.Say("Session not found. Goodbye.")))
		return
	}

	log.Printf("[gather] session step=%s pending=%v", sess.State.Step, sess.State.Pending)

	switch sess.State.Step {
	case "responder_set_pin":
		h.handleResponderSetPIN(w, r, sess, digits)
	case "responder_confirm_pin":
		h.handleResponderConfirmPIN(w, r, sess, digits)
	case "responder_pin":
		h.handleResponderPIN(w, r, sess, digits)
	case "responder_menu":
		h.handleResponderMenu(w, r, sess, digits)
	case "responder_new_pin":
		h.handleResponderNewPIN(w, r, sess, digits)
	case "responder_confirm_new_pin":
		h.handleResponderConfirmNewPIN(w, r, sess, digits)
	case "admin_responder_pre_pin":
		h.handleAdminResponderPrePIN(w, r, sess, digits)
	case "admin_pin":
		h.handleAdminPIN(w, r, sess, digits)
	case "admin_menu":
		h.handleAdminMenu(w, r, sess, digits)
	case "admin_add_number":
		h.handleAdminAddNumber(w, r, sess, digits)
	case "admin_add_name":
		h.handleAdminAddName(w, r, sess, digits)
	case "admin_remove_number":
		h.handleAdminRemoveNumber(w, r, sess, digits)
	case "admin_change_number":
		h.handleAdminChangeAvailNumber(w, r, sess, digits)
	case "admin_new_pin":
		h.handleAdminNewPIN(w, r, sess, digits)
	case "admin_confirm_pin":
		h.handleAdminConfirmPIN(w, r, sess, digits)
	case "admin_promote_number":
		h.handleAdminPromoteNumber(w, r, sess, digits)
	case "admin_demote_number":
		h.handleAdminDemoteNumber(w, r, sess, digits)
	default:
		w.Write([]byte(twiml.Say("Unknown state. Goodbye.")))
	}
}

func (h *GatherHandler) handleResponderSetPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	if sess.State.Pending == nil {
		sess.State.Pending = map[string]string{}
	}
	sess.State.Pending["new_pin"] = digits
	sess.State.Step = "responder_confirm_pin"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather("Enter your PIN again to confirm, followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 0)))
}

func (h *GatherHandler) handleResponderConfirmPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	newPIN := sess.State.Pending["new_pin"]
	if digits != newPIN {
		sess.State.Step = "responder_set_pin"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("PINs did not match. Please enter a PIN to secure your account, followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 0)))
		return
	}
	if err := h.Responders.SetPIN(ctx, sess.Caller, newPIN); err != nil {
		log.Printf("set pin: %v", err)
		w.Write([]byte(twiml.Say("Error setting PIN. Goodbye.")))
		return
	}
	if err := h.Responders.SetValidated(ctx, sess.Caller); err != nil {
		log.Printf("set validated: %v", err)
	}
	resp, err := h.Responders.FindByPhone(ctx, sess.Caller)
	if err != nil {
		log.Printf("find responder: %v", err)
		w.Write([]byte(twiml.Say("Error loading account. Goodbye.")))
		return
	}
	sess.State.Step = "responder_menu"
	sess.State.Pending = map[string]string{}
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather(responderMenuPrompt(resp.Available), h.BaseURL+"/twilio/voice/gather", 1)))
}

func (h *GatherHandler) handleResponderPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	resp, err := h.Responders.FindByPhone(ctx, sess.Caller)
	if err != nil || !resp.VerifyPIN(digits) {
		w.Write([]byte(twiml.Say("Incorrect PIN. Goodbye.")))
		return
	}
	sess.State.Step = "responder_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather(responderMenuPrompt(resp.Available), h.BaseURL+"/twilio/voice/gather", 1)))
}

func responderMenuPrompt(available bool) string {
	status := "unavailable"
	if available {
		status = "available"
	}
	return "You are currently " + status + ". Press 1 to toggle your availability. Press 2 to change your PIN."
}

func (h *GatherHandler) handleResponderMenu(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	switch digits {
	case "1":
		newState, err := h.Responders.ToggleAvailable(ctx, sess.Caller)
		if err != nil {
			log.Printf("toggle: %v", err)
			w.Write([]byte(twiml.Say("Error updating availability. Goodbye.")))
			return
		}
		status := "unavailable"
		if newState {
			status = "available"
		}
		w.Write([]byte(twiml.Say("Your availability is now set to " + status + ". Goodbye.")))
	case "2":
		sess.State.Step = "responder_new_pin"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Please enter your new PIN followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 0)))
	default:
		resp, err := h.Responders.FindByPhone(ctx, sess.Caller)
		if err != nil {
			w.Write([]byte(twiml.Say("Error loading account. Goodbye.")))
			return
		}
		sess.State.Step = "responder_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Invalid selection. "+responderMenuPrompt(resp.Available), h.BaseURL+"/twilio/voice/gather", 1)))
	}
}

func (h *GatherHandler) handleResponderNewPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	if sess.State.Pending == nil {
		sess.State.Pending = map[string]string{}
	}
	sess.State.Pending["new_pin"] = digits
	sess.State.Step = "responder_confirm_new_pin"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather("Enter your new PIN again to confirm, followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 0)))
}

func (h *GatherHandler) handleResponderConfirmNewPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	newPIN := sess.State.Pending["new_pin"]
	if digits != newPIN {
		sess.State.Step = "responder_menu"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		resp, err := h.Responders.FindByPhone(ctx, sess.Caller)
		if err != nil {
			w.Write([]byte(twiml.Say("Error loading account. Goodbye.")))
			return
		}
		w.Write([]byte(twiml.Gather("PINs did not match. "+responderMenuPrompt(resp.Available), h.BaseURL+"/twilio/voice/gather", 1)))
		return
	}
	if err := h.Responders.UpdatePIN(ctx, sess.Caller, newPIN); err != nil {
		log.Printf("update pin: %v", err)
		w.Write([]byte(twiml.Say("Error updating PIN. Goodbye.")))
		return
	}
	w.Write([]byte(twiml.Say("Your PIN has been updated. Goodbye.")))
}

func (h *GatherHandler) handleAdminResponderPrePIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	if digits == "1" {
		newState, err := h.Responders.ToggleAvailable(ctx, sess.Caller)
		if err != nil {
			log.Printf("pre-pin toggle: %v", err)
		} else {
			status := "unavailable"
			if newState {
				status = "available"
			}
			log.Printf("[pre_pin] toggled %s to %s", sess.Caller, status)
		}
	}
	sess.State.Step = "admin_pin"
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	w.Write([]byte(twiml.Gather("Please enter your PIN followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 6)))
}

func (h *GatherHandler) handleAdminPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	responder, err := h.Responders.FindByPhone(ctx, sess.Caller)
	if err != nil || !responder.VerifyPIN(digits) {
		w.Write([]byte(twiml.Say("Incorrect PIN. Goodbye.")))
		return
	}
	sess.State.Step = "admin_menu"
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("upsert session: %v", err)
	}
	w.Write([]byte(twiml.Gather(adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

func adminMenuPrompt() string {
	return "Admin menu. Press 1 to add a responder. Press 2 to remove a responder. Press 3 to list all responders. Press 4 to change a responder's availability. Press 5 for responder status summary. Press 6 to change your PIN. Press 7 to promote a responder to admin. Press 8 to demote an admin to responder."
}

func (h *GatherHandler) handleAdminMenu(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	switch digits {
	case "1":
		sess.State.Step = "admin_add_number"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Enter the 10-digit phone number of the new responder, followed by pound.", h.BaseURL+"/twilio/voice/gather", 0)))
	case "2":
		sess.State.Step = "admin_remove_number"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Enter the 10-digit phone number of the responder to remove, followed by pound.", h.BaseURL+"/twilio/voice/gather", 0)))
	case "3":
		responders, err := h.Responders.ListAll(ctx)
		if err != nil {
			w.Write([]byte(twiml.Say("Error retrieving list. Goodbye.")))
			return
		}
		if len(responders) == 0 {
			w.Write([]byte(twiml.Gather("No responders configured. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
			return
		}
		var parts []string
		for _, resp := range responders {
			var status string
			switch {
			case !resp.IsValidated:
				status = "unvalidated"
			case resp.Available:
				status = "available"
			default:
				status = "unavailable"
			}
			parts = append(parts, fmt.Sprintf("%s is %s", sayPhone(resp.PhoneNumber), status))
		}
		msg := strings.Join(parts, ". ") + ". " + adminMenuPrompt()
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather(msg, h.BaseURL+"/twilio/voice/gather", 1)))
	case "4":
		sess.State.Step = "admin_change_number"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Enter the 10-digit phone number of the responder to update, followed by pound.", h.BaseURL+"/twilio/voice/gather", 0)))
	case "6":
		sess.State.Step = "admin_new_pin"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Please enter your new PIN followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 6)))
	case "7":
		sess.State.Step = "admin_promote_number"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Enter the 10-digit phone number of the responder to promote to admin, followed by pound.", h.BaseURL+"/twilio/voice/gather", 0)))
	case "8":
		sess.State.Step = "admin_demote_number"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Enter the 10-digit phone number of the admin to demote to responder, followed by pound.", h.BaseURL+"/twilio/voice/gather", 0)))
	case "5":
		active, inactive, err := h.Responders.CountByAvailability(ctx)
		if err != nil {
			log.Printf("count availability: %v", err)
			w.Write([]byte(twiml.Gather("Error retrieving summary. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
			return
		}
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather(fmt.Sprintf("%d active, %d inactive. %s", active, inactive, adminMenuPrompt()), h.BaseURL+"/twilio/voice/gather", 1)))
	default:
		w.Write([]byte(twiml.Gather("Invalid selection. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
	}
}

func (h *GatherHandler) handleAdminAddNumber(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	if !isValidUSPhone(digits) {
		sess.State.Step = "admin_add_number"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Invalid phone number. Please enter a 10-digit US phone number followed by pound.", h.BaseURL+"/twilio/voice/gather", 0)))
		return
	}
	phone := normalizePhone(digits)
	log.Printf("[add_number] digits=%q normalized=%s", digits, phone)
	sess.State.Step = "admin_add_name"
	if sess.State.Pending == nil {
		sess.State.Pending = map[string]string{}
	}
	sess.State.Pending["phone"] = phone
	if err := h.Sessions.Upsert(ctx, sess); err != nil {
		log.Printf("[add_number] upsert session: %v", err)
	}
	resp := twiml.Gather("Got it. Press any key to confirm adding "+sayPhone(phone)+", or hang up to cancel.", h.BaseURL+"/twilio/voice/gather", 1)
	log.Printf("[add_number] responding with TwiML: %s", resp)
	w.Write([]byte(resp))
}

func (h *GatherHandler) handleAdminAddName(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := sess.State.Pending["phone"]
	if err := h.Responders.Create(ctx, phone); err != nil {
		log.Printf("create responder: %v", err)
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Error adding responder. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
		return
	}
	sess.State.Step = "admin_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather("Responder "+sayPhone(phone)+" added. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

func (h *GatherHandler) handleAdminRemoveNumber(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := normalizePhone(digits)
	if err := h.Responders.Delete(ctx, phone); err != nil {
		log.Printf("delete responder: %v", err)
	}
	sess.State.Step = "admin_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather("Responder "+sayPhone(phone)+" removed. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

func (h *GatherHandler) handleAdminChangeAvailNumber(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := normalizePhone(digits)
	newState, err := h.Responders.ToggleAvailable(ctx, phone)
	if err != nil {
		log.Printf("toggle avail: %v", err)
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Error updating responder. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
		return
	}
	status := "unavailable"
	if newState {
		status = "available"
	}
	sess.State.Step = "admin_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather(sayPhone(phone)+" is now "+status+". "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

func (h *GatherHandler) handleAdminNewPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	if sess.State.Pending == nil {
		sess.State.Pending = map[string]string{}
	}
	sess.State.Pending["new_pin"] = digits
	sess.State.Step = "admin_confirm_pin"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather("Please enter your new PIN again to confirm, followed by the pound sign.", h.BaseURL+"/twilio/voice/gather", 6)))
}

func (h *GatherHandler) handleAdminConfirmPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	newPIN := sess.State.Pending["new_pin"]
	if digits != newPIN {
		sess.State.Step = "admin_menu"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("PINs did not match. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
		return
	}
	if err := h.Responders.UpdatePIN(ctx, sess.Caller, newPIN); err != nil {
		log.Printf("update pin: %v", err)
		sess.State.Step = "admin_menu"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Error updating PIN. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
		return
	}
	sess.State.Step = "admin_menu"
	sess.State.Pending = map[string]string{}
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather("Your PIN has been updated. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

func (h *GatherHandler) handleAdminPromoteNumber(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := normalizePhone(digits)
	if err := h.Responders.SetAdmin(ctx, phone, true); err != nil {
		log.Printf("promote admin: %v", err)
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Error promoting responder. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
		return
	}
	sess.State.Step = "admin_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather(sayPhone(phone)+" is now an admin. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

func (h *GatherHandler) handleAdminDemoteNumber(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := normalizePhone(digits)
	if err := h.Responders.SetAdmin(ctx, phone, false); err != nil {
		log.Printf("demote admin: %v", err)
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(twiml.Gather("Error demoting admin. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
		return
	}
	sess.State.Step = "admin_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(twiml.Gather(sayPhone(phone)+" is now a responder. "+adminMenuPrompt(), h.BaseURL+"/twilio/voice/gather", 1)))
}

// isValidUSPhone returns true if digits is exactly 10 numeric characters.
func isValidUSPhone(digits string) bool {
	digits = strings.TrimSpace(digits)
	if len(digits) != 10 {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// normalizePhone converts a 10-digit DTMF string to E.164 (+1XXXXXXXXXX).
func normalizePhone(digits string) string {
	digits = strings.TrimSpace(digits)
	if strings.HasPrefix(digits, "+") {
		return digits
	}
	if len(digits) == 10 {
		return "+1" + digits
	}
	return "+" + digits
}

// sayPhone formats an E.164 number for TTS so Twilio reads each digit individually,
// e.g. "+13038802466" -> "1. 3. 0. 3. 8. 8. 0. 2. 4. 6. 6."
func sayPhone(e164 string) string {
	s := strings.TrimPrefix(e164, "+")
	parts := make([]string, len(s))
	for i, c := range s {
		parts[i] = string(c)
	}
	return strings.Join(parts, ". ")
}
