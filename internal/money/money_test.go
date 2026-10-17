package money

import "testing"

func TestGrossUpExactness(t *testing.T) {
	for _, bps := range []int{0, 100, 150, 215, 300} {
		for n := int64(1); n < 5000; n += 7 {
			g := GrossUp(n, bps)
			if NetAfterFee(g, bps) < n {
				t.Fatalf("bps=%d n=%d g=%d: net %d < requested", bps, n, g, NetAfterFee(g, bps))
			}
			if g > n && NetAfterFee(g-1, bps) >= n {
				t.Fatalf("bps=%d n=%d g=%d is not minimal", bps, n, g)
			}
		}
	}
}

func TestGrossUpKnown(t *testing.T) {
	// NGN 2,000 net at 2.15% -> about NGN 2,044.
	g := GrossUp(2_000_00, 215)
	if g < 2_043_00 || g > 2_045_00 {
		t.Fatalf("got %d", g)
	}
	if NetAfterFee(g, 215) < 2_000_00 {
		t.Fatal("net too low")
	}
}

func TestTransferFeeTiers(t *testing.T) {
	cases := map[int64]int64{1_000_00: 10_00, 5_000_00: 10_00, 5_000_01: 25_00, 50_000_00: 25_00, 50_000_01: 50_00}
	for amt, want := range cases {
		if got := TransferFee(amt); got != want {
			t.Fatalf("amt %d: got %d want %d", amt, got, want)
		}
	}
}

func TestPINIssue(t *testing.T) {
	bad := []string{"12345", "1234567", "abcdef", "111111", "123456", "654321", "000000"}
	for _, p := range bad {
		if PINIssue(p) == "" {
			t.Fatalf("%q should be rejected", p)
		}
	}
	for _, p := range []string{"482916", "130579"} {
		if PINIssue(p) != "" {
			t.Fatalf("%q should be accepted", p)
		}
	}
}

func TestNamesMatch(t *testing.T) {
	yes := [][2]string{
		{"Joshua Olumoye", "OLUMOYE JOSHUA ADEBAYO"},
		{"joshua olumoye", "Mr. Joshua A. Olumoye"},
		{"Musa", "MUSA IBRAHIM"},
	}
	no := [][2]string{
		{"Joshua Olumoye", "ADEBAYO TUNDE"},
		{"Joshua Olumoye", "JOSHUA BELLO"},
		{"", "ANYONE"},
	}
	for _, c := range yes {
		if !NamesMatch(c[0], c[1]) {
			t.Fatalf("%v should match", c)
		}
	}
	for _, c := range no {
		if NamesMatch(c[0], c[1]) {
			t.Fatalf("%v should not match", c)
		}
	}
}
