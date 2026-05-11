package models

import (
	"strings"
	"testing"
)

func TestSDINotification_GetStatusDescription(t *testing.T) {
	cases := []struct {
		name    string
		n       SDINotification
		wantSub string
	}{
		{"RC", SDINotification{NotificationType: NotificationRC}, "consegnata"},
		{"NS", SDINotification{NotificationType: NotificationNS, ErrorDescription: "tag missing"}, "scartata"},
		{"NS includes error", SDINotification{NotificationType: NotificationNS, ErrorDescription: "tag missing"}, "tag missing"},
		{"MC", SDINotification{NotificationType: NotificationMC, MCDescription: "unreachable"}, "Mancata consegna"},
		{"MC includes detail", SDINotification{NotificationType: NotificationMC, MCDescription: "unreachable"}, "unreachable"},
		{"NE accepted", SDINotification{NotificationType: NotificationNE, Outcome: OutcomeAccepted}, "accettata"},
		{"NE rejected", SDINotification{NotificationType: NotificationNE, Outcome: OutcomeRejected, OutcomeReason: "bad doc"}, "rifiutata"},
		{"NE rejected includes reason", SDINotification{NotificationType: NotificationNE, Outcome: OutcomeRejected, OutcomeReason: "bad doc"}, "bad doc"},
		{"DT", SDINotification{NotificationType: NotificationDT}, "Decorrenza"},
		{"AT", SDINotification{NotificationType: NotificationAT}, "Attestazione"},
		{"unknown", SDINotification{NotificationType: NotificationType("XX")}, "Notifica ricevuta"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.n.GetStatusDescription()
			if !strings.Contains(got, c.wantSub) {
				t.Errorf("GetStatusDescription() = %q, want substring %q", got, c.wantSub)
			}
		})
	}
}

func TestSDINotification_IsPositive(t *testing.T) {
	cases := []struct {
		n    SDINotification
		want bool
	}{
		{SDINotification{NotificationType: NotificationRC}, true},
		{SDINotification{NotificationType: NotificationDT}, true},
		{SDINotification{NotificationType: NotificationNE, Outcome: OutcomeAccepted}, true},
		{SDINotification{NotificationType: NotificationNE, Outcome: OutcomeRejected}, false},
		{SDINotification{NotificationType: NotificationNS}, false},
		{SDINotification{NotificationType: NotificationMC}, false},
		{SDINotification{NotificationType: NotificationAT}, false},
	}
	for _, c := range cases {
		if got := c.n.IsPositive(); got != c.want {
			t.Errorf("IsPositive(%q, outcome=%q) = %v, want %v", c.n.NotificationType, c.n.Outcome, got, c.want)
		}
	}
}

func TestSDINotification_IsNegative(t *testing.T) {
	cases := []struct {
		n    SDINotification
		want bool
	}{
		{SDINotification{NotificationType: NotificationNS}, true},
		{SDINotification{NotificationType: NotificationNE, Outcome: OutcomeRejected}, true},
		{SDINotification{NotificationType: NotificationNE, Outcome: OutcomeAccepted}, false},
		{SDINotification{NotificationType: NotificationRC}, false},
		{SDINotification{NotificationType: NotificationMC}, false},
		{SDINotification{NotificationType: NotificationDT}, false},
	}
	for _, c := range cases {
		if got := c.n.IsNegative(); got != c.want {
			t.Errorf("IsNegative(%q, outcome=%q) = %v, want %v", c.n.NotificationType, c.n.Outcome, got, c.want)
		}
	}
}

func TestSDINotification_RequiresAction(t *testing.T) {
	cases := []struct {
		n    SDINotification
		want bool
	}{
		{SDINotification{NotificationType: NotificationNS}, true},
		{SDINotification{NotificationType: NotificationMC}, true},
		{SDINotification{NotificationType: NotificationNE, Outcome: OutcomeRejected}, true},
		{SDINotification{NotificationType: NotificationNE, Outcome: OutcomeAccepted}, false},
		{SDINotification{NotificationType: NotificationRC}, false},
		{SDINotification{NotificationType: NotificationDT}, false},
		{SDINotification{NotificationType: NotificationAT}, false},
	}
	for _, c := range cases {
		if got := c.n.RequiresAction(); got != c.want {
			t.Errorf("RequiresAction(%q) = %v, want %v", c.n.NotificationType, got, c.want)
		}
	}
}
