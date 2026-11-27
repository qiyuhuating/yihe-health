package fence

import (
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"strconv"
	"testing"
)

func legacyPoint(b []byte, p *Point) error {
	var values []*float64
	if err := json.Unmarshal(b, &values); err != nil {
		return err
	}
	if len(values) != 2 || values[0] == nil || values[1] == nil {
		return errors.New("invalid coordinate")
	}
	*p = Point{*values[0], *values[1]}
	return nil
}

func TestPointDecodeDifferential(t *testing.T) {
	cases := []string{"[0,0]", " [ -0, 1e-320 ] ", "[1e309,0]", "[1,2,null]", "[1,2,3]", "[null,1]", "[1,null]", "[1]", "[]", "null", "[01,1]", "[1,+1]", "[1,NaN]", "[1,Infinity]", "[1,true]", "[1,{}]", "[1,[]]", "[1,\"2\"]", "[1,2] trailing", "[1,2,]", "[1,2e-999]"}
	rng := rand.New(rand.NewSource(20261126))
	for range 5000 {
		x := math.Float64frombits(rng.Uint64())
		y := math.Float64frombits(rng.Uint64())
		cases = append(cases, "["+strconv.FormatFloat(x, 'g', -1, 64)+","+strconv.FormatFloat(y, 'g', -1, 64)+"]")
	}
	for _, s := range cases {
		raw := []byte(s)
		want, got := Point{7, 8}, Point{7, 8}
		e1, e2 := legacyPoint(raw, &want), got.UnmarshalJSON(raw)
		if (e1 == nil) != (e2 == nil) || math.Float64bits(want[0]) != math.Float64bits(got[0]) || math.Float64bits(want[1]) != math.Float64bits(got[1]) {
			t.Fatalf("%s want=%v %v got=%v %v", s, want, e1, got, e2)
		}
	}
}

var decodedPoint Point

func BenchmarkPointDecode(b *testing.B) {
	for _, raw := range [][]byte{[]byte("[120.123456,30.123456]"), []byte("[-1.2345e-4,4.5678e+1]")} {
		b.Run(string(raw)+"/legacy", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if err := legacyPoint(raw, &decodedPoint); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(string(raw)+"/direct", func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if err := decodedPoint.UnmarshalJSON(raw); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
