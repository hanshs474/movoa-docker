// Command movoa makes a video clip with sound on Movoa (https://movoa.ai) and
// prints the URL of the finished file.
//
//	movoa video "a paper boat drifting down a rain gutter, close-up"
//	movoa video "a street drummer in the rain" --model seedance-2-5 --duration 10 --aspect-ratio 9:16
//	movoa video "the camera slowly orbits the statue" --image https://example.com/statue.jpg
//	movoa video "a fox trotting through fresh snow" --no-wait
//	movoa status <id>
//	movoa credits
//
// Set MOVOA_API_KEY (create a key at https://movoa.ai/settings/apikeys).
// Exit codes: 0 ok, 1 failure, 2 usage, 3 auth, 4 out of credits / plan.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var (
	version = "0.1.0"
	baseURL = "https://movoa.ai"
	client  = &http.Client{Timeout: 60 * time.Second}
	sleep   = time.Sleep
	now     = time.Now
)

const (
	keysURL      = "https://movoa.ai/settings/apikeys"
	defaultModel = "minimax-h3"
	pollEvery    = 10 * time.Second
)

// apiError carries the reason a call failed, in terms of what to do next.
type apiError struct {
	Kind    string // auth quota plan busy rejected failed timeout other
	Message string
}

func (e *apiError) Error() string { return e.Kind + ": movoa: " + e.Message }

type envelope struct {
	Code    *int            `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// classify maps a refusal to its kind. movoa.ai answers almost every refusal
// as HTTP 200 with a non-zero code and a data.reason, so the HTTP status alone
// would read a refusal as success.
func classify(status int, env envelope) *apiError {
	msg := env.Message
	if msg == "" {
		msg = fmt.Sprintf("request failed (HTTP %d)", status)
	}
	if status == 401 {
		return &apiError{"auth", msg + "; create a key at " + keysURL}
	}
	code := 0
	if env.Code != nil {
		code = *env.Code
	}
	switch code {
	case -2:
		return &apiError{"rejected", msg + " (the content filter refused this prompt; reword it)"}
	case -3:
		return &apiError{"rejected", msg + " (cooling down after a refused prompt; wait, then reword)"}
	}
	var d struct {
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(env.Data, &d)
	switch d.Reason {
	case "insufficient_credits":
		return &apiError{"quota", msg}
	case "plan_required":
		return &apiError{"plan", msg}
	case "concurrency":
		return &apiError{"busy", msg}
	}
	if strings.Contains(strings.ToLower(msg), "no auth") {
		return &apiError{"auth", msg + "; create a key at " + keysURL}
	}
	return &apiError{"other", msg}
}

func post(key, path string, body any, out any) error {
	if key == "" {
		return &apiError{"auth", "no API key; set MOVOA_API_KEY to a key from " + keysURL}
	}
	var rd io.Reader
	if body != nil {
		buf, _ := json.Marshal(body)
		rd = bytes.NewReader(buf)
	}
	req, _ := http.NewRequest("POST", baseURL+path, rd)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "movoa-cli/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return &apiError{"other", "could not reach movoa.ai: " + err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		if resp.StatusCode == 401 {
			return classify(401, envelope{})
		}
		return &apiError{"other", fmt.Sprintf("movoa.ai returned a non-JSON response (HTTP %d)", resp.StatusCode)}
	}
	if resp.StatusCode == 401 || env.Code == nil || *env.Code != 0 {
		return classify(resp.StatusCode, env)
	}
	if resp.StatusCode >= 300 {
		return &apiError{"other", fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return &apiError{"other", "unreadable answer from movoa.ai"}
		}
	}
	return nil
}

type task struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	Videos       []string `json:"videos,omitempty"`
	ErrorMessage string   `json:"errorMessage,omitempty"`
	FailureKind  string   `json:"failureKind,omitempty"`
	Queued       bool     `json:"queued,omitempty"`
	QueueEta     float64  `json:"queueEta,omitempty"`
}

type videoInput struct {
	Prompt      string
	Model       string
	ImageURL    string
	Duration    int
	Resolution  string
	AspectRatio string
}

func submit(key string, in videoInput) (task, error) {
	options := map[string]any{"sound": true}
	if in.Duration > 0 {
		options["duration"] = in.Duration
	}
	if in.Resolution != "" {
		options["resolution"] = in.Resolution
	}
	if in.AspectRatio != "" {
		options["aspect_ratio"] = in.AspectRatio
	}
	scene := "text-to-video"
	if in.ImageURL != "" {
		scene = "image-to-video"
		options["image_input"] = []string{in.ImageURL}
	}
	model := in.Model
	if model == "" {
		model = defaultModel
	}
	var t task
	err := post(key, "/api/ai/generate", map[string]any{
		"provider":  "auto", // the server routes by model; this only has to be non-empty
		"mediaType": "video",
		"model":     model,
		"scene":     scene,
		"prompt":    in.Prompt,
		"options":   options,
	}, &t)
	if err != nil {
		return t, err
	}
	if t.ID == "" {
		return t, &apiError{"other", "movoa.ai accepted the run but returned no id"}
	}
	return t, nil
}

func status(key, id string) (task, error) {
	var t task
	err := post(key, "/api/ai/query", map[string]string{"taskId": id}, &t)
	if t.ID == "" {
		t.ID = id
	}
	return t, err
}

func credits(key string) (int, error) {
	var d struct {
		RemainingCredits int `json:"remainingCredits"`
	}
	err := post(key, "/api/user/get-user-credits", nil, &d)
	return d.RemainingCredits, err
}

// wait polls until the run ends or the limit passes. Progress goes to log.
func wait(key, id string, limit time.Duration, log io.Writer) (string, error) {
	deadline := now().Add(limit)
	last := ""
	for {
		t, err := status(key, id)
		if err != nil {
			if e, ok := err.(*apiError); ok && e.Kind != "other" {
				return "", err
			}
			// a dropped poll is not a failed run
		} else {
			switch t.Status {
			case "success":
				if len(t.Videos) == 0 {
					return "", &apiError{"failed", "run " + id + " finished with no video"}
				}
				return t.Videos[0], nil
			case "failed", "canceled":
				msg := "run " + id + " " + t.Status
				if t.ErrorMessage != "" {
					msg += ": " + t.ErrorMessage
				}
				return "", &apiError{"failed", msg}
			}
			line := t.Status
			if t.Queued && t.QueueEta > 0 {
				line = fmt.Sprintf("queued, about %d s", int(t.QueueEta))
			}
			if line != last && line != "" {
				fmt.Fprintln(log, "movoa:", id, line)
				last = line
			}
		}
		if now().Add(pollEvery).After(deadline) {
			return "", &apiError{"timeout", "still generating after " + limit.String() + "; check later with: movoa status " + id}
		}
		sleep(pollEvery)
	}
}

// parseInterspersed lets flags come before or after the positional words.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return rest, nil
		}
		if args[0] == "--" {
			return append(rest, args[1:]...), nil
		}
		rest = append(rest, args[0])
		args = args[1:]
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `usage:
  movoa video "<prompt>" [--model M] [--image URL] [--duration S] [--resolution R] [--aspect-ratio A] [--no-wait] [--timeout 30m]
  movoa status <id>
  movoa credits
  movoa version

Makes a video clip with sound on https://movoa.ai and prints the file URL.
Needs MOVOA_API_KEY (create a key at %s).
Models: minimax-h3 (default), seedance-2-5, seedance-2, seedance-2-fast, seedance-2-mini, veo-3-1-fast.
`, keysURL)
}

func main() { os.Exit(run(os.Args[1:], os.Getenv("MOVOA_API_KEY"), os.Stdout, os.Stderr)) }

func run(args []string, key string, stdout, stderr io.Writer) int {
	if v := strings.TrimRight(os.Getenv("MOVOA_BASE_URL"), "/"); v != "" {
		baseURL = v
	}
	key = strings.TrimSpace(key)
	fail := func(err error) int {
		fmt.Fprintln(stderr, err)
		if e, ok := err.(*apiError); ok {
			switch e.Kind {
			case "auth":
				return 3
			case "quota", "plan":
				return 4
			}
		}
		return 1
	}
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "version", "--version", "-version":
		fmt.Fprintln(stdout, "movoa", version)
		return 0
	case "help", "--help", "-help", "-h":
		usage(stdout)
		return 0
	case "credits":
		n, err := credits(key)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, n)
		return 0
	case "status":
		if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
			usage(stderr)
			return 2
		}
		t, err := status(key, args[0])
		if err != nil {
			return fail(err)
		}
		out, _ := json.Marshal(t)
		fmt.Fprintln(stdout, string(out))
		return 0
	case "video":
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", cmd)
		usage(stderr)
		return 2
	}

	fs := flag.NewFlagSet("movoa video", flag.ContinueOnError)
	fs.SetOutput(stderr)
	in := videoInput{}
	fs.StringVar(&in.Model, "model", defaultModel, "minimax-h3, seedance-2-5, seedance-2, seedance-2-fast, seedance-2-mini, veo-3-1-fast")
	fs.StringVar(&in.ImageURL, "image", "", "public https image URL to animate (photo to video)")
	fs.IntVar(&in.Duration, "duration", 0, "clip length in seconds (model default if unset)")
	fs.StringVar(&in.Resolution, "resolution", "", "480p, 720p or 768p, depending on the model")
	fs.StringVar(&in.AspectRatio, "aspect-ratio", "", "16:9, 9:16 or 1:1")
	noWait := fs.Bool("no-wait", false, "print the run id and exit without waiting")
	timeout := fs.Duration("timeout", 30*time.Minute, "how long to wait for the result")
	words, err := parseInterspersed(fs, args)
	if err != nil {
		return 2
	}
	in.Prompt = strings.TrimSpace(strings.Join(words, " "))
	if in.Prompt == "" {
		usage(stderr)
		return 2
	}
	t, err := submit(key, in)
	if err != nil {
		return fail(err)
	}
	if *noWait {
		fmt.Fprintln(stdout, t.ID)
		return 0
	}
	url, err := wait(key, t.ID, *timeout, stderr)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintln(stdout, url)
	return 0
}
