package ap2core

import (
	"errors"
	"testing"
	"time"
)

func brl(cents int64) Amount { return Amount{Amount: cents, Currency: "BRL"} }

func mustAuth(t *testing.T, s *Signer, max Amount, merchants []string, now time.Time, ttl time.Duration) string {
	t.Helper()
	tok, err := SignSpendingAuthorization(s, max, merchants, "trusted-surface", NewNonce(), now, ttl)
	if err != nil {
		t.Fatalf("SignSpendingAuthorization: %v", err)
	}
	return tok
}

func TestSpendingAuthorizationRoundTrip(t *testing.T) {
	s, err := GenerateSigner("user-demo")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1777342370, 0)
	tok := mustAuth(t, s, brl(1000_00), []string{"merchant-demo"}, now, 30*24*time.Hour)

	got, err := VerifySpendingAuthorization(tok, &s.Key.PublicKey, "trusted-surface", now)
	if err != nil {
		t.Fatalf("VerifySpendingAuthorization: %v", err)
	}
	if got.VCT != VCTSpendingAuthorization {
		t.Errorf("vct = %q, want %q", got.VCT, VCTSpendingAuthorization)
	}
	if got.MaxPerPurchase != brl(1000_00) {
		t.Errorf("max_per_purchase = %+v, want 100000 BRL", got.MaxPerPurchase)
	}
	if got.EXP <= got.IAT {
		t.Errorf("exp %d must be after iat %d", got.EXP, got.IAT)
	}
}

// The whole point of the cap: the cheap recurring items go through, the
// R$3,000 appliance does not.
func TestSpendingAuthorizationPermitsByPrice(t *testing.T) {
	s, _ := GenerateSigner("user-demo")
	now := time.Unix(1777342370, 0)
	auth, err := VerifySpendingAuthorization(
		mustAuth(t, s, brl(1000_00), []string{"merchant-demo"}, now, 30*24*time.Hour),
		&s.Key.PublicKey, "trusted-surface", now)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		total Amount
		allow bool
	}{
		{"gas canister R$120", brl(120_00), true},
		{"grocery basket R$350", brl(350_00), true},
		{"exactly at the cap", brl(1000_00), true},
		{"one centavo over the cap", brl(1000_01), false},
		{"robovac R$2000", brl(2000_00), false},
		{"dishwasher R$3000", brl(3000_00), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := auth.Permits(tc.total, "merchant-demo", now)
			if tc.allow && err != nil {
				t.Fatalf("Permits(%d) = %v, want nil", tc.total.Amount, err)
			}
			if !tc.allow {
				if err == nil {
					t.Fatalf("Permits(%d) = nil, want a constraint error", tc.total.Amount)
				}
				var ce *ConstraintError
				if !errors.As(err, &ce) {
					t.Fatalf("Permits(%d) error is %T, want *ConstraintError", tc.total.Amount, err)
				}
				if ce.Code != ErrCodeUnresolvedConstraint {
					t.Errorf("code = %q, want %q", ce.Code, ErrCodeUnresolvedConstraint)
				}
			}
		})
	}
}

func TestSpendingAuthorizationConstraints(t *testing.T) {
	s, _ := GenerateSigner("user-demo")
	now := time.Unix(1777342370, 0)
	auth, err := VerifySpendingAuthorization(
		mustAuth(t, s, brl(1000_00), []string{"merchant-demo"}, now, 24*time.Hour),
		&s.Key.PublicKey, "trusted-surface", now)
	if err != nil {
		t.Fatal(err)
	}

	// Currency is never converted: BRL authorization, USD checkout.
	if err := auth.Permits(Amount{Amount: 100_00, Currency: "USD"}, "merchant-demo", now); err == nil {
		t.Error("a USD checkout must not be permitted by a BRL authorization")
	}
	// A merchant outside the list is refused even well under the cap.
	if err := auth.Permits(brl(10_00), "merchant-elsewhere", now); err == nil {
		t.Error("an unlisted merchant must not be permitted")
	}
	// Expiry is enforced at use time, not only at verification time.
	if err := auth.Permits(brl(10_00), "merchant-demo", now.Add(25*time.Hour)); err == nil {
		t.Error("an expired authorization must not permit a purchase")
	}
}

func TestSpendingAuthorizationRejectsTamperingAndMisuse(t *testing.T) {
	s, _ := GenerateSigner("user-demo")
	other, _ := GenerateSigner("attacker")
	now := time.Unix(1777342370, 0)
	tok := mustAuth(t, s, brl(1000_00), []string{"merchant-demo"}, now, 24*time.Hour)

	if _, err := VerifySpendingAuthorization(tok, &other.Key.PublicKey, "trusted-surface", now); err == nil {
		t.Error("a different key must not verify the authorization")
	}
	if _, err := VerifySpendingAuthorization(tok, &s.Key.PublicKey, "merchant", now); err == nil {
		t.Error("the wrong audience must be rejected")
	}
	if _, err := VerifySpendingAuthorization(tok, &s.Key.PublicKey, "trusted-surface", now.Add(25*time.Hour)); err == nil {
		t.Error("an expired authorization must not verify")
	}

	// A Spending Authorization must not be mistaken for a Checkout Mandate.
	if _, err := VerifyClosedCheckoutMandate(tok, &s.Key.PublicKey, "trusted-surface", "whatever"); err == nil {
		t.Error("a spending authorization must not verify as a checkout mandate")
	}
	// And a Checkout Mandate must not be usable as an authorization.
	cm, err := SignClosedCheckoutMandate(s, "some.checkout.jwt", "trusted-surface", NewNonce(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySpendingAuthorization(cm, &s.Key.PublicKey, "trusted-surface", now); err == nil {
		t.Error("a checkout mandate must not verify as a spending authorization")
	}
}

func TestSignSpendingAuthorizationRejectsBadInput(t *testing.T) {
	s, _ := GenerateSigner("user-demo")
	now := time.Unix(1777342370, 0)
	for _, tc := range []struct {
		name string
		max  Amount
		ttl  time.Duration
	}{
		{"zero cap", brl(0), time.Hour},
		{"negative cap", brl(-1), time.Hour},
		{"no currency", Amount{Amount: 100}, time.Hour},
		{"zero ttl", brl(100_00), 0},
		{"negative ttl", brl(100_00), -time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SignSpendingAuthorization(s, tc.max, nil, "trusted-surface", NewNonce(), now, tc.ttl); err == nil {
				t.Error("want an error, got nil")
			}
		})
	}
}
