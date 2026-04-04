package notification

import (
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"sync"
	"text/template"
	"time"
)

// EventType classifies notification events.
type EventType string

const (
	EventWelcome          EventType = "welcome"
	EventUsageWarning80   EventType = "usage_warning_80"
	EventUsageWarning90   EventType = "usage_warning_90"
	EventSubExpiring7d    EventType = "subscription_expiring_7d"
	EventSubExpiring3d    EventType = "subscription_expiring_3d"
	EventSubExpiring1d    EventType = "subscription_expiring_1d"
	EventSubExpired       EventType = "subscription_expired"
	EventKeyRevoked       EventType = "key_revoked"
	EventPaymentFailed    EventType = "payment_failed"
	EventPaymentSucceeded EventType = "payment_succeeded"
	EventDeviceLimitHit   EventType = "device_limit_reached"
	EventMarketingUpdate  EventType = "marketing_update"
)

// NotifierConfig holds SMTP and sending configuration.
type NotifierConfig struct {
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	FromEmail    string
	FromName     string
	Enabled      bool
}

// Notification represents a pending notification to be sent.
type Notification struct {
	Event   EventType
	Email   string
	Subject string
	Body    string
	Data    map[string]string
}

// Notifier manages sending notifications via email or logging.
type Notifier struct {
	cfg       NotifierConfig
	logger    *slog.Logger
	eventLog  *EventLog
	queue     chan Notification
	stopCh    chan struct{}
	wg        sync.WaitGroup
	templates map[EventType]*template.Template
}

// NewNotifier creates a new notification system.
func NewNotifier(cfg NotifierConfig, eventLog *EventLog, logger *slog.Logger) *Notifier {
	n := &Notifier{
		cfg:      cfg,
		logger:   logger,
		eventLog: eventLog,
		queue:    make(chan Notification, 500),
		stopCh:   make(chan struct{}),
	}
	n.templates = n.buildTemplates()
	n.startWorker()
	return n
}

// SendNotification queues a notification for async delivery.
func (n *Notifier) SendNotification(event EventType, email string, data map[string]string) {
	subject, body := n.renderTemplate(event, data)
	notif := Notification{
		Event:   event,
		Email:   email,
		Subject: subject,
		Body:    body,
		Data:    data,
	}

	select {
	case n.queue <- notif:
	default:
		n.logger.Warn("notification queue full, dropping", "event", event, "email", email)
	}
}

// SendBulkMarketing sends a marketing email to multiple recipients.
func (n *Notifier) SendBulkMarketing(subject, body string, emails []string) {
	for _, email := range emails {
		notif := Notification{
			Event:   EventMarketingUpdate,
			Email:   email,
			Subject: subject,
			Body:    body,
			Data:    map[string]string{"subject": subject},
		}
		select {
		case n.queue <- notif:
		default:
			n.logger.Warn("notification queue full during bulk send", "email", email)
		}
	}
}

// Stop shuts down the notifier worker gracefully.
func (n *Notifier) Stop() {
	close(n.stopCh)
	n.wg.Wait()
}

func (n *Notifier) startWorker() {
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		for {
			select {
			case notif := <-n.queue:
				n.deliver(notif)
			case <-n.stopCh:
				// Drain remaining
				for {
					select {
					case notif := <-n.queue:
						n.deliver(notif)
					default:
						return
					}
				}
			}
		}
	}()
}

func (n *Notifier) deliver(notif Notification) {
	status := "sent"

	if !n.cfg.Enabled || n.cfg.SMTPHost == "" {
		// Log mode — no SMTP configured
		n.logger.Info("notification (log mode)",
			"event", notif.Event,
			"email", notif.Email,
			"subject", notif.Subject,
		)
		status = "logged"
	} else {
		if err := n.sendEmail(notif.Email, notif.Subject, notif.Body); err != nil {
			n.logger.Error("failed to send notification",
				"event", notif.Event,
				"email", notif.Email,
				"error", err,
			)
			status = "failed"
		}
	}

	if n.eventLog != nil {
		n.eventLog.Record(EventLogEntry{
			ID:        fmt.Sprintf("evt_%d", time.Now().UnixNano()),
			EventType: notif.Event,
			Email:     notif.Email,
			Data:      notif.Data,
			SentAt:    time.Now(),
			Status:    status,
		})
	}
}

func (n *Notifier) sendEmail(to, subject, body string) error {
	from := n.cfg.FromEmail
	if n.cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", n.cfg.FromName, n.cfg.FromEmail)
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		from, to, subject, body)

	addr := fmt.Sprintf("%s:%d", n.cfg.SMTPHost, n.cfg.SMTPPort)

	var auth smtp.Auth
	if n.cfg.SMTPUser != "" {
		auth = smtp.PlainAuth("", n.cfg.SMTPUser, n.cfg.SMTPPassword, n.cfg.SMTPHost)
	}

	return smtp.SendMail(addr, auth, n.cfg.FromEmail, []string{to}, []byte(msg))
}

// RenderTemplate renders a notification template for external use (e.g., tests).
func (n *Notifier) RenderTemplate(event EventType, data map[string]string) (string, string) {
	return n.renderTemplate(event, data)
}

func (n *Notifier) renderTemplate(event EventType, data map[string]string) (string, string) {
	tmpl, ok := n.templates[event]
	if !ok {
		return string(event), fmt.Sprintf("Event: %s", event)
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return string(event), fmt.Sprintf("Event: %s (template error: %v)", event, err)
	}

	rendered := buf.String()
	// First line is subject, rest is body
	parts := strings.SplitN(rendered, "\n", 2)
	subject := parts[0]
	body := ""
	if len(parts) > 1 {
		body = parts[1]
	}
	return subject, body
}

func (n *Notifier) buildTemplates() map[EventType]*template.Template {
	defs := map[EventType]string{
		EventWelcome: `Welcome to Nexus Gateway!
<h2>Welcome to Nexus Gateway, {{.name}}!</h2>
<p>Your account has been created with the <strong>{{.plan}}</strong> plan.</p>
<p>Get started by generating an API key from your dashboard.</p>`,

		EventUsageWarning80: `Usage Alert: 80% of quota used
<h2>Usage Warning</h2>
<p>You've used <strong>80%</strong> of your monthly request quota.</p>
<p>Current usage: {{.usage}} / {{.limit}} requests.</p>
<p>Consider upgrading your plan to avoid interruptions.</p>`,

		EventUsageWarning90: `Usage Alert: 90% of quota used
<h2>Usage Warning — Critical</h2>
<p>You've used <strong>90%</strong> of your monthly request quota.</p>
<p>Current usage: {{.usage}} / {{.limit}} requests.</p>
<p>Upgrade now to avoid service interruption.</p>`,

		EventSubExpiring7d: `Your Nexus subscription expires in 7 days
<h2>Subscription Expiring Soon</h2>
<p>Your <strong>{{.plan}}</strong> subscription will expire on {{.expires}}.</p>
<p>Renew now to maintain uninterrupted access.</p>`,

		EventSubExpiring3d: `Your Nexus subscription expires in 3 days
<h2>Subscription Expiring Soon</h2>
<p>Your <strong>{{.plan}}</strong> subscription will expire on {{.expires}}.</p>
<p>Please renew to avoid losing access.</p>`,

		EventSubExpiring1d: `Your Nexus subscription expires tomorrow
<h2>Subscription Expiring Tomorrow</h2>
<p>Your <strong>{{.plan}}</strong> subscription expires on {{.expires}}.</p>
<p>Renew immediately to keep your API keys active.</p>`,

		EventSubExpired: `Your Nexus subscription has expired
<h2>Subscription Expired</h2>
<p>Your <strong>{{.plan}}</strong> subscription has expired.</p>
<p>Your API keys have been deactivated. Resubscribe to restore access.</p>`,

		EventKeyRevoked: `API Key Revoked
<h2>API Key Revoked</h2>
<p>An API key ({{.key_prefix}}...) has been revoked.</p>
<p>If you did not request this, please contact support immediately.</p>`,

		EventPaymentFailed: `Payment Failed — Action Required
<h2>Payment Failed</h2>
<p>We were unable to process your payment for the <strong>{{.plan}}</strong> plan.</p>
<p>Please update your payment method to avoid service interruption.</p>`,

		EventPaymentSucceeded: `Payment Received — Thank You
<h2>Payment Confirmed</h2>
<p>Your payment for the <strong>{{.plan}}</strong> plan has been received.</p>
<p>Your monthly usage counters have been reset.</p>`,

		EventDeviceLimitHit: `Device Limit Reached
<h2>Device Limit Reached</h2>
<p>You've reached the maximum number of devices ({{.max}}) for your <strong>{{.plan}}</strong> plan.</p>
<p>Currently active: {{.count}} devices. Upgrade your plan for more devices.</p>`,

		EventMarketingUpdate: `{{.subject}}
{{.body}}`,
	}

	templates := make(map[EventType]*template.Template, len(defs))
	for event, tmplStr := range defs {
		t, err := template.New(string(event)).Parse(tmplStr)
		if err != nil {
			continue
		}
		templates[event] = t
	}
	return templates
}
