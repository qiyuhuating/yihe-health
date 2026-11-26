package commands

import "testing"

func TestWireTypesAndActions(t *testing.T) {
	for _, tc := range []struct{ typ, action, channel, wire string }{
		{"K1", "location", "gprs", "get_locationdata"},
		{"C16", "location", "gprs", "get_locationdata"},
		{"S6T", "restore", "s6t", "resetq"},
		{"X1", "envvoice", "4g", "get_envvoice"},
		{"X1", "location", "4g", "get_locationdataonce"},
	} {
		channel, wire, err := Resolve(tc.typ, tc.action)
		if err != nil || channel != tc.channel || wire != tc.wire {
			t.Fatalf("%+v: %s %s %v", tc, channel, wire, err)
		}
	}
	for _, typ := range []string{"S2_K1", "S2", "BAND_A11", "unknown", "nodevice"} {
		if _, _, err := Resolve(typ, "location"); err == nil {
			t.Fatalf("accepted non-wire or unsupported type %s", typ)
		}
	}
	if _, _, err := Resolve("S6T", "ring"); err == nil {
		t.Fatal("unsupported command")
	}
	if _, _, err := Resolve("S1", "envvoice"); err == nil {
		t.Fatal("unresolved external constant must not become a guessed action")
	}
	if RetrySafe("restore") || RetrySafe("restart") || !RetrySafe("location") {
		t.Fatal("unsafe retry policy")
	}
}
