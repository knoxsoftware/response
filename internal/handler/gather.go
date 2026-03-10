package handler

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/mattventura/respond/internal/fsxml"
	"github.com/mattventura/respond/internal/store"
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
	callSid := r.FormValue("Unique-ID")
	digits := r.FormValue("pin_input")
	if digits == "" {
		digits = r.FormValue("menu_input")
	}
	callStatus := r.FormValue("CallStatus")
	ctx := r.Context()

	log.Printf("[gather] CallSid=%s CallStatus=%s Digits=%q", callSid, callStatus, digits)

	w.Header().Set("Content-Type", "application/xml")

	sess, err := h.Sessions.Get(ctx, callSid)
	if err != nil {
		log.Printf("[gather] get session %s: %v", callSid, err)
		w.Write([]byte(fsxml.Say("Session not found. Goodbye.")))
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
	case "admin_menu":
		h.handleAdminMenu(w, r, sess, digits)
	case "admin_add_remove_number":
		h.handleAdminAddRemoveNumber(w, r, sess, digits)
	case "admin_add_remove_confirm":
		h.handleAdminAddRemoveConfirm(w, r, sess, digits)
	case "admin_change_number":
		h.handleAdminChangeAvailNumber(w, r, sess, digits)
	case "admin_toggle_admin_number":
		h.handleAdminToggleAdminNumber(w, r, sess, digits)
	case "admin_toggle_admin_confirm":
		h.handleAdminToggleAdminConfirm(w, r, sess, digits)
	default:
		w.Write([]byte(fsxml.Say("Unknown state. Goodbye.")))
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
	w.Write([]byte(fsxml.Gather("Enter your PIN again to confirm, followed by the pound sign.", "pin_input", h.BaseURL+"/fs/gather", 0)))
}

func (h *GatherHandler) handleResponderConfirmPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	newPIN := sess.State.Pending["new_pin"]
	if digits != newPIN {
		sess.State.Step = "responder_set_pin"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("PINs did not match. Please enter a PIN to secure your account, followed by the pound sign.", "pin_input", h.BaseURL+"/fs/gather", 0)))
		return
	}
	if err := h.Responders.SetPIN(ctx, sess.Caller, newPIN); err != nil {
		log.Printf("set pin: %v", err)
		w.Write([]byte(fsxml.Say("Error setting PIN. Goodbye.")))
		return
	}
	if err := h.Responders.SetValidated(ctx, sess.Caller); err != nil {
		log.Printf("set validated: %v", err)
	}
	resp, err := h.Responders.FindByPhone(ctx, sess.Caller)
	if err != nil {
		log.Printf("find responder: %v", err)
		w.Write([]byte(fsxml.Say("Error loading account. Goodbye.")))
		return
	}
	sess.State.Step = "responder_menu"
	sess.State.Pending = map[string]string{}
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(fsxml.Gather(responderMenuPrompt(resp.Available, resp.IsAdmin), "menu_input", h.BaseURL+"/fs/gather", 1)))
}

func (h *GatherHandler) handleResponderPIN(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	resp, err := h.Responders.FindByPhone(ctx, sess.Caller)
	if err != nil || !resp.VerifyPIN(digits) {
		w.Write([]byte(fsxml.Say("Incorrect PIN. Goodbye.")))
		return
	}
	sess.State.Step = "responder_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(fsxml.Gather(responderMenuPrompt(resp.Available, resp.IsAdmin), "menu_input", h.BaseURL+"/fs/gather", 1)))
}

func responderMenuPrompt(available bool, isAdmin bool) string {
	status := "unavailable"
	if available {
		status = "available"
	}
	msg := "You are currently " + status + ". Press 1 to toggle your availability. Press 2 to change your PIN."
	if isAdmin {
		msg += " Press 3 for the admin menu."
	}
	return msg
}

func (h *GatherHandler) handleResponderMenu(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	switch digits {
	case "1":
		newState, err := h.Responders.ToggleAvailable(ctx, sess.Caller)
		if err != nil {
			log.Printf("toggle: %v", err)
			w.Write([]byte(fsxml.Say("Error updating availability. Goodbye.")))
			return
		}
		status := "unavailable"
		if newState {
			status = "available"
		}
		w.Write([]byte(fsxml.Say("Your availability is now set to " + status + ". Goodbye.")))
	case "2":
		sess.State.Step = "responder_new_pin"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("Please enter your new PIN followed by the pound sign.", "pin_input", h.BaseURL+"/fs/gather", 0)))
	case "3":
		resp, err := h.Responders.FindByPhone(ctx, sess.Caller)
		if err != nil || !resp.IsAdmin {
			w.Write([]byte(fsxml.Say("Invalid selection. Goodbye.")))
			return
		}
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather(adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
	default:
		resp, err := h.Responders.FindByPhone(ctx, sess.Caller)
		if err != nil {
			w.Write([]byte(fsxml.Say("Error loading account. Goodbye.")))
			return
		}
		sess.State.Step = "responder_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("Invalid selection. "+responderMenuPrompt(resp.Available, resp.IsAdmin), "menu_input", h.BaseURL+"/fs/gather", 1)))
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
	w.Write([]byte(fsxml.Gather("Enter your new PIN again to confirm, followed by the pound sign.", "pin_input", h.BaseURL+"/fs/gather", 0)))
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
			w.Write([]byte(fsxml.Say("Error loading account. Goodbye.")))
			return
		}
		w.Write([]byte(fsxml.Gather("PINs did not match. "+responderMenuPrompt(resp.Available, resp.IsAdmin), "menu_input", h.BaseURL+"/fs/gather", 1)))
		return
	}
	if err := h.Responders.UpdatePIN(ctx, sess.Caller, newPIN); err != nil {
		log.Printf("update pin: %v", err)
		w.Write([]byte(fsxml.Say("Error updating PIN. Goodbye.")))
		return
	}
	w.Write([]byte(fsxml.Say("Your PIN has been updated. Goodbye.")))
}

func adminMenuPrompt() string {
	return "Admin menu. Press 1 to add or remove a responder. Press 2 to list all responders. Press 3 to change a responder's availability. Press 4 for responder status summary. Press 5 to promote or demote admin."
}

func (h *GatherHandler) handleAdminMenu(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	switch digits {
	case "1":
		sess.State.Step = "admin_add_remove_number"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("Enter the 10-digit phone number, followed by pound.", "pin_input", h.BaseURL+"/fs/gather", 0)))
	case "2":
		responders, err := h.Responders.ListAll(ctx)
		if err != nil {
			w.Write([]byte(fsxml.Say("Error retrieving list. Goodbye.")))
			return
		}
		if len(responders) == 0 {
			w.Write([]byte(fsxml.Gather("No responders configured. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
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
		w.Write([]byte(fsxml.Gather(msg, "menu_input", h.BaseURL+"/fs/gather", 1)))
	case "3":
		sess.State.Step = "admin_change_number"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("Enter the 10-digit phone number of the responder to update, followed by pound.", "pin_input", h.BaseURL+"/fs/gather", 0)))
	case "4":
		active, inactive, err := h.Responders.CountByAvailability(ctx)
		if err != nil {
			log.Printf("count availability: %v", err)
			w.Write([]byte(fsxml.Gather("Error retrieving summary. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
			return
		}
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather(fmt.Sprintf("%d active, %d inactive. %s", active, inactive, adminMenuPrompt()), "menu_input", h.BaseURL+"/fs/gather", 1)))
	case "5":
		sess.State.Step = "admin_toggle_admin_number"
		sess.State.Pending = map[string]string{}
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("Enter the 10-digit phone number, followed by pound.", "pin_input", h.BaseURL+"/fs/gather", 0)))
	default:
		w.Write([]byte(fsxml.Gather("Invalid selection. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
	}
}

func (h *GatherHandler) handleAdminAddRemoveNumber(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	if !isValidUSPhone(digits) {
		sess.State.Step = "admin_add_remove_number"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("Invalid phone number. Please enter a 10-digit US phone number followed by pound.", "pin_input", h.BaseURL+"/fs/gather", 0)))
		return
	}
	phone := normalizePhone(digits)
	if sess.State.Pending == nil {
		sess.State.Pending = map[string]string{}
	}
	sess.State.Pending["phone"] = phone

	existing, err := h.Responders.FindByPhone(ctx, phone)
	if err != nil {
		// Not found — offer to add
		sess.State.Pending["action"] = "add"
		sess.State.Step = "admin_add_remove_confirm"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather(sayPhone(phone)+"is not already a member. Press 1 to add them or hang up to cancel.", "menu_input", h.BaseURL+"/fs/gather", 1)))
	} else {
		_ = existing
		// Found — offer to remove
		sess.State.Pending["action"] = "remove"
		sess.State.Step = "admin_add_remove_confirm"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather(sayPhone(phone)+" is already a responder. Press 1 to remove them, or hang up to cancel.", "menu_input", h.BaseURL+"/fs/gather", 1)))
	}
}

func (h *GatherHandler) handleAdminAddRemoveConfirm(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	if digits != "1" {
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("Cancelled. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
		return
	}
	phone := sess.State.Pending["phone"]
	action := sess.State.Pending["action"]
	sess.State.Step = "admin_menu"
	sess.State.Pending = map[string]string{}
	h.Sessions.Upsert(ctx, sess)

	switch action {
	case "add":
		if err := h.Responders.Create(ctx, phone); err != nil {
			log.Printf("create responder: %v", err)
			w.Write([]byte(fsxml.Gather("Error adding responder. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
			return
		}
		w.Write([]byte(fsxml.Gather("Responder "+sayPhone(phone)+" added. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
	case "remove":
		if err := h.Responders.Delete(ctx, phone); err != nil {
			log.Printf("delete responder: %v", err)
			w.Write([]byte(fsxml.Gather("Error removing responder. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
			return
		}
		w.Write([]byte(fsxml.Gather("Responder "+sayPhone(phone)+" removed. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
	default:
		w.Write([]byte(fsxml.Gather("Unknown action. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
	}
}

func (h *GatherHandler) handleAdminChangeAvailNumber(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	phone := normalizePhone(digits)
	newState, err := h.Responders.ToggleAvailable(ctx, phone)
	if err != nil {
		log.Printf("toggle avail: %v", err)
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("Error updating responder. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
		return
	}
	status := "unavailable"
	if newState {
		status = "available"
	}
	sess.State.Step = "admin_menu"
	h.Sessions.Upsert(ctx, sess)
	w.Write([]byte(fsxml.Gather(sayPhone(phone)+" is now "+status+". "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
}

func (h *GatherHandler) handleAdminToggleAdminNumber(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	if !isValidUSPhone(digits) {
		sess.State.Step = "admin_toggle_admin_number"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("Invalid phone number. Please enter a 10-digit US phone number followed by pound.", "pin_input", h.BaseURL+"/fs/gather", 0)))
		return
	}
	phone := normalizePhone(digits)
	if sess.State.Pending == nil {
		sess.State.Pending = map[string]string{}
	}
	sess.State.Pending["phone"] = phone

	resp, err := h.Responders.FindByPhone(ctx, phone)
	if err != nil {
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("Responder not found. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
		return
	}

	sess.State.Step = "admin_toggle_admin_confirm"
	if resp.IsAdmin {
		sess.State.Pending["action"] = "demote"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather(sayPhone(phone)+" is currently an admin. Press 1 to demote to responder, or hang up to cancel.", "menu_input", h.BaseURL+"/fs/gather", 1)))
	} else {
		sess.State.Pending["action"] = "promote"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather(sayPhone(phone)+" is currently a responder. Press 1 to promote to admin, or hang up to cancel.", "menu_input", h.BaseURL+"/fs/gather", 1)))
	}
}

func (h *GatherHandler) handleAdminToggleAdminConfirm(w http.ResponseWriter, r *http.Request, sess *store.Session, digits string) {
	ctx := r.Context()
	if digits != "1" {
		sess.State.Step = "admin_menu"
		h.Sessions.Upsert(ctx, sess)
		w.Write([]byte(fsxml.Gather("Cancelled. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
		return
	}
	phone := sess.State.Pending["phone"]
	action := sess.State.Pending["action"]
	sess.State.Step = "admin_menu"
	sess.State.Pending = map[string]string{}
	h.Sessions.Upsert(ctx, sess)

	switch action {
	case "promote":
		if err := h.Responders.SetAdmin(ctx, phone, true); err != nil {
			log.Printf("promote admin: %v", err)
			w.Write([]byte(fsxml.Gather("Error promoting responder. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
			return
		}
		w.Write([]byte(fsxml.Gather(sayPhone(phone)+" is now an admin. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
	case "demote":
		if err := h.Responders.SetAdmin(ctx, phone, false); err != nil {
			log.Printf("demote admin: %v", err)
			w.Write([]byte(fsxml.Gather("Error demoting admin. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
			return
		}
		w.Write([]byte(fsxml.Gather(sayPhone(phone)+" is now a responder. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
	default:
		w.Write([]byte(fsxml.Gather("Unknown action. "+adminMenuPrompt(), "menu_input", h.BaseURL+"/fs/gather", 1)))
	}
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

// sayPhone formats an E.164 number for TTS so each digit is read individually,
// e.g. "+13038802466" -> "1. 3. 0. 3. 8. 8. 0. 2. 4. 6. 6."
func sayPhone(e164 string) string {
	s := strings.TrimPrefix(e164, "+")
	parts := make([]string, len(s))
	for i, c := range s {
		parts[i] = string(c)
	}
	return strings.Join(parts, ". ")
}
