package outbox

import "time"

type Message struct {
	Recipient string
	Subject   string
	Body      string
}

type Claimed struct {
	Message
	ID       int64
	Attempts int
}

type SendResult struct {
	Err error
}

type Retry struct {
	LastErr string
	Delay   time.Duration
	ID      int64
}

type Failure struct {
	LastErr string
	ID      int64
}
