//go:build !darwin

package main

func fontFamilyName(uintptr) string { return "" }
