package fsxml_test

import (
	"strings"
	"testing"

	"github.com/mattventura/respond/internal/fsxml"
)

func TestSay(t *testing.T) {
	out := fsxml.Say("Hello world")
	if !strings.Contains(out, `application="speak"`) {
		t.Errorf("Say() missing speak action: %s", out)
	}
	if !strings.Contains(out, "Hello world") {
		t.Errorf("Say() missing message text: %s", out)
	}
	if !strings.Contains(out, `application="hangup"`) {
		t.Errorf("Say() missing hangup: %s", out)
	}
}

func TestGather(t *testing.T) {
	out := fsxml.Gather("Press 1 or 2", "myvar", "/fs/gather", 1)
	if !strings.Contains(out, `application="play_and_get_digits"`) {
		t.Errorf("Gather() missing play_and_get_digits: %s", out)
	}
	if !strings.Contains(out, "myvar") {
		t.Errorf("Gather() missing var name: %s", out)
	}
	if !strings.Contains(out, "Press 1 or 2") {
		t.Errorf("Gather() missing prompt: %s", out)
	}
}

func TestGatherNoLimit(t *testing.T) {
	out := fsxml.Gather("Enter PIN", "pin_var", "/fs/gather", 0)
	if !strings.Contains(out, "#") {
		t.Errorf("Gather() with numDigits=0 should use # terminator: %s", out)
	}
}

func TestDial(t *testing.T) {
	out := fsxml.Dial([]string{"+13035551234", "+13035555678"})
	if !strings.Contains(out, `application="bridge"`) {
		t.Errorf("Dial() missing bridge action: %s", out)
	}
	if !strings.Contains(out, "13035551234") {
		t.Errorf("Dial() missing first number: %s", out)
	}
	if !strings.Contains(out, "13035555678") {
		t.Errorf("Dial() missing second number: %s", out)
	}
}

func TestDialEmpty(t *testing.T) {
	out := fsxml.Dial([]string{})
	if !strings.Contains(out, `application="speak"`) {
		t.Errorf("Dial() with empty numbers should speak error: %s", out)
	}
	if !strings.Contains(out, "no available responders") {
		t.Errorf("Dial() with empty should say no responders: %s", out)
	}
}

func TestXMLEscape(t *testing.T) {
	out := fsxml.Say(`<script>alert("xss")</script>`)
	if strings.Contains(out, "<script>") {
		t.Errorf("Say() did not escape XML: %s", out)
	}
}
