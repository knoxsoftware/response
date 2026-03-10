package fsxml

import (
	"fmt"
	"strings"
)

// Say returns a FreeSWITCH XML document that speaks msg then hangs up.
func Say(msg string) string {
	return wrap(
		action("speak", xmlEscape(msg)),
		action("hangup", ""),
	)
}

// Gather returns a FreeSWITCH XML document that plays msg and collects DTMF into varName,
// then POSTs to actionURL. numDigits=0 means collect until # (no length limit).
func Gather(msg, varName, actionURL string, numDigits int) string {
	min := "1"
	max := "128"
	terminator := "#"
	if numDigits > 0 {
		max = fmt.Sprintf("%d", numDigits)
		terminator = "none"
	}
	data := fmt.Sprintf("%s %s 3 10000 say:%s %s %s \\d+ 5000 say:Invalid input",
		min, max, xmlEscape(msg), terminator, varName)
	return wrap(
		action("play_and_get_digits", data),
		action("transfer", xmlEscape(actionURL)),
	)
}

// Dial returns a FreeSWITCH XML document that bridges to all numbers simultaneously.
func Dial(numbers []string) string {
	if len(numbers) == 0 {
		return Say("There are no available responders at this time. Please try again later.")
	}
	var legs []string
	for _, n := range numbers {
		legs = append(legs, fmt.Sprintf("sofia/gateway/voipms/%s", xmlEscape(n)))
	}
	data := "{ignore_early_media=true}" + strings.Join(legs, ",")
	return wrap(action("bridge", data))
}

func wrap(actions ...string) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	sb.WriteString(`<document type="freeswitch/xml">`)
	sb.WriteString(`<section name="dialplan">`)
	sb.WriteString(`<context name="default">`)
	sb.WriteString(`<extension name="respond">`)
	sb.WriteString(`<condition field="destination_number" expression=".*">`)
	for _, a := range actions {
		sb.WriteString(a)
	}
	sb.WriteString(`</condition>`)
	sb.WriteString(`</extension>`)
	sb.WriteString(`</context>`)
	sb.WriteString(`</section>`)
	sb.WriteString(`</document>`)
	return sb.String()
}

func action(app, data string) string {
	if data == "" {
		return fmt.Sprintf(`<action application="%s"/>`, app)
	}
	return fmt.Sprintf(`<action application="%s" data="%s"/>`, app, data)
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
