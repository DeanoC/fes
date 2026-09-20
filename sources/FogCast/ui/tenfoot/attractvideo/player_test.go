package attractvideo

import "testing"

func TestOpenMissingFileFails(t *testing.T) {
	t.Parallel()
	player, err := Open("/no/such/fogcast-attract.mp4")
	if player != nil {
		player.Close()
		t.Fatal("expected nil player")
	}
	if err == nil {
		t.Fatal("expected open error")
	}
}

func TestAvailableMatchesPlatformDecoder(t *testing.T) {
	t.Parallel()
	player, err := Open("/no/such/fogcast-attract.mp4")
	if player != nil {
		player.Close()
	}
	if Available() && err == ErrUnavailable {
		t.Fatal("Available is true but Open returned ErrUnavailable")
	}
	if !Available() && err != ErrUnavailable {
		t.Fatalf("stub Open err = %v, want ErrUnavailable", err)
	}
}
