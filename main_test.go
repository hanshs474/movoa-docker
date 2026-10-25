package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type reply struct {
	status int
	body   string
}

type seenReq struct {
	path string
	auth string
	body map[string]any
}

// serve replays response shapes from SPEC §2 in order and records each request.
// The clock is virtual: sleep advances it, so polling tests run instantly.
func serve(t *testing.T, replies []reply) *[]seenReq {
	t.Helper()
	var seen []seenReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var b map[string]any
		_ = json.Unmarshal(raw, &b)
		seen = append(seen, seenReq{r.URL.Path, r.Header.Get("Authorization"), b})
		if len(replies) == 0 {
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		rp := replies[0]
		if len(replies) > 1 {
			replies = replies[1:]
		}
		w.WriteHeader(rp.status)
		_, _ = w.Write([]byte(rp.body))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MOVOA_BASE_URL", srv.URL)
	clock := time.Unix(0, 0)
	now = func() time.Time { return clock }
	sleep = func(d time.Duration) { clock = clock.Add(d) }
	return &seen
}

func runCLI(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := run(args, "sk-test", &out, &errb)
	return code, strings.TrimSpace(out.String()), errb.String()
}

const (
	accepted   = `{"code":0,"message":"ok","data":{"id":"t1","status":"pending"}}`
	processing = `{"code":0,"message":"ok","data":{"id":"t1","status":"processing"}}`
	queued     = `{"code":0,"message":"ok","data":{"id":"t1","status":"pending","queued":true,"queueEta":120}}`
	done       = `{"code":0,"message":"ok","data":{"id":"t1","status":"success","videos":["https://cdn.movoa.ai/t1.mp4"]}}`
)

func TestTextToVideoSuccess(t *testing.T) {
	seen := serve(t, []reply{{200, accepted}, {200, queued}, {200, processing}, {200, done}})
	code, out, errs := runCLI("video", "a fox in fresh snow")
	if code != 0 || out != "https://cdn.movoa.ai/t1.mp4" {
		t.Fatalf("exit %d out %q err %q", code, out, errs)
	}
	if !strings.Contains(errs, "queued, about 120 s") {
		t.Fatalf("queue progress not shown: %q", errs)
	}
	s := *seen
	sub := s[0]
	if sub.path != "/api/ai/generate" || sub.auth != "Bearer sk-test" {
		t.Fatalf("submit %+v", sub)
	}
	b := sub.body
	if b["provider"] != "auto" || b["mediaType"] != "video" || b["model"] != "minimax-h3" ||
		b["scene"] != "text-to-video" || b["prompt"] != "a fox in fresh snow" {
		t.Fatalf("submit body %v", b)
	}
	opts := b["options"].(map[string]any)
	if opts["sound"] != true || len(opts) != 1 {
		t.Fatalf("default options %v", opts)
	}
	if s[1].path != "/api/ai/query" || s[1].body["taskId"] != "t1" {
		t.Fatalf("query %+v", s[1])
	}
}

func TestImageToVideoFlagsAfterPrompt(t *testing.T) {
	seen := serve(t, []reply{{200, accepted}})
	code, out, errs := runCLI("video", "the camera orbits the statue",
		"--model", "seedance-2-5", "--image", "https://example.com/s.jpg",
		"--duration", "10", "--resolution", "720p", "--aspect-ratio", "9:16", "--no-wait")
	if code != 0 || out != "t1" {
		t.Fatalf("exit %d out %q err %q", code, out, errs)
	}
	b := (*seen)[0].body
	if b["model"] != "seedance-2-5" || b["scene"] != "image-to-video" || b["prompt"] != "the camera orbits the statue" {
		t.Fatalf("submit body %v", b)
	}
	o := b["options"].(map[string]any)
	img := o["image_input"].([]any)
	if o["duration"] != 10.0 || o["resolution"] != "720p" || o["aspect_ratio"] != "9:16" ||
		o["sound"] != true || len(img) != 1 || img[0] != "https://example.com/s.jpg" {
		t.Fatalf("options %v", o)
	}
	if len(*seen) != 1 {
		t.Fatal("--no-wait must not poll")
	}
}

func TestSubmitErrors(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		body   string
		kind   string
		exit   int
	}{
		{"401", 401, `{"code":-1,"message":"Invalid API key","data":{"reason":"invalid_api_key"}}`, "auth", 3},
		{"401 non-json", 401, `Unauthorized`, "auth", 3},
		{"no auth", 200, `{"code":-1,"message":"no auth"}`, "auth", 3},
		{"plan_required", 200, `{"code":-1,"message":"This model needs a plan","data":{"reason":"plan_required"}}`, "plan", 4},
		{"insufficient_credits", 200, `{"code":-1,"message":"Not enough credits","data":{"reason":"insufficient_credits"}}`, "quota", 4},
		{"concurrency", 200, `{"code":-1,"message":"A run is already in progress","data":{"reason":"concurrency"}}`, "busy", 1},
		{"code -2", 200, `{"code":-2,"message":"Prompt blocked"}`, "rejected", 1},
		{"code -3", 200, `{"code":-3,"message":"Cooling down"}`, "rejected", 1},
		{"other -1", 200, `{"code":-1,"message":"invalid duration"}`, "other", 1},
		{"5xx", 502, `<html>bad gateway</html>`, "other", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			serve(t, []reply{{c.status, c.body}})
			code, _, errs := runCLI("video", "x")
			if code != c.exit || !strings.HasPrefix(errs, c.kind+":") {
				t.Fatalf("exit %d %q", code, errs)
			}
		})
	}
}

func TestFailedRun(t *testing.T) {
	serve(t, []reply{{200, accepted},
		{200, `{"code":0,"message":"ok","data":{"id":"t1","status":"failed","errorMessage":"upstream refused the image","failureKind":"provider"}}`}})
	code, _, errs := runCLI("video", "x")
	if code != 1 || !strings.HasPrefix(errs, "failed:") || !strings.Contains(errs, "upstream refused the image") {
		t.Fatalf("exit %d %q", code, errs)
	}
}

func TestCanceledRun(t *testing.T) {
	serve(t, []reply{{200, accepted}, {200, `{"code":0,"message":"ok","data":{"id":"t1","status":"canceled"}}`}})
	if code, _, errs := runCLI("video", "x"); code != 1 || !strings.Contains(errs, "canceled") {
		t.Fatalf("exit %d %q", code, errs)
	}
}

func TestTimeoutCarriesID(t *testing.T) {
	seen := serve(t, []reply{{200, accepted}, {200, processing}})
	code, _, errs := runCLI("video", "x", "--timeout", "1m")
	if code != 1 || !strings.Contains(errs, "\ntimeout: ") || !strings.Contains(errs, "movoa status t1") {
		t.Fatalf("exit %d %q", code, errs)
	}
	// 1 submit + polls at t=0,10,...,60s
	if n := len(*seen); n != 8 {
		t.Fatalf("expected 8 requests at a 10 s interval, got %d", n)
	}
}

func TestDefaultTimeoutIs30Minutes(t *testing.T) {
	seen := serve(t, []reply{{200, accepted}, {200, processing}})
	if code, _, errs := runCLI("video", "x"); code != 1 || !strings.Contains(errs, "30m0s") {
		t.Fatalf("exit %d %q", code, errs)
	}
	if n := len(*seen) - 1; n != 181 {
		t.Fatalf("expected 181 polls (t=0 to 30m, every 10 s) in 30 minutes, got %d", n)
	}
}

func TestDroppedPollIsNotFailure(t *testing.T) {
	serve(t, []reply{{200, accepted}, {503, `oops`}, {200, done}})
	if code, out, errs := runCLI("video", "x"); code != 0 || out != "https://cdn.movoa.ai/t1.mp4" {
		t.Fatalf("exit %d %q %q", code, out, errs)
	}
}

func TestStatus(t *testing.T) {
	seen := serve(t, []reply{{200, done}})
	code, out, _ := runCLI("status", "t1")
	if code != 0 || (*seen)[0].body["taskId"] != "t1" {
		t.Fatalf("exit %d", code)
	}
	var got task
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.Status != "success" || got.Videos[0] != "https://cdn.movoa.ai/t1.mp4" {
		t.Fatalf("status output %q", out)
	}
}

func TestCredits(t *testing.T) {
	seen := serve(t, []reply{{200, `{"code":0,"message":"ok","data":{"remainingCredits":42}}`}})
	if code, out, _ := runCLI("credits"); code != 0 || out != "42" || (*seen)[0].path != "/api/user/get-user-credits" {
		t.Fatalf("exit %d %q", code, out)
	}
	serve(t, []reply{{200, `{"code":-1,"message":"no auth, please sign in"}`}})
	if code, _, errs := runCLI("credits"); code != 3 || !strings.Contains(errs, "/settings/apikeys") {
		t.Fatalf("credits no auth: exit %d %q", code, errs)
	}
	serve(t, []reply{{401, `{"code":-1,"message":"Invalid API key","data":{"reason":"invalid_api_key"}}`}})
	if code, _, _ := runCLI("credits"); code != 3 {
		t.Fatalf("credits 401: exit %d", code)
	}
}

func TestNoKey(t *testing.T) {
	seen := serve(t, nil)
	var out, errb bytes.Buffer
	if code := run([]string{"credits"}, "  ", &out, &errb); code != 3 || !strings.HasPrefix(errb.String(), "auth:") {
		t.Fatalf("exit %d %q", code, errb.String())
	}
	if len(*seen) != 0 {
		t.Fatal("no request should go out without a key")
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"video"}, {"status"}, {"nope"}, {"video", "--duration", "x", "p"}} {
		if code, _, _ := runCLI(args...); code != 2 {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
}
