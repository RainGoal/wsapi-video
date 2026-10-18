package settings

import (
	"bytes"
	"strings"
	"testing"
	"wsapi-video/internal/config"
)

func TestCredentialEncryptionAuthenticatesVersionAndMasterKey(t *testing.T) {
	s := New(nil, config.Config{AppSecret: strings.Repeat("master-one", 5)})
	st := State{Settings: Settings{Media: Media{ImageKey: "sensitive-test-credential"}}}
	a, err := s.seal(st, "credential:media:version-one")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.seal(st, "credential:media:version-one")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) || bytes.Contains(a, []byte(st.Settings.Media.ImageKey)) {
		t.Fatal("credential ciphertext is deterministic or exposes plaintext")
	}
	var out State
	if err = s.open(a, "credential:media:version-one", &out); err != nil || out.Settings.Media.ImageKey != st.Settings.Media.ImageKey {
		t.Fatal("credential failed roundtrip", err)
	}
	if err = s.open(a, "credential:media:version-two", &out); err == nil {
		t.Fatal("ciphertext could be swapped between credential versions")
	}
	tampered := append([]byte{}, a...)
	tampered[len(tampered)-1] ^= 1
	if err = s.open(tampered, "credential:media:version-one", &out); err == nil {
		t.Fatal("modified ciphertext accepted")
	}
	wrong := New(nil, config.Config{AppSecret: strings.Repeat("master-two", 5)})
	if err = wrong.open(a, "credential:media:version-one", &out); err == nil {
		t.Fatal("different master key accepted")
	}
}

func TestPolicyDefaultsAndLegacyEncryptedSettings(t *testing.T) {
	s := New(nil, config.Config{AppSecret: strings.Repeat("policy-master", 4)})
	if got := s.defaults().Settings.Site; got != (Site{Brand: "FRAME STUDIO", RegistrationEnabled: true}) {
		t.Fatal("unconfigured policies must remain empty", got)
	}
	legacy := map[string]any{
		"settings": map[string]any{"site": map[string]any{
			"brand": "Legacy Studio", "registration_enabled": true, "support_email": "legacy@example.test",
		}},
		"revision": 7,
	}
	encrypted, err := s.seal(legacy, "runtime-settings")
	if err != nil {
		t.Fatal(err)
	}
	var restored State
	if err = s.open(encrypted, "runtime-settings", &restored); err != nil {
		t.Fatal(err)
	}
	want := Site{Brand: "Legacy Studio", RegistrationEnabled: true, SupportEmail: "legacy@example.test"}
	if restored.Revision != 7 || restored.Settings.Site != want {
		t.Fatal("legacy configuration must decode with empty policies", restored)
	}
}
