package identity

import "testing"

func TestDeviceNameTellsBrowsersApart(t *testing.T) {
	chrome := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"
	edge := chrome + " Edg/130.0"
	firefox := "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:131.0) Gecko/20100101 Firefox/131.0"
	cases := map[string]string{
		chrome:               "Chrome on Windows",
		edge:                 "Edge on Windows",
		firefox:              "Firefox on Windows",
		"DocvetaAndroid/0.1": "the Android app",
		"":                   "an unknown device",
		"curl/8.4.0":         "curl/8.4.0",
	}
	for ua, want := range cases {
		if got := deviceName(ua); got != want {
			t.Errorf("deviceName(%q) = %q, want %q", ua, got, want)
		}
	}
}
