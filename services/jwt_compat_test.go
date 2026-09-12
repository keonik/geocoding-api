package services

import (
	"testing"
	"time"

	"geocoding-api/models"

	"github.com/golang-jwt/jwt/v5"
)

// A token signed before the v3 -> v5 upgrade must still validate afterwards.
//
// v5 stores exp and iat as NumericDate where v3 used int64, but both marshal
// to the same JSON numbers, so the wire format is unchanged. If that were
// wrong, the deploy would invalidate every session in flight and log out every
// signed-in user at once -- so it is asserted rather than assumed. The token
// below is built the way v3 built one: raw numeric claims, no NumericDate.
func TestTokensSignedByTheOldLibraryStillValidate(t *testing.T) {
	t.Setenv("JWT_SECRET", "compat-test-secret")

	legacy := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":  42,
		"email":    "legacy@example.com",
		"is_admin": true,
		"exp":      time.Now().Add(time.Hour).Unix(),
		"iat":      time.Now().Add(-time.Minute).Unix(),
	})
	tokenString, err := legacy.SignedString([]byte("compat-test-secret"))
	if err != nil {
		t.Fatalf("sign legacy token: %v", err)
	}

	claims, err := Auth.ValidateJWT(tokenString)
	if err != nil {
		t.Fatalf("a token in the pre-upgrade format was rejected: %v", err)
	}
	if claims.UserID != 42 || claims.Email != "legacy@example.com" || !claims.IsAdmin {
		t.Errorf("claims did not survive the round trip: %+v", claims)
	}
}

// Round trip through the new code path.
func TestGeneratedTokenValidates(t *testing.T) {
	t.Setenv("JWT_SECRET", "compat-test-secret")

	token, err := Auth.GenerateJWT(&models.User{ID: 7, Email: "user@example.com", IsAdmin: false})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	claims, err := Auth.ValidateJWT(token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if claims.UserID != 7 || claims.Email != "user@example.com" || claims.IsAdmin {
		t.Errorf("unexpected claims: %+v", claims)
	}
}

// v3 did not check expiry unless asked; v5 does it by default. Confirm an
// expired token is actually refused rather than silently accepted.
func TestExpiredTokenIsRejected(t *testing.T) {
	t.Setenv("JWT_SECRET", "compat-test-secret")

	expired := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": 1,
		"email":   "expired@example.com",
		"exp":     time.Now().Add(-time.Hour).Unix(),
		"iat":     time.Now().Add(-2 * time.Hour).Unix(),
	})
	tokenString, _ := expired.SignedString([]byte("compat-test-secret"))

	if _, err := Auth.ValidateJWT(tokenString); err == nil {
		t.Error("an expired token was accepted")
	}
}

// The classic JWT attack: swap the algorithm to one the verifier will accept
// with a key it should not use. WithValidMethods makes the refusal a parse
// option rather than a check the keyfunc has to remember to perform.
func TestAlgorithmConfusionIsRejected(t *testing.T) {
	t.Setenv("JWT_SECRET", "compat-test-secret")

	none := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"user_id":  1,
		"email":    "attacker@example.com",
		"is_admin": true,
		"exp":      time.Now().Add(time.Hour).Unix(),
	})
	tokenString, err := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign alg=none token: %v", err)
	}

	if _, err := Auth.ValidateJWT(tokenString); err == nil {
		t.Error("a token signed with alg=none was accepted")
	}
}

// A token signed with the wrong key must fail regardless of how well formed it
// is otherwise.
func TestWrongSecretIsRejected(t *testing.T) {
	t.Setenv("JWT_SECRET", "compat-test-secret")

	forged := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id":  1,
		"is_admin": true,
		"exp":      time.Now().Add(time.Hour).Unix(),
	})
	tokenString, _ := forged.SignedString([]byte("not-the-real-secret"))

	if _, err := Auth.ValidateJWT(tokenString); err == nil {
		t.Error("a token signed with the wrong secret was accepted")
	}
}
