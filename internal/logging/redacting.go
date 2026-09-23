package logging

import (
	"context"
	"log/slog"
	"strings"
)

// redactingHandler is a slog.Handler that masks sensitive attributes before
// anything is emitted. It exists to make the "never log secrets or payloads"
// rule a property of the handler rather than a promise each call site honours.
//
// The handler wraps another slog.Handler. Before delegating it walks the
// record's attributes and masks any whose key is registered as sensitive.
// Masking is by key alone, so a redacted value can never leak through a
// differently named key, and it applies uniformly to console and file output.
type redactingHandler struct {
	next      slog.Handler
	sensitive map[string]bool
}

// newRedactingHandler builds a redactingHandler wrapping next. next is usually
// a slog.TextHandler or slog.JSONHandler writing to the injected writer.
func newRedactingHandler(next slog.Handler) *redactingHandler {
	return &redactingHandler{next: next, sensitive: map[string]bool{}}
}

// RegisterSensitive marks keys as sensitive. Any attribute whose key matches
// (case-insensitively) is masked to "[redacted]" before emission. Idempotent.
func (h *redactingHandler) RegisterSensitive(keys ...string) {
	for _, k := range keys {
		h.sensitive[strings.ToLower(k)] = true
	}
}

// Enabled reports whether records at lvl are emitted, deferring to the wrapped
// handler so level filtering is inherited unchanged.
func (h *redactingHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.next.Enabled(ctx, lvl)
}

// Handle builds a fresh record whose sensitive attributes are masked, then
// delegates. There is no way to mutate a record's attributes in place: Attrs
// hands copies to its callback and the record has no clear method, so we
// reconstruct the attribute list on a new record and hand that to the next
// handler. This is the only correct way to guarantee the mask is applied.
func (h *redactingHandler) Handle(ctx context.Context, rec slog.Record) error {
	result := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		if h.isSensitive(a.Key) {
			a.Value = slog.StringValue("[redacted]")
		}
		result.AddAttrs(a)
		return true
	})
	return h.next.Handle(ctx, result)
}

// WithAttrs returns a child handler carrying the additional attrs, deferring so
// inherited behaviour (level, handler composition) is preserved.
func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	child := &redactingHandler{next: h.next.WithAttrs(attrs)}
	for k := range h.sensitive {
		child.sensitive[k] = true
	}
	return child
}

// WithGroup returns a child handler for a named group, deferring so inherited
// behaviour is preserved.
func (h *redactingHandler) WithGroup(name string) slog.Handler {
	child := &redactingHandler{next: h.next.WithGroup(name)}
	for k := range h.sensitive {
		child.sensitive[k] = true
	}
	return child
}

// isSensitive reports whether key is registered as sensitive.
func (h *redactingHandler) isSensitive(key string) bool {
	return h.sensitive[strings.ToLower(key)]
}
