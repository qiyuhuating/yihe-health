package handlers

import (
	"encoding/json"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"yihe-care/internal/response"
)

func decodeRequiredForTest(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeBody(w, r, dst, false)
}

func TestJSONParserPolicyCharacterization(t *testing.T) {
	type body struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	for _, parser := range []struct {
		name               string
		limit              int
		current, reference func(http.ResponseWriter, *http.Request, any) bool
	}{
		{"ordinary", 1 << 20, decodeRequiredForTest, func(w http.ResponseWriter, r *http.Request, v any) bool { return referenceDecodeBody(w, r, v, false) }},
		{"optional", 1 << 20, func(w http.ResponseWriter, r *http.Request, v any) bool { return decodeBody(w, r, v, true) }, func(w http.ResponseWriter, r *http.Request, v any) bool { return referenceDecodeBody(w, r, v, true) }},
		{"health", 2 << 20, decodeHealth, referenceDecodeHealth},
		{"capability", 2 << 20, decodeCapability, referenceDecodeCapability},
	} {
		t.Run(parser.name, func(t *testing.T) {
			check := func(raw string) {
				t.Helper()
				got, want := body{}, body{}
				w, old := httptest.NewRecorder(), httptest.NewRecorder()
				accepted := parser.current(w, httptest.NewRequest("POST", "/test", strings.NewReader(raw)), &got)
				expected := parser.reference(old, httptest.NewRequest("POST", "/test", strings.NewReader(raw)), &want)
				if accepted != expected || w.Code != old.Code || w.Body.String() != old.Body.String() || !reflect.DeepEqual(got, want) {
					t.Fatalf("input length=%d accepted=%v/%v status=%d/%d body=%q/%q value=%+v/%+v", len(raw), accepted, expected, w.Code, old.Code, w.Body.String(), old.Body.String(), got, want)
				}
			}
			for _, raw := range []string{"", " \n\t", `{}`, `null`, `[]`, `1`, `{"name":"ok","count":1}`, `{"name":"first","name":"last"}`, `{"count":1.5}`, `{"unknown":1}`, `{} {}`, `{} null`, `{} x`, `{"name":"unterminated`, "{\"name\":\"\xff\"}"} {
				check(raw)
			}
			prefix, suffix := `{"name":"`, `"}`
			for _, size := range []int{parser.limit - 1, parser.limit, parser.limit + 1} {
				check(prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix)
			}
			check(`{}` + strings.Repeat(" ", parser.limit-1))
			check(strings.Repeat(" ", parser.limit+1))
			rng := rand.New(rand.NewSource(50))
			for i := 0; i < 1000; i++ {
				value := make([]byte, rng.Intn(128))
				rng.Read(value)
				if i%2 == 0 {
					check(string(value))
				} else {
					check(`{"name":"` + string(value) + `"}`)
				}
			}
		})
	}
}

type trackedBody struct {
	io.Reader
	closes int
}

func (b *trackedBody) Close() error { b.closes++; return nil }

func TestJSONParsersCloseBodiesOnSuccessAndFailure(t *testing.T) {
	for _, parser := range []func(http.ResponseWriter, *http.Request, any) bool{decodeRequiredForTest, decodeHealth, decodeCapability} {
		for _, raw := range []string{`{}`, `{} {}`, `{`} {
			body := &trackedBody{Reader: strings.NewReader(raw)}
			r := httptest.NewRequest("POST", "/test", nil)
			r.Body = body
			parser(httptest.NewRecorder(), r, &struct{}{})
			if body.closes != 1 {
				t.Fatalf("body closed %d times", body.closes)
			}
		}
	}
}

// Frozen parsers from preview@50b9cc0 preserve boundary acceptance and responses.
func referenceDecodeBody(w http.ResponseWriter, r *http.Request, v any, optional bool) bool {
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	defer body.Close()
	decoder := json.NewDecoder(body)
	err := decoder.Decode(v)
	if optional && err == io.EOF {
		return true
	}
	if err == nil {
		var extra any
		if secondErr := decoder.Decode(&extra); secondErr == io.EOF {
			return true
		}
	}
	response.JSON(w, http.StatusBadRequest, map[string]any{
		"returnCode": "0000", "message": "请求参数格式错误",
	})
	return false
}

func referenceDecodeHealth(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	if err := d.Decode(dst); err != nil {
		write400(w, "健康数据 JSON 无效或超出 2 MiB")
		return false
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		write400(w, "只允许一个 JSON 对象")
		return false
	}
	return true
}

func referenceDecodeCapability(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		write400(w, "参数含未知字段、JSON 无效或超出 2 MiB")
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		write400(w, "只允许一个 JSON 对象")
		return false
	}
	return true
}
