---
worth: yes
where: telegram_test.go:TestTelegram_SendMessageNotRepeatedOnLostAnswer
added: 2026-11-28
---
# telegram test comment refers to how sendText used to work

The "Request without a body" case carries `// without a body the parameters travel in the query, as
sendText sent them before`, a reference to the code before #47, where `sendText` sent a query-only
request. The same kind of comment was removed from `interface_test.go` in #47's round 2. Fix: delete the
line; the subtest name says what the case is.
