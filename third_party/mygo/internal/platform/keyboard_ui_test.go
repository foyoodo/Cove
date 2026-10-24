package platform

import "testing"

func TestKeyboardUIModeFull(t *testing.T) {
	if KeyboardUIModeFull(0) || KeyboardUIModeFull(1) {
		t.Fatal("bit 1 clear is not Full Keyboard Access")
	}
	if !KeyboardUIModeFull(2) || !KeyboardUIModeFull(3) {
		t.Fatal("bit 1 set is Full Keyboard Access")
	}
}
