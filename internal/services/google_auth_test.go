package services

import (
	"testing"
	"time"
)

func TestGoogleTokenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	SetDataDir(dir)
	userID := "u1"
	want := &GoogleToken{
		AccessToken:  "at",
		RefreshToken: "rt",
		Expiry:       time.Now().UTC().Truncate(time.Second),
		Email:        "a@b.com",
	}
	if err := SaveGoogleToken(userID, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadGoogleToken(userID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken || got.Email != want.Email {
		t.Fatalf("got %+v want %+v", got, want)
	}
	if !got.Expiry.Equal(want.Expiry) {
		t.Fatalf("expiry got %v want %v", got.Expiry, want.Expiry)
	}
	if err := DeleteGoogleToken(userID); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGoogleToken(userID); err == nil {
		t.Fatal("expected error after delete")
	}
}
