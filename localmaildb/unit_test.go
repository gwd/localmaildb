package localmaildb

import (
	"net/mail"
	"testing"
)

func TestAddressParse(t *testing.T) {
	tests := []struct {
		in   mail.Address
		want Address
	}{
		{mail.Address{"", "foo@bar.com"},
			Address{"", "foo", "bar.com"}},
	}

	for _, test := range tests {
		var got Address
		err := convertAddress(&test.in, &got)
		if err != nil {
			t.Errorf("ERROR converting %v: %v", test.in, err)
			continue
		}
		if got != test.want {
			t.Errorf("ERROR: Converting %v: got %v, wanted %v!",
				test.in, got, test.want)
		}
	}
}
