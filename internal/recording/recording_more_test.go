package recording

import "testing"

func TestAsciinemaRecorderCloseWithNilFile(t *testing.T) {
	var recorder AsciinemaRecorder
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close() with nil file returned error: %v", err)
	}
}
