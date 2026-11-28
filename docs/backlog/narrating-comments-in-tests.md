---
worth: later
where: telegram_test.go
added: 2026-11-28
---
# narrating comments in tests

#47 added roughly 35-44 comments across `telegram_test.go`, `slack_test.go`, `email_test.go`,
`interface_test.go` and `webhook_test.go`: doc paragraphs above test functions (`TestTelegram_RequestRetries`)
and per-case narration that restates the test name or setup. Tests here are meant to carry no comments
except a one-line reason on a regression test. Several explain non-obvious net/http behaviour (GET resend
on a reused connection), which is worth keeping as one such line. Fix as one cleanup pass.
