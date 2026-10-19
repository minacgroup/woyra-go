package woyra_test

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	woyra "github.com/minacgroup/woyra-go"
)

func ExampleEmailsService_Send() {
	client := woyra.NewClient(os.Getenv("WOYRA_API_KEY"))
	email, err := client.Emails.Send(context.Background(), &woyra.SendEmailParams{
		From:    "Woyra <noreply@yourdomain.com>",
		To:      woyra.Recipients{"abebe@example.com"},
		Subject: "Your code",
		Text:    "Your code is 483920",
	})
	var apiErr *woyra.Error
	if errors.As(err, &apiErr) {
		log.Fatalf("%s (%s): %s", apiErr.Code, apiErr.RequestID, apiErr.Message)
	}
	log.Println(email.ID)
}

func ExampleEmailsService_Send_template() {
	client := woyra.NewClient("")
	_, _ = client.Emails.Send(context.Background(), &woyra.SendEmailParams{
		From:       "noreply@yourdomain.com",
		To:         woyra.Recipients{"abebe@example.com"},
		TemplateID: "tmpl_01K...",
		Variables:  map[string]string{"name": "Abebe"},
		TrackOpens: woyra.Bool(true),
	}, woyra.WithIdempotencyKey("welcome-u_9912"))
}

func ExampleEmailsService_Cancel() {
	client := woyra.NewClient("")
	ctx := context.Background()
	email, err := client.Emails.Send(ctx, &woyra.SendEmailParams{
		From: "noreply@yourdomain.com", To: woyra.Recipients{"abebe@example.com"},
		Subject: "Reminder", Text: "See you tomorrow",
		ScheduledAt: woyra.Time(time.Now().Add(24 * time.Hour)),
	})
	if err != nil {
		log.Fatal(err)
	}
	_, _ = client.Emails.Cancel(ctx, email.ID)
}

func ExampleVerifyWebhook() {
	http.HandleFunc("/hooks/woyra", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		event, err := woyra.VerifyWebhook(body, r.Header, os.Getenv("WOYRA_WEBHOOK_SECRET"))
		if err != nil {
			http.Error(w, "invalid signature", http.StatusBadRequest)
			return
		}
		log.Println(event.Type, event.ID)
		w.WriteHeader(http.StatusNoContent)
	})
}
