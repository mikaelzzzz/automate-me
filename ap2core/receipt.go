package ap2core

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ReceiptStatus per ap2/types/receipt_status.json — capitalised enum. The
// prose in agent_authorization.md says result:["success","error"], but every
// concrete schema, generated model and sample uses status:["Success","Error"];
// the schemas win (docs/research/ap2-v02-schema.md §7.1).
type ReceiptStatus string

const (
	StatusSuccess ReceiptStatus = "Success"
	StatusError   ReceiptStatus = "Error"
)

// CheckoutReceipt (checkout_receipt.json, merchant-signed). Success requires
// order_id; Error requires error + error_description. reference = hash of the
// closed Checkout Mandate, sd_hash manner.
type CheckoutReceipt struct {
	Status           ReceiptStatus `json:"status"`
	Iss              string        `json:"iss"`
	IAT              int64         `json:"iat"`
	Reference        string        `json:"reference"`
	OrderID          string        `json:"order_id,omitempty"`
	Error            string        `json:"error,omitempty"`
	ErrorDescription string        `json:"error_description,omitempty"`
}

// PaymentReceipt (payment_receipt.json, processor-signed). payment_id is
// required unconditionally — including on Error.
type PaymentReceipt struct {
	Status                ReceiptStatus `json:"status"`
	Iss                   string        `json:"iss"`
	IAT                   int64         `json:"iat"`
	Reference             string        `json:"reference"`
	PaymentID             string        `json:"payment_id"`
	PSPConfirmationID     string        `json:"psp_confirmation_id,omitempty"`
	NetworkConfirmationID string        `json:"network_confirmation_id,omitempty"`
	Error                 string        `json:"error,omitempty"`
	ErrorDescription      string        `json:"error_description,omitempty"`
}

func signReceipt(s *Signer, v any) (string, error) {
	var m map[string]any
	if err := reencode(v, &m); err != nil {
		return "", err
	}
	return signWithTyp(s, jwt.MapClaims(m), "receipt+jwt")
}

// NewCheckoutReceiptSuccess builds and signs an acceptance receipt for the
// given closed Checkout Mandate JWS.
func NewCheckoutReceiptSuccess(s *Signer, iss, mandateJWS, orderID string, now time.Time) (string, error) {
	if orderID == "" {
		return "", errors.New("checkout receipt: order_id required on Success")
	}
	return signReceipt(s, CheckoutReceipt{
		Status: StatusSuccess, Iss: iss, IAT: nowUnix(now),
		Reference: ReferenceHash(mandateJWS), OrderID: orderID,
	})
}

// NewCheckoutReceiptError builds and signs a rejection receipt. The merchant
// MUST return a receipt carrying the error on any failure
// (specification.md:316-317).
func NewCheckoutReceiptError(s *Signer, iss, mandateJWS, code, description string, now time.Time) (string, error) {
	if code == "" || description == "" {
		return "", errors.New("checkout receipt: error and error_description required on Error")
	}
	return signReceipt(s, CheckoutReceipt{
		Status: StatusError, Iss: iss, IAT: nowUnix(now),
		Reference: ReferenceHash(mandateJWS), Error: code, ErrorDescription: description,
	})
}

// NewPaymentReceiptSuccess builds and signs a settlement receipt.
func NewPaymentReceiptSuccess(s *Signer, iss, mandateJWS, paymentID, pspID, networkID string, now time.Time) (string, error) {
	if paymentID == "" || pspID == "" || networkID == "" {
		return "", errors.New("payment receipt: payment_id, psp_confirmation_id and network_confirmation_id required on Success")
	}
	return signReceipt(s, PaymentReceipt{
		Status: StatusSuccess, Iss: iss, IAT: nowUnix(now),
		Reference: ReferenceHash(mandateJWS), PaymentID: paymentID,
		PSPConfirmationID: pspID, NetworkConfirmationID: networkID,
	})
}

// NewPaymentReceiptError builds and signs a payment rejection receipt.
func NewPaymentReceiptError(s *Signer, iss, mandateJWS, paymentID, code, description string, now time.Time) (string, error) {
	if paymentID == "" {
		return "", errors.New("payment receipt: payment_id required even on Error")
	}
	if code == "" || description == "" {
		return "", errors.New("payment receipt: error and error_description required on Error")
	}
	return signReceipt(s, PaymentReceipt{
		Status: StatusError, Iss: iss, IAT: nowUnix(now),
		Reference: ReferenceHash(mandateJWS), PaymentID: paymentID,
		Error: code, ErrorDescription: description,
	})
}

// VerifyCheckoutReceipt verifies signature and the Success/Error field rules.
func VerifyCheckoutReceipt(token string, pub *ecdsa.PublicKey) (CheckoutReceipt, error) {
	claims, err := parseVerified(token, pub)
	if err != nil {
		return CheckoutReceipt{}, err
	}
	var r CheckoutReceipt
	if err := reencode(map[string]any(claims), &r); err != nil {
		return CheckoutReceipt{}, err
	}
	if r.Iss == "" || r.Reference == "" || r.IAT == 0 {
		return CheckoutReceipt{}, errors.New("checkout receipt: iss, iat and reference required")
	}
	switch r.Status {
	case StatusSuccess:
		if r.OrderID == "" {
			return CheckoutReceipt{}, errors.New("checkout receipt: order_id required on Success")
		}
	case StatusError:
		if r.Error == "" || r.ErrorDescription == "" {
			return CheckoutReceipt{}, errors.New("checkout receipt: error fields required on Error")
		}
	default:
		return CheckoutReceipt{}, fmt.Errorf("checkout receipt: invalid status %q", r.Status)
	}
	return r, nil
}

// VerifyPaymentReceipt verifies signature and the Success/Error field rules.
func VerifyPaymentReceipt(token string, pub *ecdsa.PublicKey) (PaymentReceipt, error) {
	claims, err := parseVerified(token, pub)
	if err != nil {
		return PaymentReceipt{}, err
	}
	var r PaymentReceipt
	if err := reencode(map[string]any(claims), &r); err != nil {
		return PaymentReceipt{}, err
	}
	if r.Iss == "" || r.Reference == "" || r.IAT == 0 || r.PaymentID == "" {
		return PaymentReceipt{}, errors.New("payment receipt: iss, iat, reference and payment_id required")
	}
	switch r.Status {
	case StatusSuccess:
		if r.PSPConfirmationID == "" || r.NetworkConfirmationID == "" {
			return PaymentReceipt{}, errors.New("payment receipt: confirmation ids required on Success")
		}
	case StatusError:
		if r.Error == "" || r.ErrorDescription == "" {
			return PaymentReceipt{}, errors.New("payment receipt: error fields required on Error")
		}
	default:
		return PaymentReceipt{}, fmt.Errorf("payment receipt: invalid status %q", r.Status)
	}
	return r, nil
}
