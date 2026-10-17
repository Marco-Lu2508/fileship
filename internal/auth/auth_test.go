package auth

import (
	"strings"
	"testing"
	"time"
)

func TestAccessAndPendingTokens(t *testing.T) {
	secret := "test-jwt-secret"

	access, err := GenerateAccessToken(secret, 42, true)
	if err != nil {
		t.Fatalf("GenerateAccessToken failed: %v", err)
	}
	claims, err := ParseAccessToken(secret, access)
	if err != nil {
		t.Fatalf("ParseAccessToken failed: %v", err)
	}
	if claims.UserID != 42 || !claims.IsAdmin || claims.TwoFAPending {
		t.Fatalf("unexpected access claims: %+v", claims)
	}

	pending, err := GeneratePendingToken(secret, 7, false)
	if err != nil {
		t.Fatalf("GeneratePendingToken failed: %v", err)
	}
	pendingClaims, err := ParseAccessToken(secret, pending)
	if err != nil {
		t.Fatalf("ParseAccessToken for pending failed: %v", err)
	}
	if pendingClaims.UserID != 7 || pendingClaims.IsAdmin || !pendingClaims.TwoFAPending {
		t.Fatalf("unexpected pending claims: %+v", pendingClaims)
	}

	if _, err := ParseAccessToken("wrong-secret", access); err == nil {
		t.Fatal("expected token verification with wrong secret to fail")
	}
}

func TestTOTPGenerationAndVerification(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret failed: %v", err)
	}
	if len(secret) == 0 {
		t.Fatal("expected non-empty TOTP secret")
	}

	code, err := TOTPNow(secret)
	if err != nil {
		t.Fatalf("TOTPNow failed: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("expected 6-digit TOTP code, got %q", code)
	}
	if !VerifyTOTP(secret, code) {
		t.Fatal("VerifyTOTP rejected current TOTP code")
	}
	if VerifyTOTP(secret, "000000") && code != "000000" {
		t.Fatal("VerifyTOTP accepted invalid code")
	}

	uri := TOTPProvisioningURI(secret, "alice", "Fileship")
	if !strings.HasPrefix(uri, "otpauth://totp/Fileship:alice?secret="+secret) {
		t.Fatalf("unexpected provisioning URI: %s", uri)
	}

	backups, err := GenerateBackupCodes()
	if err != nil {
		t.Fatalf("GenerateBackupCodes failed: %v", err)
	}
	if len(backups) != 8 {
		t.Fatalf("expected 8 backup codes, got %d", len(backups))
	}

	tok, exp, err := GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken failed: %v", err)
	}
	if len(tok) != 64 || !exp.After(time.Now()) {
		t.Fatalf("unexpected refresh token or expiry: len=%d exp=%v", len(tok), exp)
	}
}
