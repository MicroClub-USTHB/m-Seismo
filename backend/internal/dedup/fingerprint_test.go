package dedup

import (
	"testing"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

func crash(platform, typ, reason string, frames ...string) *ingest.Report {
	return &ingest.Report{Platform: platform, Release: &ingest.Release{Version: "2.4.0"}, Crash: &ingest.Crash{Type: typ, Reason: reason, StackTrace: frames}}
}

func TestSameBug(t *testing.T) {
	base := crash("android", "java.lang.IllegalStateException", "Fragment 4f2a not attached",
		"com.example.ProfileFragment.onResume(ProfileFragment.kt:88)",
		"androidx.fragment.app.Fragment.performResume(Fragment.java:3180)")

	same := map[string]*ingest.Report{
		"another version and line numbers": crash("android", "java.lang.IllegalStateException", "Fragment 9b1c not attached",
			"com.example.ProfileFragment.onResume(ProfileFragment.kt:91)",
			"androidx.fragment.app.Fragment.performResume(Fragment.java:3184)"),
		"no reason, same trace": crash("android", "java.lang.IllegalStateException", "",
			"com.example.ProfileFragment.onResume(ProfileFragment.kt:88)",
			"androidx.fragment.app.Fragment.performResume(Fragment.java:3180)"),
	}
	same["another version and line numbers"].Release.Version = "2.5.0"
	for name, r := range same {
		if Fingerprint(r) != Fingerprint(base) {
			t.Errorf("%s: not grouped with the original", name)
		}
	}

	different := map[string]*ingest.Report{
		"another frame": crash("android", "java.lang.IllegalStateException", "",
			"com.example.CartFragment.onResume(CartFragment.kt:88)",
			"androidx.fragment.app.Fragment.performResume(Fragment.java:3180)"),
		"another type": crash("android", "java.lang.NullPointerException", "",
			"com.example.ProfileFragment.onResume(ProfileFragment.kt:88)",
			"androidx.fragment.app.Fragment.performResume(Fragment.java:3180)"),
		"another platform": crash("ios", "java.lang.IllegalStateException", "",
			"com.example.ProfileFragment.onResume(ProfileFragment.kt:88)",
			"androidx.fragment.app.Fragment.performResume(Fragment.java:3180)"),
	}
	for name, r := range different {
		if Fingerprint(r) == Fingerprint(base) {
			t.Errorf("%s: grouped with a different bug", name)
		}
	}
}

func TestIOSFrames(t *testing.T) {
	a := crash("ios", "NSInvalidArgumentException", "", "3   MyApp   0x0000000104a2c3f4 -[CheckoutViewController pay:] + 120")
	b := crash("ios", "NSInvalidArgumentException", "", "4   MyApp   0x00000001029f81a0 -[CheckoutViewController pay:] + 96")
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("frame index, address and offset are not ignored")
	}
}

func TestWithoutTrace(t *testing.T) {
	a := crash("ios", "EXC_BAD_ACCESS", "address 0x10 in thread 7")
	b := crash("ios", "EXC_BAD_ACCESS", "address 0x7f in thread 3")
	c := crash("ios", "EXC_BAD_ACCESS", "stack overflow")
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("numbers in the reason are not ignored")
	}
	if Fingerprint(a) == Fingerprint(c) {
		t.Error("different reasons grouped")
	}
}

func TestNotGrouped(t *testing.T) {
	for name, r := range map[string]*ingest.Report{
		"no crash":      {Platform: "ios", UserInput: &ingest.UserInput{Title: "Pay does nothing"}},
		"no crash type": {Platform: "ios", Crash: &ingest.Crash{Type: " "}},
	} {
		if fp := Fingerprint(r); fp != "" {
			t.Errorf("%s: got fingerprint %q", name, fp)
		}
	}
}
