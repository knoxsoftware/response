package twiml

import (
	"fmt"
	"strings"
)

// Say returns a TwiML <Response><Say> block.
func Say(msg string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Response><Say>%s</Say></Response>`, xmlEscape(msg))
}

// Gather returns a TwiML response prompting for DTMF input that POSTs to action.
// If numDigits is 0, no numDigits attribute is set and Twilio relies on finishOnKey ("#" by default).
func Gather(msg, action string, numDigits int) string {
	numDigitsAttr := ""
	if numDigits > 0 {
		numDigitsAttr = fmt.Sprintf(` numDigits="%d"`, numDigits)
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Response><Gather%s action="%s" method="POST"><Say>%s</Say></Gather></Response>`,
		numDigitsAttr, xmlEscape(action), xmlEscape(msg))
}

// Dial returns a TwiML response dialing all numbers simultaneously.
func Dial(numbers []string) string {
	if len(numbers) == 0 {
		return Say("There are no available responders at this time. Please try again later.")
	}
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?><Response><Dial>`)
	for _, n := range numbers {
		sb.WriteString(fmt.Sprintf(`<Number>%s</Number>`, xmlEscape(n)))
	}
	sb.WriteString(`</Dial></Response>`)
	return sb.String()
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
