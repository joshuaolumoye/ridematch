// Package money holds pure kobo arithmetic: top-up fee gross-up, payout fee
// tiers, PIN strength and payout-account name matching. Everything here is
// deterministic and unit-tested; no I/O.
package money

import (
	"strings"
	"unicode"
)

// CollectionFee is the fee charged on a payment of `paidKobo` at `bps`
// basis points, rounded UP to the next kobo (never undercharge).
func CollectionFee(paidKobo int64, bps int) int64 {
	if paidKobo <= 0 || bps <= 0 {
		return 0
	}
	return (paidKobo*int64(bps) + 9999) / 10000
}

// NetAfterFee is what a wallet is credited for a payment of paidKobo.
func NetAfterFee(paidKobo int64, bps int) int64 {
	return paidKobo - CollectionFee(paidKobo, bps)
}

// GrossUp returns the smallest amount G such that NetAfterFee(G) >= netKobo,
// i.e. what a payer must transfer for the wallet to receive netKobo.
func GrossUp(netKobo int64, bps int) int64 {
	if netKobo <= 0 {
		return 0
	}
	if bps <= 0 {
		return netKobo
	}
	g := (netKobo*10000 + int64(10000-bps) - 1) / int64(10000-bps)
	// Rounding in CollectionFee can make the first estimate off by a kobo
	// or two in either direction; walk to the exact minimum.
	for g > netKobo && NetAfterFee(g-1, bps) >= netKobo {
		g--
	}
	for NetAfterFee(g, bps) < netKobo {
		g++
	}
	return g
}

// TransferFee is Flutterwave's payout fee tier for an amount (kobo):
// NGN 10 up to NGN 5,000; NGN 25 up to NGN 50,000; NGN 50 above.
func TransferFee(amountKobo int64) int64 {
	switch {
	case amountKobo <= 5_000_00:
		return 10_00
	case amountKobo <= 50_000_00:
		return 25_00
	default:
		return 50_00
	}
}

// PINIssue returns a human-readable reason a PIN is unacceptable, or "".
// A PIN is exactly six digits and may not be trivially guessable.
func PINIssue(pin string) string {
	if len(pin) != 6 {
		return "PIN must be exactly 6 digits"
	}
	for _, r := range pin {
		if r < '0' || r > '9' {
			return "PIN must contain digits only"
		}
	}
	same, asc, desc := true, true, true
	for i := 1; i < 6; i++ {
		if pin[i] != pin[0] {
			same = false
		}
		if pin[i] != pin[i-1]+1 {
			asc = false
		}
		if pin[i] != pin[i-1]-1 {
			desc = false
		}
	}
	if same || asc || desc {
		return "PIN is too easy to guess — avoid repeated or sequential digits"
	}
	return ""
}

var nameNoise = map[string]bool{
	"MR": true, "MRS": true, "MISS": true, "MS": true, "DR": true, "CHIEF": true, "ALHAJI": true,
	"ALHAJA": true, "PASTOR": true, "ENGR": true, "THE": true, "OF": true, "AND": true,
}

func nameTokens(s string) []string {
	f := func(r rune) bool { return !unicode.IsLetter(r) }
	var out []string
	for _, t := range strings.FieldsFunc(strings.ToUpper(s), f) {
		if len(t) > 1 && !nameNoise[t] {
			out = append(out, t)
		}
	}
	return out
}

// NamesMatch reports whether a bank account holder name plausibly belongs to
// the same person as the driver's registered name: at least two name parts
// in common (order-insensitive), or, for a single-part registered name, that
// part appears in the account name.
func NamesMatch(registered, accountName string) bool {
	reg, acc := nameTokens(registered), nameTokens(accountName)
	if len(reg) == 0 || len(acc) == 0 {
		return false
	}
	set := map[string]bool{}
	for _, t := range acc {
		set[t] = true
	}
	common := 0
	for _, t := range reg {
		if set[t] {
			common++
		}
	}
	if len(reg) == 1 {
		return common == 1
	}
	return common >= 2
}
