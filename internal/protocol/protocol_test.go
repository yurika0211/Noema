package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	payload, err := json.Marshal(ContentPayload{Text: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := Envelope{
		Protocol:        Version,
		Type:            TypeContent,
		EventID:         "evt_1",
		Seq:             3,
		RunID:           "run_1",
		ClientRequestID: "req_1",
		SessionID:       "sess_1",
		CharacterID:     "hiori",
		CreatedAt:       time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC),
		Payload:         payload,
	}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	var got Envelope
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Protocol != Version || got.Type != TypeContent || got.Seq != 3 {
		t.Fatalf("envelope: %+v", got)
	}
	var body ContentPayload
	if err := json.Unmarshal(got.Payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.Text != "你好" {
		t.Fatalf("text=%q", body.Text)
	}
}
