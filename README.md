# woyra-go

The official Go SDK for the [Woyra](https://woyra.io) email API.

- Go 1.24+, standard library only.
- Retries network errors, `429` and `5xx` automatically: up to 3 retries with exponential backoff and jitter, honouring `Retry-After`. Other `4xx` errors are never retried. Every call takes a `context.Context`, which also bounds retries.
- Sends get an `Idempotency-Key` automatically, and every retry reuses it, so a retried send never becomes a second email.
- Includes a webhook signature verifier.

## Install

```sh
go get github.com/minacgroup/woyra-go@latest
```

```go
import woyra "github.com/minacgroup/woyra-go"
```

## Quickstart

```go
client := woyra.NewClient(os.Getenv("WOYRA_API_KEY")) // "" also reads WOYRA_API_KEY

email, err := client.Emails.Send(ctx, &woyra.SendEmailParams{
	From:    "Acme <noreply@yourdomain.com>",
	To:      woyra.Recipients{"abebe@example.com"},
	Subject: "Your code",
	Text:    "Your code is 483920",
})
if err != nil {
	log.Fatal(err)
}
fmt.Println(email.ID, email.Status)
```

`To`, `Cc`, `Bcc` and `ReplyTo` are `woyra.Recipients`. One address is sent as a JSON string and several as an array; the API accepts either. `Name <address>` is accepted.

### Options

```go
client := woyra.NewClient("wk_live_...",
	woyra.WithBaseURL("https://kenya.woyra.io"), // default https://api.woyra.io (or WOYRA_BASE_URL)
	woyra.WithMaxRetries(3),                     // retries after the first attempt
	woyra.WithTimeout(30*time.Second),           // per attempt
	woyra.WithAPIVersion("2026-09-01"),          // sent as Woyra-Version
	// woyra.WithHTTPClient(myClient),
)
```

## Send with a template

```go
tmpl, err := client.Templates.Create(ctx, &woyra.CreateTemplateParams{
	Name:    "welcome",
	Subject: "Welcome, {{name}}",
	HTML:    "<p>Hi {{name}}, your plan is {{plan}}.</p>",
})
fmt.Println(tmpl.Variables) // [name plan]

_, err = client.Emails.Send(ctx, &woyra.SendEmailParams{
	From:        "noreply@yourdomain.com",
	To:          woyra.Recipients{"abebe@example.com"},
	TemplateID:  tmpl.ID,
	Variables:   map[string]string{"name": "Abebe", "plan": "Pro"},
	TrackOpens:  woyra.Bool(true),
	TrackClicks: woyra.Bool(true),
})
```

Templates also have `Get`, `List`, `Update` (fields are pointers: `&woyra.UpdateTemplateParams{Subject: woyra.String("...")}`) and `Remove`.

## Batch

Up to 100 emails in one request, all or nothing, under one idempotency key:

```go
res, err := client.Emails.Batch(ctx, []woyra.SendEmailParams{
	{From: "noreply@yourdomain.com", To: woyra.Recipients{"a@example.com"}, Subject: "Digest", Text: "..."},
	{From: "noreply@yourdomain.com", To: woyra.Recipients{"b@example.com", "c@example.com"}, Subject: "Digest", Text: "..."},
})
for _, e := range res.Data {
	fmt.Println(e.ID)
}
```

## Schedule and cancel

```go
scheduled, err := client.Emails.Send(ctx, &woyra.SendEmailParams{
	From:        "noreply@yourdomain.com",
	To:          woyra.Recipients{"abebe@example.com"},
	Subject:     "Reminder",
	Text:        "Your appointment is tomorrow.",
	ScheduledAt: woyra.Time(time.Now().Add(24 * time.Hour)),
})
if err != nil {
	log.Fatal(err)
}
_, err = client.Emails.Cancel(ctx, scheduled.ID)
```

`Emails.Get` returns an email with every status it has had; `Emails.List(ctx, &woyra.ListParams{Limit: 25, Cursor: page.NextCursor})` pages through them.

## Idempotency

A key is generated for you on every `POST`. To make a send safe to repeat across process restarts, derive the key from what the send is about:

```go
client.Emails.Send(ctx, params, woyra.WithIdempotencyKey("order-4821-receipt"))
```

## Domains and webhook endpoints

```go
domain, err := client.Domains.Create(ctx, "yourdomain.com")
fmt.Println(domain.Records) // DNS records to publish
client.Domains.Verify(ctx, domain.ID)

endpoint, err := client.Webhooks.Create(ctx, &woyra.CreateWebhookEndpointParams{
	URL:    "https://api.yourapp.com/hooks/woyra",
	Events: []string{woyra.EventEmailDelivered, woyra.EventEmailOpened, woyra.EventEmailClicked},
})
fmt.Println(endpoint.Secret) // shown once: store it
```

Also `Domains.List`, `Domains.Remove`, `Webhooks.List` and `Webhooks.Remove`.

## Verify webhooks

Verify the **raw** body. `VerifyWebhook` checks the HMAC-SHA256 signature in constant time, rejects timestamps more than 5 minutes away, and returns the decoded event:

```go
http.HandleFunc("/hooks/woyra", func(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	event, err := woyra.VerifyWebhook(body, r.Header, os.Getenv("WOYRA_WEBHOOK_SECRET"))
	if err != nil { // errors.Is(err, woyra.ErrInvalidSignature), woyra.ErrSignatureExpired, ...
		http.Error(w, "invalid signature", http.StatusBadRequest)
		return
	}
	if event.Type == woyra.EventEmailDelivered {
		// event.Data["email_id"] ...
	}
	w.WriteHeader(http.StatusNoContent)
})
```

`client.Webhooks.Verify` is the same function. `woyra.VerifyTolerance(d)` changes the window. Deliveries are at-least-once: deduplicate on `event.ID`.

## Errors

API failures are `*woyra.Error`:

```go
_, err := client.Emails.Send(ctx, &woyra.SendEmailParams{To: woyra.Recipients{"not-an-address"}})
var apiErr *woyra.Error
if errors.As(err, &apiErr) {
	fmt.Println(apiErr.StatusCode) // 400
	fmt.Println(apiErr.Code)       // "invalid_request"  (branch on this)
	fmt.Println(apiErr.Message)    // "\"not-an-address\" is not a valid email address."
	fmt.Println(apiErr.Param)      // "to"
	fmt.Println(apiErr.RequestID)  // "req_..." (quote it to support)
	fmt.Println(apiErr.DocURL)
}
```

`Code == "connection_error"` with `StatusCode == 0` means no response arrived after every retry; `Unwrap` gives the network error. A cancelled context returns the context's error.

## Development

```sh
cd sdks/go
GOWORK=off go vet ./...
GOWORK=off go test ./...
```

`GOWORK=off` is needed inside this repository, because its root `go.work` lists only the backend module. Users who `go get` the module are unaffected.

## License

MIT. Copyright (c) 2026 MINAC GROUP LLC.
