package email

import "net/textproto"

// preserveHeader retains field occurrence order, including empty values. Headers
// is a compatibility view of the first wire occurrence, not security evidence.
func preserveHeader(msg *Message, name, value string) {
	name = textproto.CanonicalMIMEHeaderKey(name)
	if msg.HeaderValues == nil {
		msg.HeaderValues = make(map[string][]string)
	}
	if msg.Headers == nil {
		msg.Headers = make(map[string]string)
	}
	if len(msg.HeaderValues[name]) == 0 {
		msg.Headers[name] = value
	}
	msg.HeaderValues[name] = append(msg.HeaderValues[name], value)
}
