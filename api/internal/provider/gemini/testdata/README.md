Responses replayed by `gemini_test.go`. They follow the shapes documented for
OpenRouter's Images API (`POST /api/v1/images`) and error handling at
https://openrouter.ai/docs as of November 2026.

To check them against the live API, run the opt-in live test (costs money):

    AIFS_OPENROUTER_API_KEY=... go test -tags live -run TestLive -v ./internal/provider/gemini/
