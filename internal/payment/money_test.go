// 本文件只覆盖金额精度与输入边界，避免支付金额在解析时被截断或溢出。
package payment

import "testing"

func TestParseMoney(t *testing.T) {
	for _, tc := range []struct {
		input string
		cents Money
		text  string
	}{
		{"0", 0, "0.00"}, {"0.01", 1, "0.01"}, {"1.2", 120, "1.20"},
		{"005.67", 567, "5.67"}, {"92233720368547758.07", 9223372036854775807, "92233720368547758.07"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseMoney(tc.input)
			if err != nil || got != tc.cents || got.String() != tc.text {
				t.Fatalf("ParseMoney(%q) = %d, %v; want %d / %s", tc.input, got, err, tc.cents, tc.text)
			}
		})
	}
	for _, input := range []string{"", "-1", "+1", " 1", "1 ", "1e2", ".1", "1.", "1.001", "1.2.3", "NaN", "１", "92233720368547758.08"} {
		t.Run("reject_"+input, func(t *testing.T) {
			if _, err := ParseMoney(input); err == nil {
				t.Fatalf("accepted invalid amount %q", input)
			}
		})
	}
}
