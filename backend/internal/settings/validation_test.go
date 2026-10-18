package settings

import (
	"testing"

	"wsapi-video/internal/config"
)

func TestPaymentBasePathValidationPreservesOriginRestrictions(t *testing.T) {
	s := New(nil, config.Config{
		PublicURL: "https://studio.example", DevMode: true, StorageDriver: "local", SMTPPort: 587,
		EasyPayNotifyURL: "https://studio.example/api/v1/payment/notify/easypay", EasyPayReturnURL: "https://studio.example/wallet",
	})
	v := s.defaults().Settings
	for _, origin := range []string{"https://pay.example", "https://pay.example/epay", "https://pay.example/epay/"} {
		v.Payment.Origin = origin
		if fields := s.validate(v); len(fields) != 0 {
			t.Fatalf("valid payment base URL rejected: %s: %v", origin, fields)
		}
	}
	for _, origin := range []string{"http://pay.example/epay", "https://pay.example/epay?", "https://pay.example/epay/../other"} {
		v.Payment.Origin = origin
		if fields := s.validate(v); fields["payment.origin"] == "" {
			t.Fatalf("invalid payment base URL accepted: %s", origin)
		}
	}
	v.Payment.Origin = "https://pay.example/epay"
	v.Media.Origin = "https://media.example/epay"
	v.Media.OutputOrigins = []string{"https://files.example/prefix"}
	v.Storage.Endpoint = "https://storage.example/prefix"
	fields := s.validate(v)
	for _, name := range []string{"media.origin", "media.output_origins", "storage.endpoint"} {
		if fields[name] == "" {
			t.Errorf("%s must still reject path prefixes", name)
		}
	}
}
