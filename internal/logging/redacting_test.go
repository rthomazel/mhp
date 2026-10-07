package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

type lazySecretGroup struct{}

func (lazySecretGroup) LogValue() slog.Value {
	return slog.GroupValue(slog.String("TOKEN", "lazy-secret"))
}

func TestDerivedHandlers(t *testing.T) {
	for _, json := range []bool{false, true} {
		t.Run(map[bool]string{false: "text", true: "json"}[json], func(t *testing.T) {
			var buf bytes.Buffer
			var sink slog.Handler = slog.NewTextHandler(&buf, nil)
			if json {
				sink = slog.NewJSONHandler(&buf, nil)
			}
			h := newRedactingHandler(sink)
			h.RegisterSensitive("token")
			attrs := []slog.Attr{slog.String("TOKEN", "bound-secret"), slog.Group("nested", slog.String("token", "nested-secret"))}
			child := h.WithAttrs(attrs).(*redactingHandler)
			grouped := child.WithGroup("connection").(*redactingHandler)
			grouped.RegisterSensitive("child_only")
			if h.isSensitive("child_only") || child.isSensitive("child_only") {
				t.Fatal("child registration mutated parent")
			}
			if attrs[0].Value.String() != "bound-secret" || attrs[1].Value.Group()[0].Value.String() != "nested-secret" {
				t.Fatal("caller attrs mutated")
			}
			slog.New(grouped).With("stream_id", 42).Info("connected", "token", "record-secret", "child_only", "child-secret", "lazy", lazySecretGroup{}, "group", slog.GroupValue(slog.String("token", "record-group-secret")))
			out := buf.String()
			for _, secret := range []string{"bound-secret", "nested-secret", "record-secret", "child-secret", "lazy-secret", "record-group-secret"} {
				if strings.Contains(out, secret) {
					t.Errorf("secret leaked: %s", secret)
				}
			}
			for _, want := range []string{"[redacted]", "stream_id", "42", "connected"} {
				if !strings.Contains(out, want) {
					t.Errorf("missing %q: %s", want, out)
				}
			}
		})
	}
}

func TestSetupDerivedLogger(t *testing.T) {
	for _, mode := range []string{"proxy", "exit-node", "relay"} {
		t.Run(mode, func(t *testing.T) {
			logger, err := NewSetup(mode, false, "")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = logger.Closer.Close() }()
			logger.Slog.With("stream_id", 1).WithGroup("connection").Info("regression check")
		})
	}
}
