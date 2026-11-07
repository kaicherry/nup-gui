package meta

import "testing"

func TestTlvRoundTrip(t *testing.T) {
	records := map[uint16][]byte{0x0005: []byte("Hello"), 0x0006: []byte("Acme")}
	ser := SerializeBlock(records)
	parsed := ParseBlock(ser)
	if parsed[0x0005][0] != 'H' || parsed[0x0006][0] != 'A' {
		t.Fatalf("tlv roundtrip failed: %v", parsed)
	}
}

func TestLocalizedRoundTrip(t *testing.T) {
	enc := EncodeLocalized(map[string][]byte{"ja": []byte("ゼルダ"), "en": []byte("Zelda")})
	dec := DecodeLocalized(enc)
	if string(dec["ja"]) != "ゼルダ" || string(dec["en"]) != "Zelda" {
		t.Fatalf("bad localized: %v", dec)
	}
}
