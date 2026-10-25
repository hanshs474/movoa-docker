# Movoa CLI image

Unlimited AI video generator with sound: on paid plans the tenth take of a shot costs what the first did, and your first clip is free to make right now. This is the official command-line client for [Movoa](https://movoa.ai/?utm_source=dockerhub&utm_medium=package) as a Docker image: type a prompt, get back the URL of the finished clip with its sound in the same file.

```bash
docker run --rm -e MOVOA_API_KEY san8888/movoa video "a paper boat drifting down a rain gutter, close-up, rain ticking on tin"
# prints the URL of the finished MP4
```

It covers the same flows as the site: [text to video](https://movoa.ai/video/text-to-video), [photo to video](https://movoa.ai/video/photo-to-video) and [AI video with audio](https://movoa.ai/video/ai-video-with-audio), on MiniMax H3, [Seedance 2.5](https://movoa.ai/video/seedance-2-5), Seedance 2.0 and [Veo 3](https://movoa.ai/video/google-veo-3) Fast.

## Install

```bash
docker pull san8888/movoa
```

A static Go binary on an empty base image, for `linux/amd64` and `linux/arm64`. Source: [github.com/hanshs474/movoa-docker](https://github.com/hanshs474/movoa-docker). The same binary ships as a snap: `sudo snap install movoa`.

## API key

Sign up on [movoa.ai](https://movoa.ai/?utm_source=dockerhub&utm_medium=package), create a key at [movoa.ai/settings/apikeys](https://movoa.ai/settings/apikeys), then export it and pass it through:

```bash
export MOVOA_API_KEY=sk-...
```

Runs cost credits on your Movoa account, at the prices on [movoa.ai/pricing](https://movoa.ai/pricing). Every run shows up in your library on movoa.ai.

## Commands

```bash
movoa video "<prompt>" [--model M] [--image URL] [--duration S] [--resolution R] [--aspect-ratio A] [--no-wait] [--timeout 30m]
movoa status <id>     # JSON: status, videos, errorMessage, queued, queueEta
movoa credits         # credits left on your account
```

With Docker, put `docker run --rm -e MOVOA_API_KEY san8888/movoa` in front of each one.

```bash
# Seedance 2.5, 10 seconds, vertical
docker run --rm -e MOVOA_API_KEY san8888/movoa video "a street drummer in the rain, handheld" \
  --model seedance-2-5 --duration 10 --resolution 720p --aspect-ratio 9:16

# photo to video: the image becomes the first frame, the prompt describes the motion
docker run --rm -e MOVOA_API_KEY san8888/movoa video "the camera slowly orbits the statue" \
  --image https://example.com/statue.jpg

# submit now, collect later
docker run --rm -e MOVOA_API_KEY san8888/movoa video "a fox trotting through fresh snow" --no-wait   # prints the run id
docker run --rm -e MOVOA_API_KEY san8888/movoa status <id>
```

A run waits up to 30 minutes by default, checking every 10 seconds; progress (including the queue estimate) goes to stderr, the URL to stdout. Change the wait with `--timeout 45m`.

## Models

| `--model` | Length (`--duration`) | `--resolution` | `--aspect-ratio` |
|---|---|---|---|
| `minimax-h3` (default) | 4, 5, 6, 8, 10, 15 s (6) | 480p, 768p | 16:9, 9:16, 1:1 |
| `seedance-2-5` | 5 to 30 s, steps of 5 | 480p, 720p | 16:9, 9:16, 1:1 |
| `veo-3-1-fast` | 8 s | 720p | 16:9, 9:16 (photo to video: 16:9) |
| `seedance-2` · `seedance-2-fast` · `seedance-2-mini` | 4, 6, 8, 10 s | 480p, 720p | 16:9, 9:16, 1:1 |

Every model returns the clip with sound.

## Errors and exit codes

Errors print to stderr prefixed with their kind:

- `auth` (exit 3): no key, or the key is wrong or deleted; create one at [movoa.ai/settings/apikeys](https://movoa.ai/settings/apikeys)
- `quota` (exit 4): not enough credits left for this run
- `plan` (exit 4): this model or resolution needs a plan on your account
- `busy` (exit 1): your plan's concurrency limit is reached; wait for a run to finish
- `rejected` (exit 1): the content filter refused the prompt, or the account is cooling down after a refusal; reword rather than retry
- `failed` (exit 1): the run was accepted and then failed; the message says why
- `timeout` (exit 1): the wait ran out; the run may still finish, check it with `movoa status <id>`

Anything else (a network failure, a 5xx, a parameter out of range) comes back as `other` with the service's message. Usage errors exit 2.

`MOVOA_BASE_URL` overrides the endpoint; only useful against a local build of the site.

MIT licensed. Made by [Movoa](https://movoa.ai/?utm_source=dockerhub&utm_medium=package).
