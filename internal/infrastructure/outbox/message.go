// Package outbox holds the email outbox: the queued-message type now, and the
// draining relay in a later change.
package outbox

// Message is a single email queued in email_outbox and later drained by the relay.
type Message struct {
	Recipient string
	Subject   string
	Body      string
}
