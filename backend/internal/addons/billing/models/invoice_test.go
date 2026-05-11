package models

import (
	"math"
	"testing"
)

func TestInvoice_LifecycleGates(t *testing.T) {
	cases := []struct {
		status   InvoiceStatus
		edit     bool
		send     bool
		del      bool
	}{
		{StatusDraft, true, true, true},
		{StatusRejected, false, true, false},
		{StatusPending, false, false, false},
		{StatusSent, false, false, false},
		{StatusDelivered, false, false, false},
		{StatusAccepted, false, false, false},
		{StatusPaid, false, false, false},
		{StatusCancelled, false, false, false},
	}
	for _, c := range cases {
		inv := &Invoice{Status: c.status}
		if got := inv.CanBeEdited(); got != c.edit {
			t.Errorf("status=%q CanBeEdited()=%v want %v", c.status, got, c.edit)
		}
		if got := inv.CanBeSent(); got != c.send {
			t.Errorf("status=%q CanBeSent()=%v want %v", c.status, got, c.send)
		}
		if got := inv.CanBeDeleted(); got != c.del {
			t.Errorf("status=%q CanBeDeleted()=%v want %v", c.status, got, c.del)
		}
	}
}

func TestInvoice_DirectionPredicates(t *testing.T) {
	issued := &Invoice{Direction: DirectionIssued}
	received := &Invoice{Direction: DirectionReceived}
	if !issued.IsIssued() || issued.IsReceived() {
		t.Errorf("issued invoice: IsIssued=%v IsReceived=%v", issued.IsIssued(), issued.IsReceived())
	}
	if received.IsIssued() || !received.IsReceived() {
		t.Errorf("received invoice: IsIssued=%v IsReceived=%v", received.IsIssued(), received.IsReceived())
	}
}

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 0.005
}

func TestInvoice_CalculateTotals_SingleLine(t *testing.T) {
	inv := &Invoice{
		Lines: []InvoiceLine{
			{Quantity: 2, UnitPrice: 50, VATRate: 22},
		},
	}
	inv.CalculateTotals()

	if !approxEqual(inv.Lines[0].TotalPrice, 100.00) {
		t.Errorf("line TotalPrice = %v, want 100", inv.Lines[0].TotalPrice)
	}
	if !approxEqual(inv.Lines[0].VATAmount, 22.00) {
		t.Errorf("line VATAmount = %v, want 22", inv.Lines[0].VATAmount)
	}
	if !approxEqual(inv.TotalTaxableAmount, 100.00) {
		t.Errorf("TotalTaxableAmount = %v, want 100", inv.TotalTaxableAmount)
	}
	if !approxEqual(inv.TotalVATAmount, 22.00) {
		t.Errorf("TotalVATAmount = %v, want 22", inv.TotalVATAmount)
	}
	if !approxEqual(inv.TotalAmount, 122.00) {
		t.Errorf("TotalAmount = %v, want 122", inv.TotalAmount)
	}
}

func TestInvoice_CalculateTotals_PercentageDiscountSC(t *testing.T) {
	inv := &Invoice{
		Lines: []InvoiceLine{
			{Quantity: 1, UnitPrice: 200, VATRate: 22, Discounts: []LineDiscount{{Type: "SC", Percentage: 10}}},
		},
	}
	inv.CalculateTotals()
	// 200 - 10% = 180; VAT = 39.60
	if !approxEqual(inv.Lines[0].TotalPrice, 180.00) {
		t.Errorf("TotalPrice after SC%% = %v, want 180", inv.Lines[0].TotalPrice)
	}
	if !approxEqual(inv.Lines[0].VATAmount, 39.60) {
		t.Errorf("VATAmount = %v, want 39.60", inv.Lines[0].VATAmount)
	}
}

func TestInvoice_CalculateTotals_AmountDiscountSC(t *testing.T) {
	inv := &Invoice{
		Lines: []InvoiceLine{
			{Quantity: 1, UnitPrice: 200, VATRate: 22, Discounts: []LineDiscount{{Type: "SC", Amount: 50}}},
		},
	}
	inv.CalculateTotals()
	if !approxEqual(inv.Lines[0].TotalPrice, 150.00) {
		t.Errorf("TotalPrice after SC amount = %v, want 150", inv.Lines[0].TotalPrice)
	}
}

func TestInvoice_CalculateTotals_MaggiorazioneMG(t *testing.T) {
	inv := &Invoice{
		Lines: []InvoiceLine{
			{Quantity: 1, UnitPrice: 100, VATRate: 22, Discounts: []LineDiscount{{Type: "MG", Percentage: 10}}},
		},
	}
	inv.CalculateTotals()
	if !approxEqual(inv.Lines[0].TotalPrice, 110.00) {
		t.Errorf("TotalPrice after MG%% = %v, want 110", inv.Lines[0].TotalPrice)
	}

	inv2 := &Invoice{
		Lines: []InvoiceLine{
			{Quantity: 1, UnitPrice: 100, VATRate: 22, Discounts: []LineDiscount{{Type: "MG", Amount: 25}}},
		},
	}
	inv2.CalculateTotals()
	if !approxEqual(inv2.Lines[0].TotalPrice, 125.00) {
		t.Errorf("TotalPrice after MG amount = %v, want 125", inv2.Lines[0].TotalPrice)
	}
}

func TestInvoice_CalculateTotals_RoundingAdded(t *testing.T) {
	inv := &Invoice{
		Rounding: 0.05,
		Lines: []InvoiceLine{
			{Quantity: 1, UnitPrice: 99.95, VATRate: 22},
		},
	}
	inv.CalculateTotals()
	// total = 99.95 + 21.99 + 0.05 = 121.99
	if !approxEqual(inv.TotalAmount, 121.99) {
		t.Errorf("TotalAmount with rounding = %v, want 121.99", inv.TotalAmount)
	}
}

func TestInvoice_CalculateTotals_VATSummaryAggregatesByNatura(t *testing.T) {
	// When natura is set, the VAT key uses natura so two zero-rate lines
	// with the same natura aggregate into one VATSummary entry.
	inv := &Invoice{
		Lines: []InvoiceLine{
			{Quantity: 1, UnitPrice: 10, VATRate: 0, VATNature: VATNature("N3.5")},
			{Quantity: 2, UnitPrice: 15, VATRate: 0, VATNature: VATNature("N3.5")},
		},
	}
	inv.CalculateTotals()
	if len(inv.VATSummary) != 1 {
		t.Fatalf("expected 1 aggregated VATSummary entry, got %d (%v)", len(inv.VATSummary), inv.VATSummary)
	}
	if !approxEqual(inv.VATSummary[0].TaxableAmount, 40.00) {
		t.Errorf("aggregated TaxableAmount = %v, want 40", inv.VATSummary[0].TaxableAmount)
	}
	if !approxEqual(inv.VATSummary[0].VATAmount, 0.00) {
		t.Errorf("aggregated VATAmount = %v, want 0", inv.VATSummary[0].VATAmount)
	}
	if string(inv.VATSummary[0].VATNature) != "N3.5" {
		t.Errorf("VATNature = %q, want N3.5", inv.VATSummary[0].VATNature)
	}
}

func TestInvoice_CalculateTotals_MultipleLinesSumTotals(t *testing.T) {
	inv := &Invoice{
		Lines: []InvoiceLine{
			{Quantity: 1, UnitPrice: 100, VATRate: 22},
			{Quantity: 1, UnitPrice: 50, VATRate: 10},
		},
	}
	inv.CalculateTotals()
	if !approxEqual(inv.TotalTaxableAmount, 150.00) {
		t.Errorf("TotalTaxableAmount = %v, want 150", inv.TotalTaxableAmount)
	}
	if !approxEqual(inv.TotalVATAmount, 27.00) { // 22 + 5
		t.Errorf("TotalVATAmount = %v, want 27", inv.TotalVATAmount)
	}
	if !approxEqual(inv.TotalAmount, 177.00) {
		t.Errorf("TotalAmount = %v, want 177", inv.TotalAmount)
	}
}

func TestInvoice_CalculateTotals_EmptyLines(t *testing.T) {
	inv := &Invoice{}
	inv.CalculateTotals()
	if inv.TotalAmount != 0 || inv.TotalTaxableAmount != 0 || inv.TotalVATAmount != 0 {
		t.Errorf("empty invoice totals must be zero, got %+v", inv)
	}
	if inv.VATSummary == nil {
		t.Errorf("VATSummary should be initialized (empty slice), got nil")
	}
}

func TestRoundTo2Decimals(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{0, 0},
		{1.234, 1.23},
		{1.235, 1.24}, // standard banker-free half-up rounding via +0.5
		{1.999, 2.00},
		{-0, 0},
	}
	for _, c := range cases {
		if got := roundTo2Decimals(c.in); !approxEqual(got, c.want) {
			t.Errorf("roundTo2Decimals(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
