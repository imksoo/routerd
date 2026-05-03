//go:build !linux

package main

func bindSocketToDevice(_ int, _ string) error {
	return nil
}
