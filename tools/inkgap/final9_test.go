package inkgap

import (
	"image/png"
	"math"
	"os"
	"testing"
)

// TestFinal9ComposerLight pins the 2× composer hint against the coverage
// reading that reproduces the reviewer's final9 numbers. Gaps are
// ↵→send, send→·, ·→⇧, ⇧→↵, ↵→newline.
func TestFinal9ComposerLight(t *testing.T) {
	f, err := os.Open("testdata/final9-composer-light-2x.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	gaps := FocusGaps(img, 2)
	want := []struct {
		i int
		v float64
	}{
		{0, 3.387},
		{4, 5.509},
		{5, 5.129},
		{6, 0.994},
		{7, 3.224},
	}
	if len(gaps) <= want[len(want)-1].i {
		t.Fatalf("gaps %d: %v", len(gaps), gaps)
	}
	for _, w := range want {
		if d := math.Abs(gaps[w.i] - w.v); d > 0.03 {
			t.Errorf("gap %d = %.3f, reviewer %.3f, off %.3f", w.i, gaps[w.i], w.v, gaps[w.i]-w.v)
		}
	}
}
