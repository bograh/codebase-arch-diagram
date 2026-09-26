package server

import (
	"bytes"
	"sync"
)

// hub fans rendered updates out to SSE subscribers.
type hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func newHub() *hub { return &hub{subs: map[chan []byte]struct{}{}} }

func (h *hub) subscribe() chan []byte {
	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(ch chan []byte) {
	h.mu.Lock()
	delete(h.subs, ch)
	h.mu.Unlock()
}

// publish never blocks. A subscriber that hasn't read the previous message
// has it replaced: only the latest state matters.
func (h *hub) publish(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case <-ch:
		default:
		}
		ch <- msg // cannot block: the buffer was just drained and only publish sends, under h.mu
	}
}

// sseEvent encodes one server-sent event, one data line per input line.
func sseEvent(name string, data []byte) []byte {
	var b bytes.Buffer
	b.WriteString("event: " + name + "\n")
	for _, line := range bytes.Split(data, []byte("\n")) {
		b.WriteString("data: ")
		b.Write(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	return b.Bytes()
}
