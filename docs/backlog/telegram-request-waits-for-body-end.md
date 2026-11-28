---
worth: later
where: telegram.go:Telegram.Request
added: 2026-11-28
---
# telegram request waits for the body to end after a complete answer

`Request` reads the whole answer with `io.ReadAll` before decoding it. When a 200 answer's JSON has fully
arrived but the stream stays open (chunked without the final chunk, or HTTP/2 without END_STREAM), the call
blocks until `Timeout` (5s by default) and then succeeds; the streaming decode before #47 returned as soon
as the JSON ended. Only a misbehaving proxy or self-hosted Bot API behind `APIURL` triggers it;
api.telegram.org sent Content-Length on a probe. Surfaced reviewing #47; the ReadAll shape was chosen in
that PR's round 2 to keep a decoded answer when the body breaks early.

Fix: decode from `resp.Body` through a small reader that records the first non-EOF read error; a nil
decode error is success, a recorded read error keeps `retry = readOnly`, otherwise return the decode
error. Add a case whose server flushes a complete answer and holds the stream open until the client gives up.
