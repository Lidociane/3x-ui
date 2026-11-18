package panel

import (
	"errors"
	"testing"
)

// The fork serves no placements: a non-empty list here means the upstream feed
// was reintroduced and sponsor slots would render again.
func TestGetSponsorsIsEmptyOnThisFork(t *testing.T) {
	list, err := (&PanelService{}).GetSponsors()
	if err != nil {
		t.Fatalf("GetSponsors() error = %v, want nil", err)
	}
	if list == nil {
		t.Fatal("GetSponsors() = nil list, want an empty non-nil list")
	}
	if len(list.Sponsors) != 0 {
		t.Fatalf("GetSponsors() served %d sponsors, want 0", len(list.Sponsors))
	}
}

func TestGetSponsorLogoIsUnknownOnThisFork(t *testing.T) {
	data, contentType, err := (&PanelService{}).GetSponsorLogo("acme.png")
	if !errors.Is(err, ErrSponsorLogoUnknown) {
		t.Fatalf("GetSponsorLogo() error = %v, want ErrSponsorLogoUnknown", err)
	}
	if data != nil || contentType != "" {
		t.Fatalf("GetSponsorLogo() = (%v, %q), want (nil, \"\")", data, contentType)
	}
}
