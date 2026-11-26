package commands

import (
	"strings"
	"testing"
)

func TestChannelLookupAllWireTypes(t *testing.T) {
	seen := map[string]bool{"S6T": true}
	for _, group := range []struct{ values, channel string }{{fourG, "4g"}, {gprs, "gprs"}} {
		for _, typ := range strings.Fields(group.values) {
			if seen[typ] {
				t.Fatalf("duplicate wire type %s", typ)
			}
			seen[typ] = true
			got, err := Channel(typ)
			if err != nil || got != group.channel {
				t.Fatalf("type=%s got=%s err=%v", typ, got, err)
			}
			for _, unsupported := range []string{strings.ToLower(typ), " " + typ, typ + " "} {
				if _, err := Channel(unsupported); err == nil {
					t.Fatalf("accepted %q", unsupported)
				}
			}
		}
	}
	if got, err := Channel("S6T"); err != nil || got != "s6t" {
		t.Fatal("S6T lookup changed")
	}
	if _, err := Channel(""); err == nil {
		t.Fatal("empty wire type accepted")
	}
}

func BenchmarkResolveCommand(b *testing.B) {
	for _, typ := range []string{"X1", "S1", "S6T"} {
		b.Run(typ, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := Resolve(typ, "location"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
