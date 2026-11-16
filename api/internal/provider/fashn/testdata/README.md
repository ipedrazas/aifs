Responses replayed by `fashn_test.go`. They follow the shapes documented at
https://docs.fashn.ai (run, status, error handling) as of November 2026.
`{{CDN}}` is replaced with the test server's URL.

To check them against the live API, run the opt-in live test (costs credits):

    AIFS_FASHN_API_KEY=... go test -tags live -run TestLive ./internal/provider/fashn/
