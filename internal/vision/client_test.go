package vision

import "testing"

func TestLooksLikeCaptchaQuestion(t *testing.T) {
	if !looksLikeCaptchaQuestion("recognize the captcha, only output characters") {
		t.Fatal("expected captcha hint")
	}
	if looksLikeCaptchaQuestion("describe the login page layout") {
		t.Fatal("expected non-captcha")
	}
}
