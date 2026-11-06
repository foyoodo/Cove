package main

import "testing"

func TestFitWindowSize(t *testing.T) {
	const minW, minH = 720, 520
	const margin = 24

	tests := []struct {
		name             string
		wantW, wantH     int
		workW, workH     int
		expectW, expectH int
	}{
		{
			name:  "fits unchanged",
			wantW: 1120, wantH: 760,
			workW: 1440, workH: 900,
			expectW: 1120, expectH: 760,
		},
		{
			name:  "windows runner 1024x768 work area under taskbar",
			wantW: 1120, wantH: 760,
			workW: 1024, workH: 720,
			expectW: 1024 - 2*margin, expectH: 720 - 2*margin,
		},
		{
			name:  "1366x768 laptop",
			wantW: 1120, wantH: 760,
			workW: 1366, workH: 720,
			expectW: 1120, expectH: 720 - 2*margin,
		},
		{
			name:  "work area smaller than minimum keeps minimum",
			wantW: 1120, wantH: 760,
			workW: 700, workH: 500,
			expectW: minW, expectH: minH,
		},
		{
			name:  "missing work area keeps requested size",
			wantW: 1120, wantH: 760,
			workW: 0, workH: 0,
			expectW: 1120, expectH: 760,
		},
		{
			name:  "settings window still fits a 1024x768 work area",
			wantW: 780, wantH: 560,
			workW: 1024, workH: 720,
			expectW: 780, expectH: 560,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, h := fitWindowSize(tt.wantW, tt.wantH, minW, minH, tt.workW, tt.workH, margin)
			if w != tt.expectW || h != tt.expectH {
				t.Fatalf("fitWindowSize(%d,%d on %dx%d) = %dx%d, want %dx%d",
					tt.wantW, tt.wantH, tt.workW, tt.workH, w, h, tt.expectW, tt.expectH)
			}
			if w < minW || h < minH {
				t.Fatalf("result %dx%d is below minimum %dx%d", w, h, minW, minH)
			}
		})
	}
}
