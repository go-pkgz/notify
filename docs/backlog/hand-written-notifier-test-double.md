---
worth: no
where: interface_test.go:schemeNotifier
added: 2026-11-28
---
# hand-written Notifier test double

`schemeNotifier` in `interface_test.go` (added in #47) is a hand-written double of `Notifier`, where mocks
are meant to be generated with moq. The obvious fix, `//go:generate moq ... . Notifier` into a `mocks`
subpackage, publishes a `mocks` package that every importer of this library sees, for a three-method stub
with no call tracking. Not worth it unless the package gains other generated mocks.
