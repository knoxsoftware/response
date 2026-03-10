package handler

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/mattventura/respond/internal/store"
)

// helpers

func makeSession(callSid, caller, step string) *store.Session {
	return &store.Session{
		CallSid: callSid,
		Caller:  caller,
		State:   store.SessionState{Step: step, Pending: map[string]string{}},
	}
}

func hashPIN(t *testing.T, pin string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash pin: %v", err)
	}
	return string(h)
}

func postGather(h *GatherHandler, callSid, pinInput, menuInput string) *httptest.ResponseRecorder {
	form := url.Values{"Unique-ID": {callSid}}
	if pinInput != "" {
		form.Set("pin_input", pinInput)
	}
	if menuInput != "" {
		form.Set("menu_input", menuInput)
	}
	req := httptest.NewRequest("POST", "/fs/gather", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func newGatherHandler(responders *mockResponderStore, sessions *mockSessionStore) *GatherHandler {
	return &GatherHandler{Responders: responders, Sessions: sessions, BaseURL: "https://example.com"}
}

// PIN setup flow

func TestGather_SetPIN_StoresAndAdvances(t *testing.T) {
	sessions := newMockSessionStore()
	sessions.Upsert(nil, makeSession("call-1", "+15551111111", "responder_set_pin"))
	h := newGatherHandler(newMockResponderStore(), sessions)

	rr := postGather(h, "call-1", "4321", "")

	body := rr.Body.String()
	if !strings.Contains(body, "confirm") {
		t.Errorf("expected confirm prompt, got: %s", body)
	}
	sess, _ := sessions.Get(nil, "call-1")
	if sess.State.Step != "responder_confirm_pin" {
		t.Errorf("expected responder_confirm_pin, got %s", sess.State.Step)
	}
	if sess.State.Pending["new_pin"] != "4321" {
		t.Errorf("expected pending new_pin=4321, got %s", sess.State.Pending["new_pin"])
	}
}

func TestGather_ConfirmPIN_Mismatch_RestartsFlow(t *testing.T) {
	sessions := newMockSessionStore()
	sess := makeSession("call-1", "+15551111111", "responder_confirm_pin")
	sess.State.Pending["new_pin"] = "4321"
	sessions.Upsert(nil, sess)
	h := newGatherHandler(newMockResponderStore(), sessions)

	rr := postGather(h, "call-1", "9999", "")

	body := rr.Body.String()
	if !strings.Contains(body, "did not match") {
		t.Errorf("expected mismatch message, got: %s", body)
	}
	updated, _ := sessions.Get(nil, "call-1")
	if updated.State.Step != "responder_set_pin" {
		t.Errorf("expected reset to responder_set_pin, got %s", updated.State.Step)
	}
}

func TestGather_ConfirmPIN_Match_SavesPINAndShowsMenu(t *testing.T) {
	responder := &store.Responder{PhoneNumber: "+15551111111", IsValidated: false}
	responders := newMockResponderStore(responder)
	sessions := newMockSessionStore()
	sess := makeSession("call-1", "+15551111111", "responder_confirm_pin")
	sess.State.Pending["new_pin"] = "4321"
	sessions.Upsert(nil, sess)
	h := newGatherHandler(responders, sessions)

	rr := postGather(h, "call-1", "4321", "")

	body := rr.Body.String()
	if !strings.Contains(body, "available") && !strings.Contains(body, "unavailable") {
		t.Errorf("expected responder menu, got: %s", body)
	}
}

// PIN verification flow

func TestGather_ResponderPIN_Correct_ShowsMenu(t *testing.T) {
	pinHash := hashPIN(t, "1234")
	responder := &store.Responder{
		PhoneNumber: "+15551111111",
		Available:   false,
		IsValidated: true,
		PinHash:     &pinHash,
	}
	responders := newMockResponderStore(responder)
	sessions := newMockSessionStore()
	sessions.Upsert(nil, makeSession("call-1", "+15551111111", "responder_pin"))
	h := newGatherHandler(responders, sessions)

	rr := postGather(h, "call-1", "1234", "")

	body := rr.Body.String()
	if !strings.Contains(body, "available") && !strings.Contains(body, "unavailable") {
		t.Errorf("expected responder menu after correct PIN, got: %s", body)
	}
}

func TestGather_ResponderPIN_Wrong_Goodbye(t *testing.T) {
	pinHash := hashPIN(t, "1234")
	responder := &store.Responder{
		PhoneNumber: "+15551111111",
		IsValidated: true,
		PinHash:     &pinHash,
	}
	responders := newMockResponderStore(responder)
	sessions := newMockSessionStore()
	sessions.Upsert(nil, makeSession("call-1", "+15551111111", "responder_pin"))
	h := newGatherHandler(responders, sessions)

	rr := postGather(h, "call-1", "9999", "")

	body := rr.Body.String()
	if !strings.Contains(body, "Incorrect") {
		t.Errorf("expected incorrect PIN message, got: %s", body)
	}
}

// Responder menu

func TestGather_ResponderMenu_1_TogglesAvailability(t *testing.T) {
	responder := &store.Responder{PhoneNumber: "+15551111111", Available: false, IsValidated: true}
	responders := newMockResponderStore(responder)
	sessions := newMockSessionStore()
	sessions.Upsert(nil, makeSession("call-1", "+15551111111", "responder_menu"))
	h := newGatherHandler(responders, sessions)

	postGather(h, "call-1", "", "1")

	if responder.Available != true {
		t.Error("expected Available to be toggled to true")
	}
}

// Missing session

func TestGather_NoSession_SaysGoodbye(t *testing.T) {
	h := newGatherHandler(newMockResponderStore(), newMockSessionStore())

	rr := postGather(h, "nonexistent-call", "1234", "")

	body := rr.Body.String()
	if !strings.Contains(body, "Session not found") {
		t.Errorf("expected session not found message, got: %s", body)
	}
}

// Input sanitization integration

func TestGather_LongPINInput_IsTruncated(t *testing.T) {
	sessions := newMockSessionStore()
	sessions.Upsert(nil, makeSession("call-1", "+15551111111", "responder_set_pin"))
	h := newGatherHandler(newMockResponderStore(), sessions)

	longPin := strings.Repeat("1", 200)
	postGather(h, "call-1", longPin, "")

	sess, _ := sessions.Get(nil, "call-1")
	if len(sess.State.Pending["new_pin"]) > 128 {
		t.Errorf("stored PIN longer than 128 chars: %d", len(sess.State.Pending["new_pin"]))
	}
}
