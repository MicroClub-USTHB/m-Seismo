package dedup

import (
	"fmt"
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

// Frames as the iOS SDK sends them.
const (
	exceptionPreprocess = "0   CoreFoundation                      0x0000000192f0fa30 __exceptionPreprocess + 176"
	exceptionThrow      = "1   libobjc.A.dylib                     0x00000001929d2b90 objc_exception_throw + 88"
	arrayApply          = "2   CoreFoundation                      0x0000000192f380cc CFArrayApply + 0"
	start               = "9   dyld                                0x0000000192a12b98 start + 6076"
)

func TestSystemFramesDontCount(t *testing.T) {
	system := []string{exceptionPreprocess, exceptionThrow, arrayApply,
		"3   libsystem_c.dylib 0x0000000192b0a1f0 abort + 180", "4   libc++abi.dylib 0x0000000192c0a2f0 __cxxabiv1::__terminate + 20", "5   libobjc.A.dylib 0x0000000192c0b3f0 _objc_terminate + 96"}
	pay := crash("ios", "NSRangeException", "", append(system[:6:6], "6   ShopApp 0x0000000104802e74 $s7ShopApp8CheckoutC3payyyAA4CartVF + 72", start)...)
	login := crash("ios", "NSRangeException", "", append(system[:6:6], "6   ShopApp 0x0000000104802f80 $s7ShopApp5LoginC6submityyF + 64", start)...)
	if Fingerprint(pay) == Fingerprint(login) {
		t.Error("two bugs below the same system frames are grouped")
	}

	short := crash("ios", "NSRangeException", "", exceptionPreprocess, "1   ShopApp 0x0000000104802e74 $s7ShopApp8CheckoutC3payyyAA4CartVF + 72")
	if Fingerprint(pay) != Fingerprint(short) {
		t.Error("the number of system frames changes the fingerprint")
	}

	android := crash("android", "java.lang.IndexOutOfBoundsException", "",
		"at java.util.ArrayList.get(ArrayList.java:437)", "at com.shop.Checkout.pay(Checkout.kt:88)")
	shorter := crash("android", "java.lang.IndexOutOfBoundsException", "", "at com.shop.Checkout.pay(Checkout.kt:88)")
	if Fingerprint(android) != Fingerprint(shorter) {
		t.Error("android: system frames count")
	}
}

func TestSDKFramesDontCount(t *testing.T) {
	sdk := []string{
		"0   ShopApp 0x0000000100d474b8 $s7MSeismo13CrashReporterC11recordError_5fatalys0C0_p_SbtF + 120",
		"1   ShopApp 0x0000000100d5379c $s7MSeismoAAC11recordError_5fatalys0C0_p_SbtF + 88",
	}
	a := crash("ios", "ShopError", "", append(sdk[:2:2], "2   ShopApp 0x0000000100d98570 $s7ShopApp6OrdersC4syncyyF + 24")...)
	b := crash("ios", "ShopError", "", "0   ShopApp 0x0000000100d98570 $s7ShopApp6OrdersC4syncyyF + 24")
	if Fingerprint(a) != Fingerprint(b) {
		t.Error("frames of the SDK count")
	}
}

func TestOnlySystemFrames(t *testing.T) {
	a := crash("ios", "NSRangeException", "", exceptionPreprocess, exceptionThrow)
	b := crash("ios", "NSRangeException", "", exceptionPreprocess, arrayApply)
	if Fingerprint(a) == "" || Fingerprint(a) == Fingerprint(b) {
		t.Error("system frames are not used when there is nothing else")
	}
}

func TestStackWithoutNames(t *testing.T) {
	stripped := func(version, build string, offset int) *ingest.Report {
		r := crash("ios", "NSRangeException", "", exceptionPreprocess, fmt.Sprintf("3   ShopApp 0x000000010284f0ac ShopApp_main + %d", offset), "4   ShopApp 0x000000010284ebbc ShopApp_main + 1080", start)
		r.Release = &ingest.Release{Version: version, Build: build}
		return r
	}
	pay := Fingerprint(stripped("1.0", "42", 2344))
	if pay == "" {
		t.Fatal("no fingerprint")
	}

	again := stripped("1.0", "42", 2344)
	again.Crash.StackTrace[1] = "4   ShopApp 0x00000001002f90ac ShopApp_main + 2344"
	again.Crash.StackTrace[2] = "5   ShopApp 0x00000001002f8bbc ShopApp_main + 1080"
	if Fingerprint(again) != pay {
		t.Error("same crash on another run not grouped")
	}
	if Fingerprint(stripped("1.0", "42", 2484)) == pay {
		t.Error("two functions of one build grouped")
	}
	if Fingerprint(stripped("1.1", "42", 2344)) == pay {
		t.Error("offsets of another version grouped")
	}
	if Fingerprint(stripped("1.0", "43", 2344)) == pay {
		t.Error("offsets of another build grouped")
	}

	for name, frames := range map[string][]string{
		"module":    {"0   ShopApp 0x0000000100063bec ShopApp + 31724", "1   ShopApp 0x0000000100069c58 ShopApp + 56408"},
		"redacted":  {"3   ShopApp 0x0000000100063bec <redacted> + 124"},
		"no symbol": {"3   ShopApp 0x0000000100063bec 0x0 + 31724"},
	} {
		a, b := crash("ios", "E", "", frames...), crash("ios", "E", "", frames...)
		b.Release.Version = "9.9"
		if Fingerprint(a) == "" || Fingerprint(a) == Fingerprint(b) {
			t.Errorf("%s: not grouped on the build", name)
		}
	}

	noRelease := stripped("1.0", "42", 2344)
	noRelease.Release = nil
	if fp := Fingerprint(noRelease); fp != "" {
		t.Errorf("no release: got fingerprint %q", fp)
	}

	named := crash("ios", "NSRangeException", "", "3   ShopApp 0x0000000104802e74 $s7ShopApp8CheckoutC3payyyAA4CartVF + 72", "4   ShopApp 0x0000000104804e98 ShopApp_main + 1556")
	other := crash("ios", "NSRangeException", "", "3   ShopApp 0x0000000104802e74 $s7ShopApp8CheckoutC3payyyAA4CartVF + 96", "4   ShopApp 0x0000000104804e98 ShopApp_main + 1800")
	other.Release.Version = "9.9"
	if Fingerprint(named) != Fingerprint(other) {
		t.Error("a stack with names depends on the build")
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
	placeholder := crash("ios", "EXC_BAD_ACCESS", "address 0x10 in thread 7", noStackTrace)
	if Fingerprint(placeholder) != Fingerprint(a) {
		t.Error("the SDK's placeholder counts as a frame")
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
