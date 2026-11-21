//go:build !darwin

package privacy

func screenRecording() bool { return false }

func accessibility() bool { return false }
